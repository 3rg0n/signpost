package telemetry

// OTLP over HTTP with a JSON payload, on net/http and encoding/json.
//
// This is the half of the pipeline ADR 0014 declines to import. `otlptracehttp` — the
// upstream HTTP exporter — links `google.golang.org/grpc`, `protobuf`, and
// `grpc-gateway/v2`: 65 gRPC packages and 36 protobuf packages for a transport that uses
// neither, because its wire format is protobuf-encoded. OTLP/JSON is a documented,
// stable alternative every collector accepts, and the failure mode of getting it wrong
// is a rejected payload — loud, local, and caught by a test posting to an
// httptest.Server.
//
// Two encoding rules in the OTLP JSON mapping are easy to get silently wrong, and both
// are asserted in exporter_test.go:
//
//   - 64-bit integers are **strings**, per proto3 JSON. A timestamp emitted as a JSON
//     number loses precision at the nanosecond, and a collector may reject it outright.
//   - trace and span IDs are **lowercase hex**, not the base64 that proto3 JSON
//     otherwise specifies for `bytes`. This is an explicit exception in the OTLP spec.
//
// Deliberately not implemented, because nothing here needs them and each is a surface
// with its own failure modes: compression (OTEL_EXPORTER_OTLP_COMPRESSION — a five-span
// payload is smaller than the header block that would describe its encoding), custom CA
// and client certificates (the default transport's system roots cover a collector with a
// real certificate), and retry. A dropped batch costs a measurement; the run it measured
// has already succeeded.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// The OTEL_* variables this exporter reads. Endpoint, headers, and timeout, per-signal
// override first, in the spelling the specification fixes — an operator's existing
// collector configuration should work without learning signpost's own names.
const (
	envEndpoint       = "OTEL_EXPORTER_OTLP_ENDPOINT"
	envTracesEndpoint = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
	envHeaders        = "OTEL_EXPORTER_OTLP_HEADERS"
	envTracesHeaders  = "OTEL_EXPORTER_OTLP_TRACES_HEADERS"
	envTimeout        = "OTEL_EXPORTER_OTLP_TIMEOUT"
	envTracesTimeout  = "OTEL_EXPORTER_OTLP_TRACES_TIMEOUT"
	envProtocol       = "OTEL_EXPORTER_OTLP_PROTOCOL"
	envTracesProtocol = "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"
)

// defaultEndpoint is the collector's default HTTP listener, from the specification.
const defaultEndpoint = "http://localhost:4318"

// tracesPath is appended to the non-signal-specific endpoint and never to the
// signal-specific one — the specification is explicit that
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT is a full URL used as given, which is how an
// operator points one signal at a different path.
const tracesPath = "/v1/traces"

// defaultTimeout matches the specification's 10s default for one export attempt. It is
// not the same bound as flushTimeout: this caps a single POST, and flushTimeout caps how
// long exiting waits for the queue.
const defaultTimeout = 10 * time.Second

// exporter implements sdktrace.SpanExporter, which is two methods.
type exporter struct {
	client   *http.Client
	endpoint string
	headers  map[string]string

	mu      sync.Mutex
	stopped bool
}

var _ sdktrace.SpanExporter = (*exporter)(nil)

// newExporter resolves the environment and returns an error only for configuration this
// exporter cannot honour. It does not contact the collector: a build must not fail, or
// pause, because something is not listening on 4318, so the first attempt happens on the
// batch processor's goroutine where its failure is a report rather than an exit code.
func newExporter() (*exporter, error) {
	if err := checkProtocol(); err != nil {
		return nil, err
	}
	endpoint, err := resolveEndpoint()
	if err != nil {
		return nil, err
	}
	return &exporter{
		client:   &http.Client{Timeout: resolveTimeout()},
		endpoint: endpoint,
		headers:  resolveHeaders(),
	}, nil
}

// checkProtocol refuses a protocol this exporter does not speak, rather than posting
// JSON to a collector expecting protobuf and reporting the rejection as a transport
// fault. Saying "this build cannot do grpc" names the fix; a 400 does not.
func checkProtocol() error {
	p := firstEnv(envTracesProtocol, envProtocol)
	switch p {
	case "", "http/json":
		return nil
	default:
		return fmt.Errorf("%s=%q, and this build only speaks http/json", envProtocol, p)
	}
}

// resolveEndpoint builds the URL to POST to.
func resolveEndpoint() (string, error) {
	if raw := firstEnv(envTracesEndpoint); raw != "" {
		return validateEndpoint(raw, envTracesEndpoint, false)
	}
	if raw := firstEnv(envEndpoint); raw != "" {
		return validateEndpoint(raw, envEndpoint, true)
	}
	return defaultEndpoint + tracesPath, nil
}

