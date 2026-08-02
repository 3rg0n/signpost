// Package telemetry instruments signpost's own execution.
//
// This is the only package that reports on the *run* rather than on the repository.
// Everything else answers "what is in this tree"; this answers "which stage of the
// pipeline ate the forty seconds", which is the question a user in CI cannot otherwise
// ask ([ADR 0014](../../docs/adr/0014-adopt-the-otel-sdk-and-write-the-exporter.md)).
//
// Four properties, and each is a rule a later change is likely to break:
//
//   - **Off unless asked.** [EnvEnable] gates it. Disabled means no SDK is constructed,
//     no goroutine starts, and [Stage] returns the context it was handed with a zero
//     [Span] — no allocation on any path a disabled run takes.
//   - **Fail open.** [Init] returns no error. An unreachable collector, a malformed
//     endpoint, and a resource-detection fault all leave the run's exit code alone.
//     Telemetry can never be the reason `signpost build` failed.
//   - **No repository content, structurally.** A span carries counts and durations. The
//     only attribute setter on [Span] takes an int, so there is no way to put a path,
//     a module name, or an error message on a span — the rule is enforced by the API's
//     shape rather than by every call site remembering it. [Span.Failed] takes no
//     argument for the same reason: an error message routinely contains the path that
//     failed to open.
//   - **Bounded on exit.** [Shutdown] flushes for at most [flushTimeout]. signpost is a
//     short-lived process, so a batch that has not been posted by the time the pipeline
//     finishes is flushed at exit or dropped — never waited on indefinitely.
//
// The tracer is package state, set once by [Init] before the CLI dispatches and read
// from there on. That mirrors upstream's own `otel.SetTracerProvider`, and it is what
// lets the pipeline take spans without every command signature growing a parameter for
// a feature almost no run enables.
package telemetry

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// EnvEnable is the switch, and it is the only one.
//
// An endpoint in the environment is deliberately not sufficient. CI runners carry
// OTEL_* variables for unrelated collectors, and ADR 0009 established that a default
// which sends anything anywhere has already sent it by the time anyone notices. So the
// variable that means "this run may talk to a collector" is signpost's own, and the
// OTEL_* variables only say where.
const EnvEnable = "SIGNPOST_ENABLE_TELEMETRY"

// scopeName identifies the instrumentation, not the instrumented thing. Upstream's
// convention is the Go import path of the package holding the calls.
const scopeName = "github.com/3rg0n/signpost/internal/telemetry"

// flushTimeout bounds the exit path. Two seconds because the alternative is a tool that
// hangs on a collector that stopped answering, and a lost trace costs a measurement
// where a hung `signpost build` costs a merge.
const flushTimeout = 2 * time.Second

// Shutdown flushes what is buffered and releases the exporter. Always safe to call,
// including on a run where telemetry was never enabled.
type Shutdown func()

// tracer is nil when telemetry is disabled, which is the whole of the no-op: Stage
// checks it and returns without allocating.
//
// provider is kept alongside it so that a test can force a flush without waiting out the
// batch processor's five-second schedule. Nothing outside this package needs it — the
// Shutdown closure holds its own reference — and it is nil whenever tracer is.
var (
	tracer   trace.Tracer
	provider *sdktrace.TracerProvider
)

// Init turns telemetry on if the environment asks for it, and otherwise does nothing.
//
// It returns no error by design. Every failure mode here — a value that is not a
// boolean, a resource detector that faulted, a collector that is not listening — ends
// with telemetry off or degraded and the run proceeding. version is passed in rather
// than read from a variable here because the release workflow stamps it into package
// main, and a second copy would be a second thing to stamp.
//
// Whatever the exporter or the SDK reports asynchronously is collected and printed by
// the returned Shutdown, on the caller's goroutine. Not printed as it happens: the
// batch processor reports from its own goroutine, and interleaving that into the middle
// of a coverage report would both garble the report and race the writer.
func Init(ctx context.Context, errOut io.Writer, version string) Shutdown {
	raw, set := os.LookupEnv(EnvEnable)
	if !set || strings.TrimSpace(raw) == "" {
		return func() {}
	}
	on, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		// Said rather than swallowed. §4.2's rule is that the absence of a measurement
		// must never read as a clean bill of health, and somebody who typed `=yes` has
		// asked for telemetry and would otherwise get silence indistinguishable from a
		// working exporter with nothing to say.
		report(errOut, "%s=%q is not a boolean, so telemetry stays off", EnvEnable, raw)
		return func() {}
	}
	if !on {
		return func() {}
	}

	exp, err := newExporter()
	if err != nil {
		report(errOut, "%v, so telemetry stays off", err)
		return func() {}
	}

	sink := &errSink{}
	otel.SetErrorHandler(sink)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(newResource(ctx, version)),
	)
	provider = tp
	tracer = tp.Tracer(scopeName, trace.WithInstrumentationVersion(version))

	var once sync.Once
	return func() {
		// Idempotent, because a deferred stop and an explicit one are both reasonable at
		// a call site and the second Shutdown would otherwise report "already stopped" as
		// a telemetry failure on stderr.
		once.Do(func() {
			// Bounded, and bounded from a fresh context: the caller's may already be
			// cancelled by the time a command is unwinding, and a flush that skipped
			// itself because of that would drop the whole run's trace at the last step.
			flushCtx, cancel := context.WithTimeout(context.Background(), flushTimeout)
			defer cancel()
			if err := tp.Shutdown(flushCtx); err != nil {
				sink.Handle(err)
			}
			tracer, provider = nil, nil
			if err := sink.first(); err != nil {
				report(errOut, "%v", err)
			}
		})
	}
}

