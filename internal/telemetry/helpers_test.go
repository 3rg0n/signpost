package telemetry

// Shared reading of what a collector received. Every assertion in this package goes
// through these, so the payload is navigated as decoded JSON in one place rather than
// with a chain of type assertions per test.

import (
	"context"
	"testing"
)

// flush forces the pending batch out and waits for the collector to see it.
//
// A ForceFlush rather than a sleep: the batch processor's default schedule is 5 seconds,
// so a test that slept would either be slow or flaky depending on which side of that it
// guessed. The bounded wait afterwards covers the gap between the POST returning and the
// handler having appended the body.
func flush(t *testing.T) {
	t.Helper()
	if provider == nil {
		t.Fatal("flush called with telemetry off")
	}
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	if err := provider.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}
}

// spansIn returns every span across every payload the collector received, flattened out
// of the resource/scope grouping. Flattened because the grouping is asserted separately;
// a test about span content should not have to walk two levels to reach one.
func spansIn(t *testing.T, c *collector) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, p := range c.payloads(t) {
		for _, rs := range list(t, p["resourceSpans"]) {
			for _, ss := range list(t, rs["scopeSpans"]) {
				out = append(out, list(t, ss["spans"])...)
			}
		}
	}
	return out
}

// attrsOf indexes a span's attributes by key, with each value the decoded oneof — so a
// caller checks attrs["signpost.files"]["intValue"] and the test reads as the assertion
// it is.
func attrsOf(t *testing.T, span map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, a := range list(t, span["attributes"]) {
		v, ok := a["value"].(map[string]any)
		if !ok {
			t.Fatalf("attribute %v has no value object", a)
		}
		out[str(a["key"])] = v
	}
	return out
}

func list(t *testing.T, v any) []map[string]any {
	t.Helper()
	if v == nil {
		return nil
	}
	raw, ok := v.([]any)
	if !ok {
		t.Fatalf("%#v is not a JSON array", v)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, e := range raw {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("%#v is not a JSON object", e)
		}
		out = append(out, m)
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func names(spans []map[string]any) []string {
	out := make([]string, 0, len(spans))
	for _, s := range spans {
		out = append(out, str(s["name"]))
	}
	return out
}
