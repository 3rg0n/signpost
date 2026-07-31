package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAI is a Backend speaking the OpenAI chat-completions wire format over HTTP.
//
// One implementation covers Bedrock, Anthropic, vLLM, LiteLLM, and Ollama, because
// they all accept the same request shape at a different base URL. That is the reason
// this is the remote backend rather than a per-vendor set of them: the format is the
// portable part, and a vendor SDK would trade that portability for a dependency tree
// signpost has to patch (ADR 0002).
//
// Bedrock is worth naming precisely because its path is not the obvious one. The
// OpenAI-compatible surface lives at
//
//	https://bedrock-runtime.<region>.amazonaws.com/openai/v1
//
// and, on the newer Mantle endpoint,
//
//	https://bedrock-mantle.<region>.api.aws/v1
//
// both of which take `Authorization: Bearer <Bedrock API key>` — no SigV4 and no AWS
// SDK, which is what lets signpost reach Bedrock while carrying no dependencies. Note
// that a Bedrock API key is not an IAM access key: it is minted from an IAM principal
// and its use is gated by the `bedrock:CallWithBearerToken` action, so an account that
// can call Bedrock with SigV4 can still be denied the bearer-token path.
type OpenAI struct {
	// BaseURL is the API root, with or without a trailing "/v1". Required.
	BaseURL string

	// APIKey is sent as a bearer token. Empty is allowed: a local vLLM or Ollama
	// needs no credential, and requiring one would make the common local case
	// awkward to configure.
	APIKey string

	// Model is the model id, passed through verbatim. Required.
	//
	// Verbatim matters more than it looks. Bedrock's OpenAI-compatible path accepts
	// `google.gemma-3-12b-it` and rejects both the `:0`-suffixed Invoke-style id and
	// the `global.`-prefixed cross-region profile id, so any normalisation here would
	// silently rewrite a working id into a 400.
	Model string

	// HTTPClient is the transport. Nil means a client with a timeout, because the
	// default http.Client has none and a hung backend would stall the whole run
	// rather than failing open.
	HTTPClient *http.Client

	// Version is signpost's version, for Actor.
	Version string
}

// DefaultTimeout bounds one model call.
//
// Generous because a schema-constrained summary from a small model on a busy shared
// endpoint is slow, not because anything here is expected to take a minute. The point
// is that some bound exists: §5's fail-open promise is worthless if an unreachable
// backend hangs instead of erroring.
const DefaultTimeout = 90 * time.Second

// Actor implements Backend.
func (o *OpenAI) Actor() string {
	v := o.Version
	if v == "" {
		v = "dev"
	}
	return "signpost/" + v + "+" + o.Model
}

// Complete implements Backend.
//
// The response is checked to be valid JSON before it is returned, but not checked
// against Schema. That split is deliberate: server-side schema enforcement varies by
// model — the request field is validated (an unknown `response_format.type` is a 400)
// yet a model with a reasoning channel can still wrap the constrained object in prose
// — so the only honest guarantee at this layer is "parses as a JSON object". The
// claim-level check that matters is the grounding rule at emit time, which drops a
// claim whose citation does not resolve regardless of how well-shaped it was.
func (o *OpenAI) Complete(ctx context.Context, req Request) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}
	if o.BaseURL == "" {
		return Result{}, unavailablef("no base URL is configured for the openai backend")
	}
	if o.Model == "" {
		return Result{}, errors.New("model: no model id is configured for the openai backend")
	}

	body, err := json.Marshal(o.request(req))
	if err != nil {
		return Result{}, fmt.Errorf("model: encoding the request: %w", err)
	}

	url := chatCompletionsURL(o.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("model: building the request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	resp, err := o.client().Do(httpReq)
	if err != nil {
		// A transport error is the unreachable case: DNS, refused, TLS, timeout.
		// Nothing was answered, so this is fail-open territory rather than a fault.
		return Result{}, unavailablef("%s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Capped because the body is attacker-adjacent — a compromised or misconfigured
	// endpoint should not be able to exhaust memory on a machine running a build.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Result{}, unavailablef("reading the response from %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, o.statusError(resp.StatusCode, raw)
	}

	return parseChatCompletion(raw)
}

// maxResponseBytes caps a response body. A schema-constrained summary is kilobytes;
// eight megabytes is far past any legitimate answer and still small enough to hold.
const maxResponseBytes = 8 << 20

func (o *OpenAI) client() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return &http.Client{Timeout: DefaultTimeout}
}

// statusError turns a non-200 into either an unavailable error or a fault.
//
// The distinction is the one §5 needs. 401, 403, and 404 mean this backend will not
// serve this caller — a missing token, a denied action, a wrong base URL — and none
// of them get better by retrying, so they fail open like an unreachable daemon. 429
// and 5xx are the endpoint being busy or broken, which is also not signpost's fault
// and also fails open. What is left, chiefly 400, means signpost sent something the
// endpoint rejected: a bad model id or an unsupported field. That is a real bug and
// swallowing it would hide it behind a bundle that quietly has no semantic pages.
func (o *OpenAI) statusError(code int, raw []byte) error {
	detail := apiErrorMessage(raw)
	switch {
	case code == http.StatusUnauthorized, code == http.StatusForbidden,
		code == http.StatusNotFound, code == http.StatusTooManyRequests,
		code >= 500:
		return unavailablef("%s returned %d: %s", o.Model, code, detail)
	default:
		return fmt.Errorf("model: %s rejected the request with %d: %s", o.Model, code, detail)
	}
}

// chatCompletionsURL joins a base URL to the chat-completions path.
//
// Tolerating both "…/v1" and "…" is not politeness, it is the shape of the real
// configuration: AWS documents OPENAI_BASE_URL with the /v1 included, while other
// deployments document the bare host. Guessing wrong produces a 404 that reads like a
// dead endpoint, so both spellings are accepted and neither is doubled.
func chatCompletionsURL(base string) string {
	b := strings.TrimRight(base, "/")
	if strings.HasSuffix(b, "/chat/completions") {
		return b
	}
	if !strings.HasSuffix(b, "/v1") {
		b += "/v1"
	}
	return b + "/chat/completions"
}

// request is the wire body.
//
// max_completion_tokens rather than max_tokens: the latter is deprecated in the
// OpenAI format and rejected outright by some endpoints, and this is a new client
// with no legacy callers to keep working.
func (o *OpenAI) request(req Request) map[string]any {
	messages := make([]map[string]string, 0, 2)
	if req.System != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.System})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.User})

	body := map[string]any{
		"model":    o.Model,
		"messages": messages,
		// Zero, not merely low. Two runs at the same commit must produce the same
		// bundle (§4.6 byte-stability), and sampling temperature is the one knob that
		// would make an unchanged repository emit a changed page.
		"temperature": 0,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name": req.schemaName(),
				// strict is what makes the schema a constraint rather than a hint on
				// endpoints that honour it. Endpoints that do not honour it ignore
				// the field, which is why Complete still validates the body.
				"strict": true,
				"schema": req.Schema,
			},
		},
	}
	if req.MaxTokens > 0 {
		body["max_completion_tokens"] = req.MaxTokens
	}
	return body
}

