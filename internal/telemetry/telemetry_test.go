package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// collector is a stand-in for an OTLP endpoint. It records every payload posted to it,
// so a test can assert on the wire bytes rather than on the SDK's in-memory spans —
// which is the point: an in-memory exporter would pass with a marshaller that emits
// timestamps as JSON numbers, and a collector rejects that.
type collector struct {
	*httptest.Server

	mu      sync.Mutex
	bodies  [][]byte
	headers []http.Header
	status  int
}

func newCollector(t *testing.T) *collector {
	t.Helper()
	c := &collector{status: http.StatusOK}
	c.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		c.headers = append(c.headers, r.Header.Clone())
		status := c.status
		c.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(c.Close)
	return c
}

// contentType returns the Content-Type of the first request, which is what tells a
// collector to parse the body as JSON rather than as protobuf.
func (c *collector) contentType(t *testing.T) string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.headers) == 0 {
		t.Fatal("the collector received no request")
	}
	return c.headers[0].Get("Content-Type")
}

// header returns a request header the exporter sent, for asserting on
// OTEL_EXPORTER_OTLP_HEADERS.
func (c *collector) header(t *testing.T, name string) string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.headers) == 0 {
		t.Fatal("the collector received no request")
	}
	return c.headers[0].Get(name)
}

// payloads returns what was posted, decoded. Decoded into map[string]any rather than into
// the exporter's own structs, because asserting with the types that produced the bytes
// would pass whatever they emitted — including an integer serialised as a number.
func (c *collector) payloads(t *testing.T) []map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]map[string]any, 0, len(c.bodies))
	for _, b := range c.bodies {
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("collector received invalid JSON: %v\n%s", err, b)
		}
		out = append(out, m)
	}
	return out
}

func (c *collector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bodies)
}

// enable points telemetry at the collector and turns it on, restoring the environment
// afterwards. t.Setenv rather than os.Setenv so the restore is unconditional and the test
// is refused if it ever runs in parallel — the tracer is package state.
func enable(t *testing.T, c *collector) {
	t.Helper()
	t.Setenv(EnvEnable, "1")
	t.Setenv(envTracesEndpoint, c.URL+tracesPath)
}

// initFor runs Init and guarantees Shutdown, returning what Init wrote to stderr.
func initFor(t *testing.T) *bytes.Buffer {
	t.Helper()
	var errOut bytes.Buffer
	stop := Init(context.Background(), &errOut, "v9.9.9-test")
	t.Cleanup(stop)
	return &errOut
}

func TestSpansReachACollectorAsOTLPJSON(t *testing.T) {
	c := newCollector(t)
	enable(t, c)
	errOut := initFor(t)

	ctx, root := Stage(context.Background(), "analyse")
	_, child := Stage(ctx, "discover")
	child.Count("signpost.files", 42)
	child.End()
	root.End()

	flush(t)

	if errOut.Len() != 0 {
		t.Errorf("Init and export wrote to stderr on a working collector:\n%s", errOut)
	}
	if c.count() == 0 {
		t.Fatal("the collector received nothing; no span was exported")
	}

	spans := spansIn(t, c)
	if len(spans) != 2 {
		t.Fatalf("%d span(s) exported, want 2 (analyse, discover)", len(spans))
	}
	byName := map[string]map[string]any{}
	for _, s := range spans {
		byName[str(s["name"])] = s
	}
	for _, want := range []string{"analyse", "discover"} {
		if byName[want] == nil {
			t.Fatalf("no span named %q; got %v", want, names(spans))
		}
	}

	// The child's parent is the root, which is the whole reason Stage returns a context:
	// a flat pair of spans reports two durations and no nesting, and the question
	// "which stage" needs the tree.
	rootID := str(byName["analyse"]["spanId"])
	if got := str(byName["discover"]["parentSpanId"]); got != rootID {
		t.Errorf("discover's parentSpanId is %q, want the analyse span %q", got, rootID)
	}
	// And the root has no parent at all, rather than a field of zeroes: an all-zero span
	// ID is not valid, and a collector may read the key's presence as "has a parent".
	if _, ok := byName["analyse"]["parentSpanId"]; ok {
		t.Errorf("the root span carries a parentSpanId: %v", byName["analyse"]["parentSpanId"])
	}
	if got := str(byName["discover"]["traceId"]); got != str(byName["analyse"]["traceId"]) {
		t.Errorf("the two spans are in different traces: %q and %q",
			got, str(byName["analyse"]["traceId"]))
	}
}