// newResource describes the process the spans came from.
//
// Order is the contract: signpost's own attributes go first so that OTEL_SERVICE_NAME
// and OTEL_RESOURCE_ATTRIBUTES override them, which is what an operator running several
// instrumented tools against one collector expects. None of the detectors used here
// reads a filesystem path — no WithProcess, no WithHost — because a resource attribute
// is attached to every span in the batch, so a path leaking in here would leak
// everywhere at once.
func newResource(ctx context.Context, version string) *resource.Resource {
	attrs := []attribute.KeyValue{
		attribute.String("service.name", "signpost"),
		attribute.String("service.version", version),
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(attrs...),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		// Detection is partial or the schema URLs conflicted. Both return a usable
		// resource alongside the error, but the fail-open floor is the two attributes
		// this process can state without detecting anything.
		return resource.NewSchemaless(attrs...)
	}
	return res
}

// Stage starts a span for one stage of the pipeline and returns the context its
// children must use.
//
// The returned Span is safe to use when telemetry is off: its zero value is the no-op,
// so a call site needs no branch and no nil check.
func Stage(ctx context.Context, name string) (context.Context, Span) {
	if tracer == nil {
		return ctx, Span{}
	}
	ctx, s := tracer.Start(ctx, name)
	return ctx, Span{span: s}
}

// Span is one stage's measurement. The zero value is a working no-op.
//
// Its surface is deliberately two methods and one value type. A SetAttributes taking
// attribute.KeyValue would let any caller put a path on a span, and the rule that spans
// carry no repository content would then live in review comments rather than in the
// compiler.
type Span struct {
	span trace.Span
}

// Count records how many of something a stage handled. An int, because a count is the
// only span attribute signpost emits: it answers "was this stage slow because the
// repository is large" without saying anything about what is in it.
func (s Span) Count(key string, n int) {
	if s.span == nil {
		return
	}
	s.span.SetAttributes(attribute.Int(key, n))
}

// Failed marks the stage as errored, and records nothing about the error.
//
// No message and no RecordError: a Go error from this pipeline routinely reads
// "open /home/someone/private/repo/x.go: permission denied", and the stage that failed
// is the whole of what a trace needs to say.
func (s Span) Failed() {
	if s.span == nil {
		return
	}
	s.span.SetStatus(codes.Error, "")
}

// End stops the span. Deferring it is correct on a disabled run too.
func (s Span) End() {
	if s.span == nil {
		return
	}
	s.span.End()
}

// report writes one telemetry note to stderr, prefixed so it is attributable, and
// discards the write error deliberately.
//
// The discard is the point of the helper. Everywhere else in signpost a write error is
// kept — printer.go and mermaid.go both hold onto one — because there the bytes are the
// output. Here they are a note about a subsystem that has already failed open, and a
// caller cannot do anything with "reporting that telemetry is off did not work" except
// fail a run over telemetry, which is exactly what this package promises not to do.
func report(errOut io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(errOut, "telemetry: "+format+"\n", args...)
}

// errSink keeps the first thing the SDK or the exporter complained about, for Shutdown
// to print once.
//
// First rather than a count or a list: a collector that is down produces one fault per
// batch saying the same thing, and the useful report is that exporting failed at all.
type errSink struct {
	mu  sync.Mutex
	err error
}

func (s *errSink) Handle(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (s *errSink) first() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
