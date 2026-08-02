package telemetry

// The exporter's configuration surface: the OTEL_* variables an operator already has set
// for their collector. Each of these has a wrong reading that produces a payload posted to
// the wrong place or with the wrong credential, and neither failure is visible from the
// run — a collector that never receives anything looks exactly like a tool that never
// sent anything.

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTheBodyIsAnnouncedAsJSON(t *testing.T) {
	c := newCollector(t)
	enable(t, c)
	initFor(t)
	_, span := Stage(context.Background(), "discover")
	span.End()
	flush(t)

	// Not a detail: OTLP over HTTP defaults to protobuf, so a collector reads this header
	// to decide which parser to hand the body to. Omit it and a payload this exporter
	// spent the trouble to encode as JSON is rejected as a malformed protobuf.
	if got := c.contentType(t); got != "application/json" {
		t.Errorf("Content-Type is %q, want application/json", got)
	}
}

func TestHeadersFromTheEnvironmentReachTheCollector(t *testing.T) {
	c := newCollector(t)
	enable(t, c)
	// Percent-encoded, per the W3C Baggage encoding the OTLP specification points at. A
	// token with a `=` or a comma in it cannot be sent any other way, since both are the
	// format's own separators.
	t.Setenv(envHeaders, "authorization=Bearer%20abc%3D%3D,x-scope-orgid=team-a")
	initFor(t)
	_, span := Stage(context.Background(), "discover")
	span.End()
	flush(t)

	if got := c.header(t, "Authorization"); got != "Bearer abc==" {
		t.Errorf("Authorization is %q, want %q — the value must be percent-decoded, or a "+
			"credential arrives with %%20 in it and the collector rejects every batch",
			got, "Bearer abc==")
	}
	if got := c.header(t, "X-Scope-OrgID"); got != "team-a" {
		t.Errorf("X-Scope-OrgID is %q, want team-a", got)
	}
}