// The two encodings in OTLP's JSON mapping that are wrong in a way nothing else catches:
// a 64-bit integer is a string, and an ID is lowercase hex rather than proto3's base64.
// Both produce a payload that looks right and is rejected or silently truncated.
func TestSixtyFourBitFieldsAreStringsAndIDsAreHex(t *testing.T) {
	c := newCollector(t)
	enable(t, c)
	initFor(t)

	_, span := Stage(context.Background(), "discover")
	span.Count("signpost.files", 1<<53+1) // beyond float64's exact integer range
	span.End()
	flush(t)

	spans := spansIn(t, c)
	if len(spans) != 1 {
		t.Fatalf("%d span(s), want 1", len(spans))
	}
	s := spans[0]

	for _, key := range []string{"startTimeUnixNano", "endTimeUnixNano"} {
		v, ok := s[key]
		if !ok {
			t.Errorf("%s missing from the payload", key)
			continue
		}
		if _, isString := v.(string); !isString {
			t.Errorf("%s is %T (%v), want a JSON string: a number loses the low digits",
				key, v, v)
		}
	}

	for _, key := range []string{"traceId", "spanId"} {
		got := str(s[key])
		want := map[string]int{"traceId": 32, "spanId": 16}[key]
		if len(got) != want {
			t.Errorf("%s is %q (%d chars), want %d hex chars", key, got, len(got), want)
		}
		if strings.ToLower(got) != got || strings.Trim(got, "0123456789abcdef") != "" {
			t.Errorf("%s is %q, want lowercase hex — base64 is the proto3 default and "+
				"OTLP/JSON is an explicit exception to it", key, got)
		}
	}

	attrs := attrsOf(t, s)
	got, ok := attrs["signpost.files"]
	if !ok {
		t.Fatalf("signpost.files missing; attributes were %v", attrs)
	}
	if got["intValue"] != "9007199254740993" {
		t.Errorf("signpost.files intValue is %#v, want the string \"9007199254740993\"; "+
			"a JSON number here is read as a float64 and comes back 9007199254740992",
			got["intValue"])
	}
}

// codes.Error is 1 and codes.Ok is 2; in OTLP, ERROR is 2 and OK is 1. A marshaller that
// passes the value through compiles, exports, and reports every failed stage as a
// successful one.
func TestAFailedStageIsExportedAsOTLPError(t *testing.T) {
	c := newCollector(t)
	enable(t, c)
	initFor(t)

	_, bad := Stage(context.Background(), "history")
	bad.Failed()
	bad.End()
	_, good := Stage(context.Background(), "assemble")
	good.End()
	flush(t)

	byName := map[string]map[string]any{}
	for _, s := range spansIn(t, c) {
		byName[str(s["name"])] = s
	}
	if len(byName) != 2 {
		t.Fatalf("%d span(s), want 2: %v", len(byName), byName)
	}

	status, ok := byName["history"]["status"].(map[string]any)
	if !ok {
		t.Fatalf("the failed span carries no status: %v", byName["history"])
	}
	if code, _ := status["code"].(float64); code != 2 {
		t.Errorf("the failed span's status code is %v, want 2 (OTLP ERROR). "+
			"1 is codes.Error in Go and OK in OTLP, so passing the value through "+
			"reports every failure as a success", status["code"])
	}
	// The negative half: an unset status is omitted rather than sent as a code. Without
	// this, a marshaller that stamped ERROR on everything would pass the check above.
	if v, ok := byName["assemble"]["status"]; ok {
		t.Errorf("the successful span carries a status %v; an unset status is omitted", v)
	}
}

