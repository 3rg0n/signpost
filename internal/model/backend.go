// Package model is the boundary between signpost's deterministic core and a
// language model.
//
// Everything else in signpost reads code and reports what is provably there. This
// package is the one place that asks something to guess, so the guessing is confined
// behind one interface with two properties that make its output safe to commit:
//
//   - Output is schema-constrained. A JSON Schema goes out with every request and
//     validated JSON comes back. On inferd the schema compiles to a grammar that
//     constrains the sampler, so the model cannot emit malformed output — only wrong
//     output. See §4.5.
//   - Output is attributed. Actor names the model that produced a claim, and that
//     string is stamped into the bundle's `generated.by`, so a reader can tell which
//     pages are asserted by a tool and which are asserted by a model.
//
// What this package deliberately does not do is decide whether a claim is true. That
// is the grounding rule's job at emit time (§4.5): a claim whose citation does not
// resolve is dropped. This package's contract is narrower — well-shaped JSON, or an
// error.
package model

import (
	"context"
	"errors"
	"fmt"
)

// Backend is a schema-constrained text generator.
//
// Both implementations are first-party code over the standard library, which is the
// whole reason this interface is shaped around JSON rather than around a vendor's
// request type: signpost carries no third-party dependencies (ADR 0002), so a
// backend is HTTP or a socket and a JSON codec, never an SDK.
type Backend interface {
	// Complete sends a prompt plus a JSON Schema and returns validated JSON.
	Complete(ctx context.Context, req Request) (Result, error)

	// Actor is the OKF `generated.by` string, e.g. "signpost/0.1.0+gemma-3-12b".
	Actor() string
}

// Request is one call to a model.
//
// System and User are separate because the untrusted-input boundary depends on it:
// repository content goes in User wrapped in delimited blocks, and the instruction
// that content inside those blocks is data rather than instructions goes in System
// (§4.5). A single concatenated prompt would erase that distinction on the wire.
type Request struct {
	// System is the instruction, written by signpost. Never repository content.
	System string

	// User is the analysis task, and the only place repository content appears.
	User string

	// Schema is a JSON Schema object the response must satisfy. Required: a call
	// without one is a call whose output cannot be parsed reliably, which is the
	// failure mode this package exists to prevent.
	Schema map[string]any

	// SchemaName labels the schema for backends that require one. Defaults to
	// "response".
	SchemaName string

	// MaxTokens caps the response. Zero means the backend's default.
	MaxTokens int
}

// Result is what came back.
type Result struct {
	// JSON is the response body, already checked to be valid JSON that parses as an
	// object. It is not checked against Schema — see Complete's contract on each
	// implementation for why that check belongs where it does.
	JSON []byte

	// InputTokens and OutputTokens are what the backend reported, for the budget in
	// §5. Zero when a backend does not report usage.
	InputTokens  int
	OutputTokens int
}

// ErrUnavailable reports that a backend could not be reached at all: no daemon
// listening, no credentials, DNS failure, connection refused.
//
// It exists as a distinct error because §5 makes fail-open the required behaviour —
// an unreachable backend emits the deterministic bundle, records the skip, and exits
// 0. That decision needs to distinguish "there is no model here" from "the model
// answered and the answer was garbage": the first is a supported mode, and the second
// is a bug worth surfacing. Callers test with errors.Is.
var ErrUnavailable = errors.New("model backend is unavailable")

// unavailablef wraps a cause as ErrUnavailable while keeping the cause readable.
//
// Both branches are needed: errors.Is(err, ErrUnavailable) has to be true so the
// caller can fail open, and the operator has to be able to read what actually
// failed, because "unavailable" alone does not tell anyone whether to start a daemon
// or set a token.
func unavailablef(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnavailable, fmt.Sprintf(format, args...))
}

// validate checks a Request before it costs a round trip.
func (r Request) validate() error {
	if r.User == "" {
		return errors.New("model: request has no prompt")
	}
	if len(r.Schema) == 0 {
		return errors.New("model: request has no schema, which would make the response unparseable")
	}
	return nil
}

// schemaName is SchemaName or a default, restricted to what a backend will accept as
// an identifier.
func (r Request) schemaName() string {
	if r.SchemaName == "" {
		return "response"
	}
	return r.SchemaName
}
