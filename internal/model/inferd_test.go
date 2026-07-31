package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// frames encodes a response stream the way the daemon would.
func frames(t *testing.T, payloads ...any) *bufio.Reader {
	t.Helper()
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	for _, p := range payloads {
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("encoding a test frame: %v", err)
		}
		if err := writeFrame(w, frameJSON, raw); err != nil {
			t.Fatalf("writeFrame: %v", err)
		}
	}
	return bufio.NewReader(&buf)
}

func textFrame(delta string) map[string]any {
	return map[string]any{
		"type":  "frame",
		"block": map[string]any{"type": "text", "delta": delta},
	}
}

func TestReadGenerationAssemblesTextDeltas(t *testing.T) {
	r := frames(t,
		textFrame(`{"role":`),
		textFrame(`"reads git history"}`),
		map[string]any{
			"type":        "done",
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 40, "output_tokens": 9},
		},
	)

	got, err := readGeneration(r, "test")
	if err != nil {
		t.Fatalf("readGeneration: %v", err)
	}
	if want := `{"role":"reads git history"}`; string(got.JSON) != want {
		t.Errorf("JSON = %q, want %q", got.JSON, want)
	}
	if got.InputTokens != 40 || got.OutputTokens != 9 {
		t.Errorf("usage = %d/%d, want 40/9", got.InputTokens, got.OutputTokens)
	}
}

// A reasoning trace arrives as its own block type. Concatenating it into the answer is
// the bug the protocol separates block types to prevent, so thinking deltas are dropped
// rather than folded in.
func TestReadGenerationDropsThinkingDeltas(t *testing.T) {
	r := frames(t,
		map[string]any{
			"type":  "frame",
			"block": map[string]any{"type": "thinking", "delta": "The user wants {a summary}."},
		},
		textFrame(`{"role":"x"}`),
		map[string]any{"type": "done", "stop_reason": "end_turn"},
	)

	got, err := readGeneration(r, "test")
	if err != nil {
		t.Fatalf("readGeneration: %v", err)
	}
	if want := `{"role":"x"}`; string(got.JSON) != want {
		t.Errorf("JSON = %q, want %q — a thinking delta leaked into the answer", got.JSON, want)
	}
}

func TestReadGenerationRejectsTruncatedGeneration(t *testing.T) {
	r := frames(t,
		textFrame(`{"role":"reads git`),
		map[string]any{"type": "done", "stop_reason": "max_tokens"},
	)

	_, err := readGeneration(r, "test")
	if err == nil {
		t.Fatal("a max_tokens generation was accepted")
	}
	if !strings.Contains(err.Error(), "token limit") {
		t.Errorf("err = %v, want it to name the token limit", err)
	}
}

// Unknown response types are additive in the protocol, so one is not a reason to fail a
// request that still terminates normally.
func TestReadGenerationIgnoresUnknownResponseTypes(t *testing.T) {
	r := frames(t,
		map[string]any{"type": "some_future_type", "block": map[string]any{"type": "text", "delta": "ignored"}},
		textFrame(`{"role":"x"}`),
		map[string]any{"type": "done", "stop_reason": "end_turn"},
	)

	got, err := readGeneration(r, "test")
	if err != nil {
		t.Fatalf("readGeneration: %v", err)
	}
	if want := `{"role":"x"}`; string(got.JSON) != want {
		t.Errorf("JSON = %q, want %q", got.JSON, want)
	}
}

// A stream that ends without a terminal frame is the daemon dying mid-generation, which
// fails open — a build should not break because a local daemon crashed.
func TestReadGenerationTreatsMissingTerminalFrameAsUnavailable(t *testing.T) {
	r := frames(t, textFrame(`{"role":"x"}`))

	_, err := readGeneration(r, "test")
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestReadGenerationRejectsBlobFrame(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := writeFrame(w, frameBlob, []byte("raw")); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}

	_, err := readGeneration(bufio.NewReader(&buf), "test")
	if err == nil {
		t.Fatal("a blob frame on the response stream was accepted")
	}
	if errors.Is(err, ErrUnavailable) {
		t.Error("a protocol violation should be a fault, not an unavailable backend")
	}
}