// The rule ADR 0014 binds the implementation to: spans carry no repository content. The
// API makes it structurally impossible — Count takes an int and Failed takes nothing —
// and this asserts the outcome on the bytes, because a later change adding a string
// setter would compile and this is what would fail.
func TestNoRepositoryContentReachesTheWire(t *testing.T) {
	c := newCollector(t)
	enable(t, c)
	// A path-shaped value in the environment the resource detector reads, so the test
	// covers the one route by which a filesystem path could reach a span without any
	// call site asking for it.
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=ci")
	initFor(t)

	ctx, root := Stage(context.Background(), "analyse")
	_, child := Stage(ctx, "discover")
	child.Count("signpost.files", 3)
	child.Failed()
	child.End()
	root.End()
	flush(t)

	// Asserted on the decoded structure rather than by searching the bytes for a path
	// separator. Two of the payload's fields legitimately contain one — the scope name is
	// this package's import path and the schema URL is a URL — so a substring search
	// needs them redacted first, and redacting by string replacement mutilates the
	// surrounding JSON into something that then matches. The structural check is also the
	// stronger one: it fails on *any* unexpected value, not only on ones shaped like a
	// path.
	for _, s := range spansIn(t, c) {
		switch str(s["name"]) {
		case "analyse", "discover":
		default:
			t.Errorf("span named %q; a span name is a fixed stage name", s["name"])
		}
		for key, v := range attrsOf(t, s) {
			if !strings.HasPrefix(key, "signpost.") {
				t.Errorf("span attribute %q is outside signpost's own namespace", key)
			}
			// The rule, on the wire: the only span attribute is a count. A string-valued
			// span attribute is how a path would arrive, and there is no API on Span that
			// can produce one.
			if _, isInt := v["intValue"]; !isInt {
				t.Errorf("span attribute %q is %v, want an intValue: a count is the only "+
					"thing a span carries about the repository", key, v)
			}
		}
	}

	// The resource is the other route, and it is attached to every span in the batch. Its
	// keys are fixed: two this package sets, three the SDK's own detector adds, and
	// whatever OTEL_RESOURCE_ATTRIBUTES asked for. No detector that reads a filesystem
	// path is enabled — a WithProcess added later would put the executable's path here and
	// fail on this list.
	allowed := map[string]bool{
		"service.name": true, "service.version": true,
		"telemetry.sdk.name": true, "telemetry.sdk.language": true,
		"telemetry.sdk.version":  true,
		"deployment.environment": true,
	}
	for _, p := range c.payloads(t) {
		for _, rs := range list(t, p["resourceSpans"]) {
			res, ok := rs["resource"].(map[string]any)
			if !ok {
				t.Fatalf("payload has no resource: %v", rs)
			}
			for _, a := range list(t, res["attributes"]) {
				if !allowed[str(a["key"])] {
					t.Errorf("resource attribute %q is not one this package sets or the "+
						"environment asked for; it is attached to every span in the batch",
						a["key"])
				}
			}
		}
	}
}

func TestTelemetryIsOffUnlessAsked(t *testing.T) {
	c := newCollector(t)
	t.Setenv(envTracesEndpoint, c.URL+tracesPath)

	for _, tc := range []struct {
		name  string
		set   bool
		value string
		warns bool
	}{
		{name: "unset", set: false},
		{name: "empty", set: true, value: ""},
		{name: "false", set: true, value: "false"},
		{name: "zero", set: true, value: "0"},
		// Off *and* said out loud. Somebody who typed `=yes` has asked for telemetry,
		// and silence is indistinguishable from a working exporter with nothing to say.
		{name: "not a boolean", set: true, value: "yes", warns: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(EnvEnable, tc.value)
			} else {
				// Set first so t.Setenv registers the restore, then unset: the "unset"
				// case has to be reachable even when the developer running the suite has
				// the variable exported in their own shell.
				t.Setenv(EnvEnable, "1")
				if err := os.Unsetenv(EnvEnable); err != nil {
					t.Fatalf("Unsetenv: %v", err)
				}
			}
			var errOut bytes.Buffer
			stop := Init(context.Background(), &errOut, "v0")
			if tracer != nil {
				t.Error("Init constructed a tracer with telemetry off")
			}
			_, span := Stage(context.Background(), "discover")
			span.Count("signpost.files", 1)
			span.Failed()
			span.End()
			stop()

			if got := errOut.Len() > 0; got != tc.warns {
				t.Errorf("stderr non-empty = %v, want %v:\n%s", got, tc.warns, errOut.String())
			}
			if tc.warns && !strings.Contains(errOut.String(), EnvEnable) {
				t.Errorf("the warning does not name %s, so it does not name the fix:\n%s",
					EnvEnable, errOut.String())
			}
			if n := c.count(); n != 0 {
				t.Errorf("%d payload(s) posted with telemetry off", n)
			}
		})
	}
}

