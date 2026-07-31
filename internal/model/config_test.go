package model

import (
	"strings"
	"testing"
)

// clearEnv unsets everything Config reads, so a test describes its own environment
// rather than the developer's. t.Setenv also fails the test if it is parallel, which is
// the behaviour wanted here.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		EnvBackend, EnvModel, EnvBaseURL, EnvAPIKey,
		EnvBedrockToken, EnvAWSRegion, EnvAWSDefaultRegion,
	} {
		t.Setenv(k, "")
	}
}

// The default is none, and this is the test that keeps it that way. Inferring a backend
// from a stray environment variable would mean a build that silently starts spending
// tokens and sending repository content to a third party because something unrelated was
// set (§5 makes the semantic pass opt-in).
func TestNewDefaultsToDeterministicOnly(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvBedrockToken, "a-token-that-should-not-enable-anything")
	t.Setenv(EnvAWSRegion, "us-east-1")

	b, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b != nil {
		t.Errorf("New() = %T, want nil — a credential in the environment must not enable a backend", b)
	}
}

func TestNewNoneReturnsNilBackend(t *testing.T) {
	clearEnv(t)
	b, err := New(Config{Backend: KindNone})
	if err != nil || b != nil {
		t.Errorf("New(none) = (%v, %v), want (nil, nil)", b, err)
	}
}

func TestNewRejectsUnknownBackend(t *testing.T) {
	clearEnv(t)
	_, err := New(Config{Backend: "ollama"})
	if err == nil {
		t.Fatal("an unknown backend was accepted")
	}
	if !strings.Contains(err.Error(), "ollama") {
		t.Errorf("err = %v, want it to name the unknown backend", err)
	}
}

func TestBackendComesFromTheEnvironmentWhenUnset(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvBackend, "inferd")

	b, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := b.(*Inferd); !ok {
		t.Errorf("New() = %T, want *Inferd", b)
	}
}

// Explicit configuration beats the environment, which beats the default. That order is
// what makes a CI job configurable without a config file and a developer's config
// authoritative on their own machine.
func TestExplicitConfigBeatsTheEnvironment(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvBackend, "inferd")
	t.Setenv(EnvModel, "from-env")
	t.Setenv(EnvBaseURL, "http://from-env.invalid")

	b, err := New(Config{
		Backend: KindOpenAI,
		Model:   "explicit-model",
		BaseURL: "http://explicit.invalid",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	o, ok := b.(*OpenAI)
	if !ok {
		t.Fatalf("New() = %T, want *OpenAI", b)
	}
	if o.Model != "explicit-model" || o.BaseURL != "http://explicit.invalid" {
		t.Errorf("backend = %+v, want the explicit values", o)
	}
}

func TestOpenAIBackendReadsBaseURLAndKeyFromEnvironment(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvBaseURL, "http://localhost:8000/v1")
	t.Setenv(EnvAPIKey, "sk-test")

	b, err := New(Config{Backend: KindOpenAI, Version: "0.1.0"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	o := b.(*OpenAI)
	if o.BaseURL != "http://localhost:8000/v1" {
		t.Errorf("BaseURL = %q", o.BaseURL)
	}
	if o.APIKey != "sk-test" {
		t.Errorf("APIKey was not read from %s", EnvAPIKey)
	}
	if o.Model != DefaultBedrockModel {
		t.Errorf("Model = %q, want the default %q", o.Model, DefaultBedrockModel)
	}
}

// A host already set up for Bedrock should need no signpost-specific configuration: the
// AWS token generator and the Bedrock console both write AWS_BEARER_TOKEN_BEDROCK, so
// that plus a region is enough to derive the endpoint.
func TestOpenAIBackendDerivesBedrockEndpointFromTokenAndRegion(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvBedrockToken, "bedrock-key")
	t.Setenv(EnvAWSRegion, "us-east-1")

	b, err := New(Config{Backend: KindOpenAI})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	o := b.(*OpenAI)
	if want := BedrockBaseURL("us-east-1"); o.BaseURL != want {
		t.Errorf("BaseURL = %q, want %q", o.BaseURL, want)
	}
	if o.APIKey != "bedrock-key" {
		t.Errorf("APIKey = %q, want the Bedrock token", o.APIKey)
	}
}

func TestOpenAIBackendAcceptsAWSDefaultRegion(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvBedrockToken, "bedrock-key")
	t.Setenv(EnvAWSDefaultRegion, "eu-west-1")

	b, err := New(Config{Backend: KindOpenAI})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if want := BedrockBaseURL("eu-west-1"); b.(*OpenAI).BaseURL != want {
		t.Errorf("BaseURL = %q, want %q", b.(*OpenAI).BaseURL, want)
	}
}