// Endpoint resolution, including the two boundaries that decide whether a payload lands
// on a route the collector serves.
func TestEndpointResolution(t *testing.T) {
	for _, tc := range []struct {
		name            string
		generic, signal string
		want            string
	}{
		{
			name: "neither set",
			want: "http://localhost:4318/v1/traces",
		},
		{
			// The generic variable names the collector, not the route, so the signal's
			// path is appended. Posting to the bare host is a 404 on every collector.
			name:    "generic gets the traces path",
			generic: "http://collector:4318",
			want:    "http://collector:4318/v1/traces",
		},
		{
			// A trailing slash is the common spelling and must not produce `//v1/traces`,
			// which some collectors route differently or not at all.
			name:    "a trailing slash does not double",
			generic: "http://collector:4318/",
			want:    "http://collector:4318/v1/traces",
		},
		{
			// A base path is kept: this is how a collector behind a reverse proxy is
			// addressed, and dropping it posts to a route the proxy does not serve.
			name:    "a base path is preserved",
			generic: "https://gateway.example/otlp",
			want:    "https://gateway.example/otlp/v1/traces",
		},
		{
			// The signal-specific variable is a full URL used as given. Appending to it
			// produces `/v1/traces/v1/traces`, and it is also the only way to point
			// traces at a path that is not /v1/traces at all.
			name:   "the signal-specific endpoint is used verbatim",
			signal: "http://collector:4318/custom/traces",
			want:   "http://collector:4318/custom/traces",
		},
		{
			name:    "the signal-specific endpoint wins",
			generic: "http://wrong:4318",
			signal:  "http://right:4318/v1/traces",
			want:    "http://right:4318/v1/traces",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envEndpoint, tc.generic)
			t.Setenv(envTracesEndpoint, tc.signal)
			got, err := resolveEndpoint()
			if err != nil {
				t.Fatalf("resolveEndpoint: %v", err)
			}
			if got != tc.want {
				t.Errorf("endpoint is %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTimeoutResolution(t *testing.T) {
	for _, tc := range []struct {
		name            string
		generic, signal string
		want            time.Duration
	}{
		{name: "unset", want: defaultTimeout},
		{name: "milliseconds", generic: "2500", want: 2500 * time.Millisecond},
		{name: "the signal-specific value wins", generic: "9000", signal: "1000",
			want: time.Second},
		// A timeout is not worth failing a run over, so a value that is not a positive
		// integer falls back rather than turning telemetry off. The boundary matters
		// because zero would otherwise mean "no timeout" to http.Client — an unreachable
		// collector would then hang the flush until its own bound expired.
		{name: "not a number", generic: "10s", want: defaultTimeout},
		{name: "zero", generic: "0", want: defaultTimeout},
		{name: "negative", generic: "-1", want: defaultTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envTimeout, tc.generic)
			t.Setenv(envTracesTimeout, tc.signal)
			if got := resolveTimeout(); got != tc.want {
				t.Errorf("timeout is %v, want %v", got, tc.want)
			}
		})
	}
}

// Per-signal headers replace the generic ones rather than merging, which the
// specification requires and is also the safer reading: merging would send a credential
// the operator deliberately scoped to another signal.
func TestPerSignalHeadersReplaceRatherThanMerge(t *testing.T) {
	t.Setenv(envHeaders, "authorization=metrics-token,x-shared=yes")
	t.Setenv(envTracesHeaders, "authorization=traces-token")
	got := resolveHeaders()
	if got["authorization"] != "traces-token" {
		t.Errorf("authorization is %q, want traces-token", got["authorization"])
	}
	if v, ok := got["x-shared"]; ok {
		t.Errorf("x-shared=%q leaked in from the generic variable; the per-signal value "+
			"replaces the whole set", v)
	}
}

// A malformed entry is skipped and the rest are kept. Dropping every header because one
// pair had no `=` would silently unauthenticate an exporter that was working.
func TestAMalformedHeaderEntryDoesNotDiscardTheRest(t *testing.T) {
	t.Setenv(envHeaders, "authorization=token,garbage,=novalue, x-ok = spaced ")
	got := resolveHeaders()
	if got["authorization"] != "token" {
		t.Errorf("authorization is %q, want token", got["authorization"])
	}
	if got["x-ok"] != "spaced" {
		t.Errorf("x-ok is %q, want %q — keys and values are trimmed", got["x-ok"], "spaced")
	}
	if len(got) != 2 {
		t.Errorf("%d header(s), want 2: %v", len(got), got)
	}
}

// After Shutdown, ExportSpans is a no-op returning nil rather than an error. The batch
// processor can drain a queue after shutting the exporter down, and an error there is
// reported to the user as an export failure that did not happen.
func TestExportAfterShutdownIsSilent(t *testing.T) {
	c := newCollector(t)
	e, err := newExporter()
	if err != nil {
		t.Fatal(err)
	}
	e.endpoint = c.URL + tracesPath

	if err := e.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := e.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
	if err := e.ExportSpans(context.Background(), nil); err != nil {
		t.Errorf("ExportSpans after Shutdown returned %v, want nil", err)
	}
	if n := c.count(); n != 0 {
		t.Errorf("%d payload(s) posted after Shutdown", n)
	}
}

// The protocol check is a refusal with a name, not a 400 from the collector.
func TestOnlyHTTPJSONIsClaimed(t *testing.T) {
	for _, tc := range []struct {
		value string
		ok    bool
	}{
		{"", true},
		{"http/json", true},
		{"grpc", false},
		{"http/protobuf", false},
	} {
		t.Run("protocol="+tc.value, func(t *testing.T) {
			t.Setenv(envProtocol, tc.value)
			err := checkProtocol()
			if (err == nil) != tc.ok {
				t.Fatalf("checkProtocol() = %v, want ok=%v", err, tc.ok)
			}
			if err != nil && !strings.Contains(err.Error(), "http/json") {
				t.Errorf("the refusal does not say what this build speaks: %v", err)
			}
		})
	}
}
