package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/3rg0n/signpost/internal/model"
)

// `signpost model check` is the one command that talks to a model.
//
// It exists because the semantic pass fails open (§5): a backend that is misconfigured,
// denied, or pointed at the wrong endpoint produces the deterministic bundle and exits 0,
// which is the right behaviour for a build and the wrong behaviour for someone trying to
// find out why their bundle has no semantic pages. This is where that question gets a
// straight answer.
//
// So this command does not fail open. It exits non-zero when the backend does not work,
// because an operator who ran a check wants the check to fail.
func runModelCheck(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("model check", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		u := newPrinter(errOut)
		u.printf("usage: signpost model check [flags]\n")
		u.printf("\nSend one schema-constrained request to the configured backend and report\n")
		u.printf("what came back. Exits non-zero if the backend does not work.\n\n")
		u.printf("Configuration is read from the environment:\n")
		u.printf("  %-28s inferd, openai, or none (default none)\n", model.EnvBackend)
		u.printf("  %-28s model id for the openai backend\n", model.EnvModel)
		u.printf("  %-28s API root for the openai backend\n", model.EnvBaseURL)
		u.printf("  %-28s credential for the openai backend\n", model.EnvAPIKey)
		u.printf("  %-28s AWS's own Bedrock key; used with %s\n", model.EnvBedrockToken, model.EnvAWSRegion)
		u.printf("\nFlags:\n")
		fs.PrintDefaults()
	}
	backend := fs.String("backend", "", "override "+model.EnvBackend)
	name := fs.String("model", "", "override "+model.EnvModel)
	baseURL := fs.String("base-url", "", "override "+model.EnvBaseURL)
	addr := fs.String("addr", "", "inferd socket or pipe path")
	timeout := fs.Duration("timeout", model.DefaultTimeout, "how long to wait for a response")
	verbose := fs.Bool("verbose", false, "print the response body")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: model check takes no positional arguments", errUsage)
	}

	b, err := model.New(model.Config{
		Backend: model.Kind(*backend),
		Model:   *name,
		BaseURL: *baseURL,
		Addr:    *addr,
		Version: version,
	})
	if err != nil {
		return err
	}
	// The deterministic-only case is not an error — it is the default and the supported
	// mode — but it is also not a passing check, so it says what to set rather than
	// printing "ok" about a backend that does not exist.
	if b == nil {
		p := newPrinter(out)
		p.printf("no backend is configured, so signpost runs deterministic-only.\n")
		p.printf("Set %s=inferd for a local daemon, or %s=openai with %s.\n",
			model.EnvBackend, model.EnvBackend, model.EnvBaseURL)
		return p.Err()
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	p := newPrinter(out)
	p.printf("backend: %s\n", b.Actor())

	start := time.Now()
	res, err := b.Complete(ctx, model.ProbeRequest())
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		// Named rather than folded into the error text, because the two cases point at
		// different fixes: unavailable means start a daemon or fix a credential, a fault
		// means the request itself was wrong.
		if errors.Is(err, model.ErrUnavailable) {
			return fmt.Errorf("the backend is unreachable after %s: %w", elapsed, err)
		}
		return fmt.Errorf("the backend answered but the response was unusable after %s: %w", elapsed, err)
	}

	answer, err := model.ParseProbe(res.JSON)
	if err != nil {
		return err
	}

	p.printf("  round trip: %s\n", elapsed)
	p.printf("  tokens: %d in, %d out\n", res.InputTokens, res.OutputTokens)
	p.printf("  schema honoured: yes\n")
	// The probe file contains an injection attempt and the fence tells the model to
	// report it as an observation. A model that noticed is demonstrating §4.5 working on
	// this machine; one that did not still has a working transport, so this is reported
	// rather than judged.
	p.printf("  noticed the embedded instruction: %s\n", yesNo(answer.ContainedInstructions))
	if answer.AnsweredCorrectly() {
		p.printf("  identified the source language: yes (%s)\n", answer.Language)
	} else {
		p.printf("  identified the source language: no (said %q, expected Go)\n", answer.Language)
		p.printf("  this backend works but the model is weak; summaries will be poor.\n")
	}
	if *verbose {
		p.printf("  response: %s\n", res.JSON)
	}
	p.printf("ok: the backend serves schema-constrained JSON\n")
	return p.Err()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// runModel dispatches the model subcommands.
//
// A group rather than a flat `signpost model-check`, because check is the first of
// several — enable and list belong here too — and a group keeps them discoverable
// together instead of scattered through the top-level verb list.
func runModel(args []string, out, errOut io.Writer) error {
	subs := []command{
		{"check", "send one request to the configured backend and report what came back", runModelCheck},
	}
	if len(args) == 0 {
		modelUsage(errOut, subs)
		return errUsage
	}
	switch args[0] {
	case "-h", "--help", "help":
		modelUsage(out, subs)
		return flag.ErrHelp
	}
	for _, s := range subs {
		if s.name == args[0] {
			return s.run(args[1:], out, errOut)
		}
	}
	return fmt.Errorf("%w: unknown model subcommand %q", errUsage, args[0])
}

func modelUsage(w io.Writer, subs []command) {
	p := newPrinter(w)
	p.printf("usage: signpost model <subcommand> [flags]\n\n")
	for _, s := range subs {
		p.printf("  %-8s %s\n", s.name, s.summary)
	}
	p.printf("\nRun `signpost model <subcommand> -h` for flags.\n")
}