// A token with no region is a misconfiguration worth naming, not a silent guess at a
// region — guessing would send repository content to a region the operator did not pick.
func TestOpenAIBackendRequiresARegionWithABedrockToken(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvBedrockToken, "bedrock-key")

	_, err := New(Config{Backend: KindOpenAI})
	if err == nil {
		t.Fatal("a Bedrock token with no region was accepted")
	}
	if !strings.Contains(err.Error(), EnvAWSRegion) {
		t.Errorf("err = %v, want it to name %s", err, EnvAWSRegion)
	}
}

func TestOpenAIBackendRequiresABaseURL(t *testing.T) {
	clearEnv(t)
	_, err := New(Config{Backend: KindOpenAI})
	if err == nil {
		t.Fatal("the openai backend was built with nowhere to send a request")
	}
	if !strings.Contains(err.Error(), EnvBaseURL) {
		t.Errorf("err = %v, want it to name %s", err, EnvBaseURL)
	}
}

// signpost's own variable wins so a run against a different endpoint can override
// without unsetting AWS's variable, which on a developer machine is usually set for
// other tools.
func TestAPIKeyPrefersSignpostVariable(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvAPIKey, "signpost-key")
	t.Setenv(EnvBedrockToken, "bedrock-key")

	if got := apiKey(); got != "signpost-key" {
		t.Errorf("apiKey() = %q, want the signpost variable to win", got)
	}
}

func TestBedrockBaseURLUsesTheOpenAICompatiblePath(t *testing.T) {
	got := BedrockBaseURL("us-east-1")
	// /openai/v1, not /v1. The documented-looking bedrock-runtime/v1 path answers with
	// UnknownOperationException, so this is the one detail here that cannot be guessed.
	if want := "https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1"; got != want {
		t.Errorf("BedrockBaseURL = %q, want %q", got, want)
	}
}

// "auto" is spelled by a config file that wants the default, and treating it as a model
// id would send a 400 naming a model nobody configured.
func TestResolvedModelTreatsAutoAsUnset(t *testing.T) {
	cases := []struct {
		cfg  Config
		want string
	}{
		{Config{Backend: KindOpenAI, Model: "auto"}, DefaultBedrockModel},
		{Config{Backend: KindOpenAI}, DefaultBedrockModel},
		{Config{Backend: KindOpenAI, Model: "openai.gpt-oss-20b-1:0"}, "openai.gpt-oss-20b-1:0"},
		{Config{Backend: KindInferd}, "inferd"},
		{Config{Backend: KindInferd, Model: "gemma-3-12b"}, "gemma-3-12b"},
	}
	for _, tc := range cases {
		if got := tc.cfg.resolvedModel(); got != tc.want {
			t.Errorf("%+v resolvedModel() = %q, want %q", tc.cfg, got, tc.want)
		}
	}
}

func TestInferdBackendTakesTheConfiguredAddress(t *testing.T) {
	clearEnv(t)
	b, err := New(Config{Backend: KindInferd, Addr: "/run/custom.sock", Version: "0.1.0"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	i := b.(*Inferd)
	if i.Addr != "/run/custom.sock" {
		t.Errorf("Addr = %q, want the configured path", i.Addr)
	}
	if i.Version != "0.1.0" {
		t.Errorf("Version = %q, want it stamped for Actor", i.Version)
	}
}
