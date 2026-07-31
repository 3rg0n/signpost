package model

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// roleSchema is a stand-in for a real semantic-pass schema, small enough to read.
var roleSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"role": map[string]any{"type": "string"},
	},
	"required":             []string{"role"},
	"additionalProperties": false,
}

func testRequest() Request {
	return Request{
		System: SystemPrompt,
		User:   "Summarise internal/vcs.",
		Schema: roleSchema,
	}
}

// chatResponse builds a minimal chat-completions body around one content string.
func chatResponse(content, finish string) string {
	body := map[string]any{
		"choices": []any{map[string]any{
			"finish_reason": finish,
			"message":       map[string]any{"content": content},
		}},
		"usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 7},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// serve stands up a fake endpoint and returns a backend pointed at it, plus a pointer
// to the last request body it received.
func serve(t *testing.T, handler http.HandlerFunc) (*OpenAI, *[]byte) {
	t.Helper()
	var last []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last, _ = io.ReadAll(r.Body)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return &OpenAI{BaseURL: srv.URL, Model: "test-model", Version: "0.1.0"}, &last
}

func TestOpenAICompleteReturnsJSON(t *testing.T) {
	b, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(chatResponse(`{"role":"reads git history"}`, "stop")))
	})

	got, err := b.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if want := `{"role":"reads git history"}`; string(got.JSON) != want {
		t.Errorf("JSON = %q, want %q", got.JSON, want)
	}
	if got.InputTokens != 12 || got.OutputTokens != 7 {
		t.Errorf("usage = %d/%d, want 12/7", got.InputTokens, got.OutputTokens)
	}
}

// The request has to carry the schema and a zero temperature. Both are load-bearing:
// the schema is what constrains the output shape (§4.5), and temperature 0 is what
// makes two runs at the same commit produce the same bundle (§4.6). A regression in
// either is invisible in the response, so it is asserted on the wire.
func TestOpenAIRequestCarriesSchemaAndZeroTemperature(t *testing.T) {
	b, last := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(chatResponse(`{"role":"x"}`, "stop")))
	})
	if _, err := b.Complete(context.Background(), testRequest()); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var sent struct {
		Model          string `json:"model"`
		Temperature    *int   `json:"temperature"`
		ResponseFormat struct {
			Type       string `json:"type"`
			JSONSchema struct {
				Name   string         `json:"name"`
				Strict bool           `json:"strict"`
				Schema map[string]any `json:"schema"`
			} `json:"json_schema"`
		} `json:"response_format"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(*last, &sent); err != nil {
		t.Fatalf("decoding the sent body: %v", err)
	}
	if sent.Model != "test-model" {
		t.Errorf("model = %q, want the configured id verbatim", sent.Model)
	}
	if sent.Temperature == nil || *sent.Temperature != 0 {
		t.Errorf("temperature = %v, want 0 for byte-stability", sent.Temperature)
	}
	if sent.ResponseFormat.Type != "json_schema" || !sent.ResponseFormat.JSONSchema.Strict {
		t.Errorf("response_format = %+v, want a strict json_schema", sent.ResponseFormat)
	}
	if sent.ResponseFormat.JSONSchema.Schema["type"] != "object" {
		t.Errorf("schema was not passed through: %+v", sent.ResponseFormat.JSONSchema.Schema)
	}
	if len(sent.Messages) != 2 || sent.Messages[0].Role != "system" || sent.Messages[1].Role != "user" {
		t.Errorf("messages = %+v, want a system turn then a user turn", sent.Messages)
	}
}

// A model with a reasoning channel puts its trace in the same content field as the
// answer. gpt-oss on Bedrock does exactly this, so the object is located rather than
// assumed — otherwise a response that honoured the schema is reported as one that did
// not.
func TestOpenAICompleteRecoversObjectFromReasoningPreamble(t *testing.T) {
	content := "<reasoning>The user wants a summary.</reasoning>**Package{ \"role\": \"reads git history\" }"
	b, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(chatResponse(content, "stop")))
	})

	got, err := b.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(got.JSON, &parsed); err != nil {
		t.Fatalf("the recovered span did not parse: %v", err)
	}
	if parsed["role"] != "reads git history" {
		t.Errorf("recovered %v, want the role field", parsed)
	}
}

// Truncation is a failure, not a partial success. A summary cut off at the token limit
// is often still valid JSON, and committing it as complete is the confidently-wrong
// output the design refuses to emit.
func TestOpenAICompleteRejectsTruncatedResponse(t *testing.T) {
	b, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(chatResponse(`{"role":"reads git`, "length")))
	})

	_, err := b.Complete(context.Background(), testRequest())
	if err == nil {
		t.Fatal("Complete accepted a truncated response")
	}
	if !strings.Contains(err.Error(), "token limit") {
		t.Errorf("error = %v, want it to name the token limit", err)
	}
}