// The positive boundary for the test above: the same call sequence with the variable set
// does reach a collector. Without it, a broken gate that never enabled anything would
// pass every off-case.
func TestTheGateOpens(t *testing.T) {
	c := newCollector(t)
	enable(t, c)
	initFor(t)
	if tracer == nil {
		t.Fatal("Init constructed no tracer with " + EnvEnable + " set")
	}
	_, span := Stage(context.Background(), "discover")
	span.End()
	flush(t)
	if c.count() == 0 {
		t.Error("nothing reached the collector with telemetry enabled")
	}
}

// Fail open, clause 4. Every one of these is a run that must produce spans or not, and
// must never produce an error the caller could return.
func TestFailuresNeverReachTheCaller(t *testing.T) {
	t.Run("a collector answering 500", func(t *testing.T) {
		c := newCollector(t)
		c.status = http.StatusInternalServerError
		enable(t, c)
		var errOut bytes.Buffer
		stop := Init(context.Background(), &errOut, "v0")
		_, span := Stage(context.Background(), "discover")
		span.End()
		stop()
		// Reported, because a user who asked for telemetry and is not getting it needs
		// to know — but on stderr, after the run, and with no effect on anything.
		if !strings.Contains(errOut.String(), "500") {
			t.Errorf("a rejected export was not reported on stderr:\n%s", errOut.String())
		}
		// And the collector's own error body is not quoted back: it can contain the
		// payload, and this string reaches a terminal.
		if strings.Contains(errOut.String(), "resourceSpans") {
			t.Errorf("the report quotes the payload back:\n%s", errOut.String())
		}
	})

	t.Run("nothing listening", func(t *testing.T) {
		c := newCollector(t)
		enable(t, c)
		c.Close() // the URL stays valid and the port stops answering
		var errOut bytes.Buffer
		stop := Init(context.Background(), &errOut, "v0")
		_, span := Stage(context.Background(), "discover")
		span.End()
		stop()
		if errOut.Len() == 0 {
			t.Error("an unreachable collector was not reported at all")
		}
	})

	for _, tc := range []struct{ name, env, value string }{
		{"an endpoint that is not a URL", envTracesEndpoint, "http://[::1"},
		{"an endpoint with no scheme", envTracesEndpoint, "localhost:4318"},
		{"a scheme that is not http", envTracesEndpoint, "grpc://localhost:4317"},
		{"a protocol this build cannot speak", envProtocol, "grpc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvEnable, "1")
			t.Setenv(tc.env, tc.value)
			var errOut bytes.Buffer
			stop := Init(context.Background(), &errOut, "v0")
			if tracer != nil {
				t.Error("Init enabled telemetry on configuration it cannot honour")
			}
			_, span := Stage(context.Background(), "discover")
			span.End()
			stop()
			if errOut.Len() == 0 {
				t.Errorf("%s was rejected silently, so the run gets no telemetry and no "+
					"reason", tc.name)
			}
		})
	}
}

// Shutdown is called on paths where a command already failed, so it must be safe with a
// cancelled context and safe twice.
func TestShutdownIsBoundedAndIdempotent(t *testing.T) {
	c := newCollector(t)
	enable(t, c)
	var errOut bytes.Buffer
	stop := Init(context.Background(), &errOut, "v0")

	_, span := Stage(context.Background(), "discover")
	span.End()

	start := time.Now()
	stop()
	stop() // the second call is what a deferred stop plus an explicit one produces
	if d := time.Since(start); d > flushTimeout*3 {
		t.Errorf("Shutdown took %v, want well under %v", d, flushTimeout*3)
	}
	if c.count() == 0 {
		t.Error("Shutdown returned without flushing the pending span")
	}
	// After Shutdown, Stage is the no-op again: a span started on a torn-down provider
	// would otherwise be buffered by a processor nothing will ever flush.
	if _, s := Stage(context.Background(), "late"); s.span != nil {
		t.Error("Stage still starts spans after Shutdown")
	}
}
