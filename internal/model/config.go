package model

import (
	"fmt"
	"os"
	"strings"
)

// Kind names a backend.
type Kind string

const (
	// KindInferd is local IPC to a resident daemon.
	KindInferd Kind = "inferd"
	// KindOpenAI is HTTP to any OpenAI-compatible endpoint, Bedrock included.
	KindOpenAI Kind = "openai"
	// KindNone is deterministic-only.
	//
	// Not an error state — a supported mode (§5). Most runs of signpost will use it,
	// because the deterministic bundle is the product and the semantic pass is the
	// part that improves it.
	KindNone Kind = "none"
)

// Config selects and configures a backend.
//
// Credentials are read from the environment and never from a file, which is the one
// rule here that is not a preference: .signpost.yml is committed, and a config format
// with a place to put an API key is a config format that will eventually have one in
// it.
type Config struct {
	Backend Kind

	// Model is the model id for the openai backend, or a label for inferd. "auto" or
	// empty resolves per backend.
	Model string

	// BaseURL is the openai backend's API root. Empty reads
	// SIGNPOST_OPENAI_BASE_URL, then falls back to Bedrock in AWS_REGION when a
	// Bedrock token is present.
	BaseURL string

	// Addr overrides the inferd socket or pipe path.
	Addr string

	// Version is signpost's version, stamped into Actor.
	Version string
}

// Environment variables read here. Named as constants because they are part of the
// tool's interface — CI configures signpost through them — and a typo in a string
// literal spelled twice is a silent misconfiguration.
const (
	EnvBackend = "SIGNPOST_BACKEND"
	EnvModel   = "SIGNPOST_MODEL"
	EnvBaseURL = "SIGNPOST_OPENAI_BASE_URL"
	EnvAPIKey  = "SIGNPOST_OPENAI_API_KEY" // #nosec G101 -- the name of a variable to read, not a value

	// EnvBedrockToken is AWS's own variable for a Bedrock API key. Honoured directly
	// so that a host already set up for Bedrock needs no signpost-specific
	// configuration: the aws token generator and the Bedrock console both write this
	// name, and asking a user to copy it into a second variable is friction with no
	// benefit.
	EnvBedrockToken = "AWS_BEARER_TOKEN_BEDROCK" // #nosec G101 -- as above: a variable name

	// EnvAWSRegion supplies the region when the base URL is derived for Bedrock.
	EnvAWSRegion        = "AWS_REGION"
	EnvAWSDefaultRegion = "AWS_DEFAULT_REGION"
)

// DefaultBedrockModel is the model signpost asks Bedrock for when none is configured.
//
// Chosen by testing the OpenAI-compatible surface rather than by reputation, and the
// two findings that decided it are worth recording because neither is guessable:
//
//   - Amazon's own text-generation models are not on this surface at all. Every Titan
//     text model there is now an embeddings model, and the Nova family supports Invoke
//     and Converse but not Chat Completions. So "use a cheap Amazon model" is not an
//     option on an OpenAI-compatible path.
//   - gpt-oss is cheaper and does honour the schema, but it emits its reasoning trace
//     into the same `content` field as the answer, so a strict json_schema response
//     arrives as `<reasoning>…</reasoning>{…}`. Recoverable, and jsonObject does
//     recover it, but a default should not need recovering.
//
// Gemma 3 12B has no reasoning channel and returns the constrained object alone, which
// makes it the model whose output matches what §4.5 promises. It is also the family
// inferd runs locally, so the local and remote backends behave alike.
const DefaultBedrockModel = "google.gemma-3-12b-it"

// New builds the configured backend.
//
// Returns nil for KindNone. A nil Backend is the deterministic-only mode and callers
// are expected to check for it, rather than a null-object backend that would report a
// skip for every node individually.
func New(cfg Config) (Backend, error) {
	cfg = cfg.withEnv()
	switch cfg.Backend {
	case KindNone:
		return nil, nil
	case KindInferd:
		return &Inferd{Addr: cfg.Addr, Model: cfg.resolvedModel(), Version: cfg.Version}, nil
	case KindOpenAI:
		base, err := cfg.resolvedBaseURL()
		if err != nil {
			return nil, err
		}
		return &OpenAI{
			BaseURL: base,
			APIKey:  apiKey(),
			Model:   cfg.resolvedModel(),
			Version: cfg.Version,
		}, nil
	default:
		return nil, fmt.Errorf("model: unknown backend %q (want inferd, openai, or none)", cfg.Backend)
	}
}

// withEnv fills empty fields from the environment.
//
// Explicit configuration wins over the environment, and the environment wins over the
// default. That order is what makes a CI job configurable without a config file and a
// developer's config file authoritative on their own machine.
func (c Config) withEnv() Config {
	if c.Backend == "" {
		c.Backend = Kind(strings.TrimSpace(os.Getenv(EnvBackend)))
	}
	if c.Backend == "" {
		c.Backend = defaultBackend()
	}
	if c.Model == "" {
		c.Model = strings.TrimSpace(os.Getenv(EnvModel))
	}
	if c.BaseURL == "" {
		c.BaseURL = strings.TrimSpace(os.Getenv(EnvBaseURL))
	}
	return c
}

// defaultBackend picks a backend for a run that did not choose one.
//
// none, always. Inferring a backend from a stray environment variable would mean a
// build that silently starts calling a model — spending tokens, sending repository
// content to a third party — because something unrelated was set. §5 makes the
// semantic pass opt-in, and this is where that is enforced.
func defaultBackend() Kind { return KindNone }

func (c Config) resolvedModel() string {
	if c.Model != "" && c.Model != "auto" {
		return c.Model
	}
	if c.Backend == KindOpenAI {
		return DefaultBedrockModel
	}
	// inferd serves whatever it has warm and the wire carries no model selector, so
	// there is nothing to resolve — the label says the daemon chose.
	return "inferd"
}

// resolvedBaseURL determines where to send an openai-backend request.
func (c Config) resolvedBaseURL() (string, error) {
	if c.BaseURL != "" {
		return c.BaseURL, nil
	}
	if os.Getenv(EnvBedrockToken) != "" {
		region := firstEnv(EnvAWSRegion, EnvAWSDefaultRegion)
		if region == "" {
			return "", fmt.Errorf("model: %s is set but no region is: set %s",
				EnvBedrockToken, EnvAWSRegion)
		}
		return BedrockBaseURL(region), nil
	}
	return "", fmt.Errorf("model: the openai backend needs a base URL: set %s (or %s for Bedrock)",
		EnvBaseURL, EnvBedrockToken)
}

// BedrockBaseURL is the OpenAI-compatible root for a region.
//
// bedrock-runtime rather than the newer bedrock-mantle endpoint, even though AWS
// recommends mantle. The two are separately authorised — mantle gates on
// bedrock-mantle:CallWithBearerToken and its own project resource, bedrock-runtime on
// bedrock:CallWithBearerToken — and a role permitted to call one can be denied the
// other. bedrock-runtime is the surface an account with ordinary Bedrock access
// already has, so it is the one that works without a policy change. Point BaseURL at
// mantle explicitly to use it.
func BedrockBaseURL(region string) string {
	return "https://bedrock-runtime." + region + ".amazonaws.com/openai/v1"
}

// apiKey reads the credential, preferring signpost's own variable.
//
// Both are read so that a host configured for Bedrock works untouched while a run
// against a different endpoint can override without unsetting AWS's variable — which
// matters because on a developer machine that variable is usually set for other tools.
func apiKey() string {
	return firstEnv(EnvAPIKey, EnvBedrockToken)
}

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v
		}
	}
	return ""
}