// validateEndpoint rejects what would otherwise fail once per batch inside the HTTP
// client, where the message names Go's URL parser rather than the variable to fix.
func validateEndpoint(raw, name string, appendPath bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s=%q is not a URL", name, raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%s=%q is not http or https", name, raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%s=%q names no host", name, raw)
	}
	if !appendPath {
		return u.String(), nil
	}
	// A trailing slash on the base is the common spelling and must not produce
	// `//v1/traces`, which some collectors route to a 404.
	u.Path = strings.TrimSuffix(u.Path, "/") + tracesPath
	return u.String(), nil
}

// resolveTimeout reads the specification's milliseconds. A value that is not a positive
// integer falls back to the default rather than failing the run — it is a timeout, and
// the worst a wrong one costs is a dropped batch.
func resolveTimeout() time.Duration {
	raw := firstEnv(envTracesTimeout, envTimeout)
	if raw == "" {
		return defaultTimeout
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return defaultTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

// resolveHeaders parses `k1=v1,k2=v2`. Values are percent-decoded per the W3C Baggage
// encoding the specification points at, with url.PathUnescape rather than
// QueryUnescape: the latter turns `+` into a space, and a `+` is legal in a token.
//
// Per-signal headers replace the generic ones rather than merging with them, which is
// what the specification says and is also the safer reading — merging would send a
// credential the operator scoped to another signal.
func resolveHeaders() map[string]string {
	raw := firstEnv(envTracesHeaders, envHeaders)
	if raw == "" {
		return nil
	}
	out := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(pair, "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !ok || k == "" {
			continue
		}
		if dec, err := url.PathUnescape(v); err == nil {
			v = dec
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// firstEnv returns the first of names with a non-empty value, which is how the
// specification's per-signal-then-generic precedence is spelled.
func firstEnv(names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}

// ExportSpans posts one batch. Called from the batch processor's goroutine.
func (e *exporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	stopped := e.stopped
	e.mu.Unlock()
	// After Shutdown this is a no-op returning nil, which the SpanExporter contract
	// requires: the processor may drain a queue after shutting the exporter down, and an
	// error there would be reported as an export failure that did not happen.
	if stopped || len(spans) == 0 {
		return nil
	}

	body, err := json.Marshal(payloadFor(spans))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		// The status and nothing else. A collector's error body can quote the payload
		// back, and this string reaches a user's terminal.
		return fmt.Errorf("collector answered %s", resp.Status)
	}
	return nil
}

// Shutdown releases the exporter. Idempotent, because both TracerProvider.Shutdown and a
// processor draining its queue can reach it.
func (e *exporter) Shutdown(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopped = true
	e.client.CloseIdleConnections()
	return nil
}

// The OTLP JSON shapes. Structs rather than map[string]any so the field names and the
// omissions are stated once and checked by the compiler, and so key order is stable —
// which is what makes the test's assertions on the wire payload readable.
type tracePayload struct {
	ResourceSpans []resourceSpans `json:"resourceSpans"`
}

type resourceSpans struct {
	Resource   otlpResource `json:"resource"`
	ScopeSpans []scopeSpans `json:"scopeSpans"`
	SchemaURL  string       `json:"schemaUrl,omitempty"`
}

type otlpResource struct {
	Attributes []otlpAttr `json:"attributes,omitempty"`
}

type scopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type otlpSpan struct {
	TraceID string `json:"traceId"`
	SpanID  string `json:"spanId"`
	// Omitted rather than sent as zeroes for a root span: an all-zero parent is not a
	// valid span ID, and some collectors read the field's presence as "has a parent".
	ParentSpanID      string      `json:"parentSpanId,omitempty"`
	Name              string      `json:"name"`
	Kind              int         `json:"kind"`
	StartTimeUnixNano string      `json:"startTimeUnixNano"`
	EndTimeUnixNano   string      `json:"endTimeUnixNano"`
	Attributes        []otlpAttr  `json:"attributes,omitempty"`
	Status            *otlpStatus `json:"status,omitempty"`
}

type otlpStatus struct {
	Code int `json:"code"`
}

type otlpAttr struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

// otlpValue is proto3 JSON's oneof: exactly one field is set, and the rest are omitted.
type otlpValue struct {
	StringValue *string  `json:"stringValue,omitempty"`
	IntValue    *string  `json:"intValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
}

// payloadFor groups spans by resource and scope, which is the structure OTLP requires
// rather than an optimisation: `resource` and `scope` are stated once per group, so a
// flat list of spans has nowhere to put them.
//
// Grouping is by pointer identity for the resource and by name+version for the scope.
// Both are correct here for the same reason — one TracerProvider means one resource, and
// this package uses one scope — and neither assumption is what the grouping relies on.
func payloadFor(spans []sdktrace.ReadOnlySpan) tracePayload {
	type scopeKey struct{ name, version string }

	var out tracePayload
	// Insertion order preserved deliberately: a map iteration would reorder the payload
	// between runs, and the test that reads it would have to sort before asserting.
	resIndex := map[*resource.Resource]int{}
	scopeIndex := map[int]map[scopeKey]int{}

	for _, s := range spans {
		res := s.Resource()
		ri, ok := resIndex[res]
		if !ok {
			ri = len(out.ResourceSpans)
			resIndex[res] = ri
			out.ResourceSpans = append(out.ResourceSpans, resourceSpans{
				Resource:  otlpResource{Attributes: attrsFor(res.Attributes())},
				SchemaURL: res.SchemaURL(),
			})
			scopeIndex[ri] = map[scopeKey]int{}
		}
		sc := s.InstrumentationScope()
		key := scopeKey{sc.Name, sc.Version}
		si, ok := scopeIndex[ri][key]
		if !ok {
			si = len(out.ResourceSpans[ri].ScopeSpans)
			scopeIndex[ri][key] = si
			out.ResourceSpans[ri].ScopeSpans = append(out.ResourceSpans[ri].ScopeSpans,
				scopeSpans{Scope: otlpScope{Name: sc.Name, Version: sc.Version}})
		}
		target := &out.ResourceSpans[ri].ScopeSpans[si]
		target.Spans = append(target.Spans, spanFor(s))
	}
	return out
}

func spanFor(s sdktrace.ReadOnlySpan) otlpSpan {
	sc := s.SpanContext()
	out := otlpSpan{
		TraceID:           traceHex(sc.TraceID()),
		SpanID:            spanHex(sc.SpanID()),
		Name:              s.Name(),
		Kind:              int(s.SpanKind()),
		StartTimeUnixNano: nanos(s.StartTime()),
		EndTimeUnixNano:   nanos(s.EndTime()),
		Attributes:        attrsFor(s.Attributes()),
	}
	if p := s.Parent(); p.HasSpanID() {
		out.ParentSpanID = spanHex(p.SpanID())
	}
	if code := statusCode(s.Status().Code); code != 0 {
		out.Status = &otlpStatus{Code: code}
	}
	return out
}

// statusCode translates the Go API's codes.Code to OTLP's, and the two do not agree.
//
// codes.Error is 1 and codes.Ok is 2; in OTLP, ERROR is 2 and OK is 1. Passing the value
// through unchanged compiles, exports, and reports every failed stage as a successful
// one — the exact kind of quiet inversion ADR 0014 gives as the reason not to hand-roll
// the parts of OTel that are hard to check.
func statusCode(c codes.Code) int {
	switch c {
	case codes.Error:
		return 2
	case codes.Ok:
		return 1
	default:
		return 0
	}
}

// attrsFor converts the SDK's attributes.
//
// Only the four scalar types are spelled out, because they are the only ones that can
// arrive: Span.Count emits an int and the resource is strings. Anything else — a slice,
// a type added upstream later — is rendered with the attribute package's own encoder
// rather than dropped, so an attribute signpost does not know how to model still reaches
// the collector as something readable instead of vanishing from the payload.
func attrsFor(kvs []attribute.KeyValue) []otlpAttr {
	if len(kvs) == 0 {
		return nil
	}
	out := make([]otlpAttr, 0, len(kvs))
	for _, kv := range kvs {
		out = append(out, otlpAttr{Key: string(kv.Key), Value: valueFor(kv.Value)})
	}
	return out
}

func valueFor(v attribute.Value) otlpValue {
	switch v.Type() {
	case attribute.INT64:
		s := strconv.FormatInt(v.AsInt64(), 10)
		return otlpValue{IntValue: &s}
	case attribute.BOOL:
		b := v.AsBool()
		return otlpValue{BoolValue: &b}
	case attribute.FLOAT64:
		f := v.AsFloat64()
		return otlpValue{DoubleValue: &f}
	case attribute.STRING:
		s := v.AsString()
		return otlpValue{StringValue: &s}
	default:
		s := v.String()
		return otlpValue{StringValue: &s}
	}
}

// Lowercase hex is the OTLP JSON exception to proto3's base64-for-bytes rule. The SDK's
// own String() on these types is already this encoding, and these spell it out rather
// than depending on a Stringer whose contract is "for humans".
func traceHex(id trace.TraceID) string { return hex.EncodeToString(id[:]) }

func spanHex(id trace.SpanID) string { return hex.EncodeToString(id[:]) }

// nanos renders a timestamp as proto3 JSON requires 64-bit integers to be rendered: a
// decimal string. A JSON number would be read as a float64 by many parsers and lose the
// low digits, which is how a span acquires a duration it did not have.
func nanos(t time.Time) string {
	if t.IsZero() {
		return "0"
	}
	return strconv.FormatInt(t.UnixNano(), 10)
}