func TestOpenAICompleteRejectsProse(t *testing.T) {
	b, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(chatResponse("It reads git history.", "stop")))
	})

	if _, err := b.Complete(context.Background(), testRequest()); err == nil {
		t.Fatal("Complete accepted prose where a JSON object was required")
	}
}

// The status split is what makes fail-open work (§5). A denied or missing endpoint is
// unavailable — the deterministic bundle still ships. A rejected request is a fault,
// because swallowing it would hide a bad model id behind a bundle that quietly has no
// semantic pages.
func TestOpenAIStatusCodesSplitUnavailableFromFault(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		unavailable bool
	}{
		{"no credential", http.StatusUnauthorized, true},
		{"action denied", http.StatusForbidden, true},
		{"wrong base url", http.StatusNotFound, true},
		{"throttled", http.StatusTooManyRequests, true},
		{"endpoint broken", http.StatusInternalServerError, true},
		{"request rejected", http.StatusBadRequest, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
			})
			_, err := b.Complete(context.Background(), testRequest())
			if err == nil {
				t.Fatalf("status %d produced no error", tc.status)
			}
			if got := errors.Is(err, ErrUnavailable); got != tc.unavailable {
				t.Errorf("errors.Is(err, ErrUnavailable) = %v, want %v (err: %v)",
					got, tc.unavailable, err)
			}
			if !strings.Contains(err.Error(), "nope") {
				t.Errorf("error = %v, want the endpoint's message included", err)
			}
		})
	}
}

// An unreachable endpoint has to be unavailable rather than a fault, because that is
// the case fail-open exists for: no daemon, no network, wrong host.
func TestOpenAIUnreachableIsUnavailable(t *testing.T) {
	b := &OpenAI{BaseURL: "http://127.0.0.1:1", Model: "test-model"}
	_, err := b.Complete(context.Background(), testRequest())
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestOpenAIRejectsRequestWithoutSchema(t *testing.T) {
	b := &OpenAI{BaseURL: "http://example.invalid", Model: "test-model"}
	_, err := b.Complete(context.Background(), Request{User: "hi"})
	if err == nil {
		t.Fatal("a request with no schema was accepted")
	}
	if errors.Is(err, ErrUnavailable) {
		t.Error("a malformed request should be a fault, not an unavailable backend")
	}
}

// Both spellings of a base URL are real: AWS documents OPENAI_BASE_URL with /v1
// included, other deployments document the bare host. Guessing wrong is a 404 that
// reads like a dead endpoint.
func TestChatCompletionsURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1",
			"https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1/chat/completions"},
		{"https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1/",
			"https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1/chat/completions"},
		{"http://localhost:11434", "http://localhost:11434/v1/chat/completions"},
		{"http://localhost:8000/v1/chat/completions", "http://localhost:8000/v1/chat/completions"},
	}
	for _, tc := range cases {
		if got := chatCompletionsURL(tc.in); got != tc.want {
			t.Errorf("chatCompletionsURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestOpenAIActor(t *testing.T) {
	b := &OpenAI{Model: "google.gemma-3-12b-it", Version: "0.1.0"}
	if want := "signpost/0.1.0+google.gemma-3-12b-it"; b.Actor() != want {
		t.Errorf("Actor() = %q, want %q", b.Actor(), want)
	}
}