// chatCompletion is the subset of the response signpost reads.
type chatCompletion struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func parseChatCompletion(raw []byte) (Result, error) {
	var cc chatCompletion
	if err := json.Unmarshal(raw, &cc); err != nil {
		return Result{}, fmt.Errorf("model: the response was not chat-completions JSON: %w", err)
	}
	if len(cc.Choices) == 0 {
		return Result{}, errors.New("model: the response carried no choices")
	}
	choice := cc.Choices[0]
	if choice.Message.Refusal != "" {
		return Result{}, fmt.Errorf("model: the model refused: %s", choice.Message.Refusal)
	}
	// A truncated response is reported rather than parsed. It is usually valid JSON
	// as far as it goes and a partial summary committed as if complete is exactly the
	// confidently-wrong output the design refuses to emit.
	if choice.FinishReason == "length" {
		return Result{}, errors.New("model: the response hit the token limit before it finished")
	}

	content, err := jsonObject(choice.Message.Content)
	if err != nil {
		return Result{}, err
	}
	return Result{
		JSON:         content,
		InputTokens:  cc.Usage.PromptTokens,
		OutputTokens: cc.Usage.CompletionTokens,
	}, nil
}

// jsonObject extracts the JSON object from a message body.
//
// This exists because "schema-constrained" is not the same claim on every model. A
// model with a reasoning channel — gpt-oss is the one to know about — emits its
// reasoning trace into the same `content` string as the answer, so a strict
// json_schema request comes back as `<reasoning>…</reasoning>{"role":…}` and a plain
// json.Unmarshal of the field fails on a response that did in fact honour the schema.
//
// So the object is located rather than assumed: content is used directly when it
// parses, and otherwise the outermost balanced brace span is tried. This is recovery,
// not permissiveness — the result must still parse as a JSON object, and a model that
// wraps its answer in prose is noted in the error when it does not.
func jsonObject(content string) ([]byte, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil, errors.New("model: the response was empty")
	}
	if isJSONObject(trimmed) {
		return []byte(trimmed), nil
	}
	if span, ok := outermostObject(trimmed); ok && isJSONObject(span) {
		return []byte(span), nil
	}
	return nil, fmt.Errorf("model: the response was not a JSON object despite a schema constraint: %s",
		truncate(trimmed, 200))
}

func isJSONObject(s string) bool {
	var v map[string]any
	return json.Unmarshal([]byte(s), &v) == nil
}

// outermostObject returns the span from the first "{" to the last "}".
//
// Byte-scanning rather than brace-counting on purpose: counting braces without
// tracking string literals and escapes is wrong on any object containing a "}" inside
// a string, and tracking them properly is re-implementing the parser that
// json.Unmarshal already is. Taking the widest plausible span and letting the decoder
// rule on it keeps the one real parser in one place.
func outermostObject(s string) (string, bool) {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return "", false
	}
	return s[start : end+1], true
}

// apiErrorMessage pulls the human-readable part out of an error body.
//
// Both shapes are real and both come from the same vendor: Bedrock's OpenAI-compatible
// path returns `{"error":{"message":…}}`, and for some validation failures the message
// is itself a JSON-encoded error document. The nested case is not unwrapped further —
// one level of readability is the goal, not a general-purpose unwrapper.
func apiErrorMessage(raw []byte) string {
	var body struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &body); err == nil {
		if body.Error.Message != "" {
			return truncate(body.Error.Message, 400)
		}
		if body.Message != "" {
			return truncate(body.Message, 400)
		}
	}
	return truncate(strings.TrimSpace(string(raw)), 400)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