// The error-code split is fail-open (§5) versus fault. queue_full and friends are the
// daemon declining to serve; invalid_request means signpost sent something wrong, and
// hiding that behind a bundle with no semantic pages would hide a bug.
func TestInferdErrorCodesSplitUnavailableFromFault(t *testing.T) {
	cases := []struct {
		code        string
		unavailable bool
	}{
		{"queue_full", true},
		{"backend_unavailable", true},
		{"internal", true},
		{"wire_version_unsupported", true},
		{"invalid_request", false},
		{"tool_call_malformed", false},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			r := frames(t, map[string]any{
				"type":    "error",
				"code":    tc.code,
				"message": "detail here",
			})
			_, err := readGeneration(r, "test")
			if err == nil {
				t.Fatalf("code %q produced no error", tc.code)
			}
			if got := errors.Is(err, ErrUnavailable); got != tc.unavailable {
				t.Errorf("errors.Is(err, ErrUnavailable) = %v, want %v (err: %v)",
					got, tc.unavailable, err)
			}
			if !strings.Contains(err.Error(), "detail here") {
				t.Errorf("err = %v, want the daemon's message included", err)
			}
		})
	}
}

// The request has to carry wire_version 1, a schema, no streaming, and temperature 0. A
// missing wire_version is a terminal error frame from the daemon, and the other three
// are the guarantees the design rests on, so they are asserted on the encoded body.
func TestInferdRequestShape(t *testing.T) {
	raw, err := json.Marshal(inferdRequest(Request{
		System:    SystemPrompt,
		User:      "Summarise internal/vcs.",
		Schema:    roleSchema,
		MaxTokens: 512,
	}))
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	var sent struct {
		WireVersion    int   `json:"wire_version"`
		Stream         *bool `json:"stream"`
		Temperature    *int  `json:"temperature"`
		MaxTokens      int   `json:"max_tokens"`
		ResponseFormat struct {
			Type   string         `json:"type"`
			Schema map[string]any `json:"schema"`
		} `json:"response_format"`
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
		Thinking any `json:"thinking"`
	}
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	if sent.WireVersion != wireVersion {
		t.Errorf("wire_version = %d, want %d", sent.WireVersion, wireVersion)
	}
	if sent.Stream == nil || *sent.Stream {
		t.Errorf("stream = %v, want an explicit false", sent.Stream)
	}
	if sent.Temperature == nil || *sent.Temperature != 0 {
		t.Errorf("temperature = %v, want 0 for byte-stability", sent.Temperature)
	}
	if sent.MaxTokens != 512 {
		t.Errorf("max_tokens = %d, want 512", sent.MaxTokens)
	}
	if sent.ResponseFormat.Type != "json_schema" || sent.ResponseFormat.Schema["type"] != "object" {
		t.Errorf("response_format = %+v, want a json_schema carrying the schema", sent.ResponseFormat)
	}
	if sent.Thinking != nil {
		t.Errorf("thinking = %v, want it omitted", sent.Thinking)
	}
	if len(sent.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(sent.Messages))
	}
	if sent.Messages[0].Role != "system" || sent.Messages[1].Role != "user" {
		t.Errorf("roles = %q/%q, want system/user", sent.Messages[0].Role, sent.Messages[1].Role)
	}
	// Content is a block array, not a bare string: the v2 protocol requires it and a
	// string would be an invalid_request the daemon rejects.
	if len(sent.Messages[1].Content) != 1 || sent.Messages[1].Content[0].Type != "text" {
		t.Errorf("user content = %+v, want one text block", sent.Messages[1].Content)
	}
	if sent.Messages[1].Content[0].Text != "Summarise internal/vcs." {
		t.Errorf("user text = %q, want the prompt verbatim", sent.Messages[1].Content[0].Text)
	}
}

// No daemon is the ordinary case on a machine that has not installed one, and it has to
// fail open rather than fail the build.
func TestInferdCompleteWithNoDaemonIsUnavailable(t *testing.T) {
	i := &Inferd{Addr: filepathNotThere(t)}
	_, err := i.Complete(context.Background(), Request{User: "hi", Schema: roleSchema})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

// filepathNotThere is a path in a fresh temp dir, so nothing is listening on it.
func filepathNotThere(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "/absent.sock"
}

func TestInferdActor(t *testing.T) {
	cases := []struct {
		in   Inferd
		want string
	}{
		{Inferd{Model: "gemma-3-12b", Version: "0.1.0"}, "signpost/0.1.0+gemma-3-12b"},
		{Inferd{}, "signpost/dev+inferd"},
	}
	for _, tc := range cases {
		if got := tc.in.Actor(); got != tc.want {
			t.Errorf("Actor() = %q, want %q", got, tc.want)
		}
	}
}

func TestDefaultInferdAddrIsNotEmpty(t *testing.T) {
	if DefaultInferdAddr() == "" {
		t.Error("DefaultInferdAddr() is empty, so a default-configured backend cannot dial")
	}
}
