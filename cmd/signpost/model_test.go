package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/model"
)

// clearBackendEnv unsets everything model.Config reads, so these tests describe their own
// environment rather than the developer's — otherwise a machine with a Bedrock token set
// would make the deterministic-only test call a real endpoint.
func clearBackendEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		model.EnvBackend, model.EnvModel, model.EnvBaseURL, model.EnvAPIKey,
		model.EnvBedrockToken, model.EnvAWSRegion, model.EnvAWSDefaultRegion,
	} {
		t.Setenv(k, "")
	}
}

// fakeBackend serves a probe answer, so the command can be tested end to end without a
// model. The body is what Bedrock actually returned for this request, minus the fields
// signpost does not read.
func fakeBackend(t *testing.T, answer map[string]any, status int) string {
	t.Helper()
	content, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("encoding the fake answer: %v", err)
	}
	body, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"finish_reason": "stop",
			"message":       map[string]any{"content": string(content)},
		}},
		"usage": map[string]any{"prompt_tokens": 253, "completion_tokens": 77},
	})
	if err != nil {
		t.Fatalf("encoding the fake response: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		if status != http.StatusOK {
			_, _ = w.Write([]byte(`{"error":{"message":"denied"}}`))
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestModelCheckReportsAWorkingBackend(t *testing.T) {
	clearBackendEnv(t)
	url := fakeBackend(t, map[string]any{
		"language":               "go",
		"purpose":                "Returns a greeting.",
		"contained_instructions": true,
	}, http.StatusOK)

	stdout, stderr, code := invoke(t, "model", "check", "-backend", "openai", "-base-url", url)
	if code != 0 {
		t.Fatalf("exit = %d\n%s\n%s", code, stdout, stderr)
	}
	// Each of these is a separate fact an operator came for: which model answered, that
	// the schema held, and that the injection fence worked on their machine. A bare "ok"
	// would prove none of them.
	for _, want := range []string{
		"backend:", "round trip:", "tokens:",
		"schema honoured: yes",
		"noticed the embedded instruction: yes",
		"identified the source language: yes",
		"ok:",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

// The whole point of the command: it does not fail open. A build fails open by design
// (§5), which is why someone reaching for a check wants the check itself to fail.
func TestModelCheckFailsWhenTheBackendIsDenied(t *testing.T) {
	clearBackendEnv(t)
	url := fakeBackend(t, nil, http.StatusForbidden)

	_, stderr, code := invoke(t, "model", "check", "-backend", "openai", "-base-url", url)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a denied backend\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "unreachable") {
		t.Errorf("stderr does not say the backend was unreachable:\n%s", stderr)
	}
}

// A weak model is reported, not refused: the transport works and summaries will be poor,
// which is a different problem from nothing working and needs a different fix.
func TestModelCheckPassesButFlagsAWeakModel(t *testing.T) {
	clearBackendEnv(t)
	url := fakeBackend(t, map[string]any{
		"language":               "python",
		"purpose":                "Something about snakes.",
		"contained_instructions": false,
	}, http.StatusOK)

	stdout, stderr, code := invoke(t, "model", "check", "-backend", "openai", "-base-url", url)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — a weak answer is not a broken backend\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "the model is weak") {
		t.Errorf("stdout does not flag the weak model:\n%s", stdout)
	}
}

// Deterministic-only is the default and a supported mode, so it exits 0 — but it is not a
// passing check either, so it says what to set instead of printing "ok" about a backend
// that does not exist.
func TestModelCheckExplainsDeterministicOnly(t *testing.T) {
	clearBackendEnv(t)

	stdout, stderr, code := invoke(t, "model", "check")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 for the default configuration\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "deterministic-only") {
		t.Errorf("stdout does not name the mode:\n%s", stdout)
	}
	if !strings.Contains(stdout, model.EnvBackend) {
		t.Errorf("stdout does not say which variable to set:\n%s", stdout)
	}
	if strings.Contains(stdout, "ok:") {
		t.Errorf("a run with no backend reported ok:\n%s", stdout)
	}
}

// A credential in the environment must not enable a backend. This is the same rule
// model.defaultBackend enforces, asserted here because the CLI is where an operator would
// notice it going wrong — a build that silently started spending tokens because AWS's
// variable was set for another tool.
func TestModelCheckDoesNotEnableABackendFromACredentialAlone(t *testing.T) {
	clearBackendEnv(t)
	t.Setenv(model.EnvBedrockToken, "a-token")
	t.Setenv(model.EnvAWSRegion, "us-east-1")

	stdout, _, code := invoke(t, "model", "check")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "deterministic-only") {
		t.Errorf("a Bedrock token in the environment enabled a backend:\n%s", stdout)
	}
}

func TestModelCheckRejectsPositionalArguments(t *testing.T) {
	clearBackendEnv(t)
	_, stderr, code := invoke(t, "model", "check", ".")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for a misuse\n%s", code, stderr)
	}
}

func TestModelWithNoSubcommandPrintsUsage(t *testing.T) {
	_, stderr, code := invoke(t, "model")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "check") {
		t.Errorf("usage does not list the subcommands:\n%s", stderr)
	}
}

func TestModelRejectsUnknownSubcommand(t *testing.T) {
	_, stderr, code := invoke(t, "model", "enable")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "enable") {
		t.Errorf("stderr does not name the unknown subcommand:\n%s", stderr)
	}
}

// The help text is where an operator learns how to configure a backend, since credentials
// are read from the environment and there is no config file to read them from.
func TestModelCheckHelpNamesTheEnvironmentVariables(t *testing.T) {
	stdout, stderr, code := invoke(t, "model", "check", "-h")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for -h\n%s", code, stderr)
	}
	text := stdout + stderr
	for _, want := range []string{
		model.EnvBackend, model.EnvModel, model.EnvBaseURL,
		model.EnvAPIKey, model.EnvBedrockToken,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("help does not mention %s:\n%s", want, text)
		}
	}
}

func TestUsageListsModel(t *testing.T) {
	stdout, _, _ := invoke(t, "help")
	if !strings.Contains(stdout, "model") {
		t.Errorf("top-level usage does not list the model command:\n%s", stdout)
	}
}
