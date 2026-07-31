package model

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
)

// Inferd is a Backend speaking the inferd v2 generation protocol over local IPC.
//
// This is the backend for anywhere signpost runs on a machine we control: a laptop, a
// self-hosted runner, a cloud VM. inferd holds one warm model host-wide and hands it
// to every local consumer over a Unix socket or a Windows named pipe, so the marginal
// cost of a run is zero, nothing leaves the machine, and there is no network listener
// involved.
//
// It is also the backend where the design's schema guarantee is strongest. inferd
// compiles the JSON Schema in `response_format` to a grammar that constrains the
// sampler, so a local model cannot emit output of the wrong shape — which is what
// makes a small local model trustworthy enough for the semantic pass (§4.5). The
// remote backend gets whatever the endpoint chooses to enforce.
type Inferd struct {
	// Addr is the socket or pipe path. Empty means the platform default.
	Addr string

	// Model names the model for Actor. inferd serves whatever the daemon has warm and
	// the wire carries no model selector, so this is a label, not a request — it
	// should say what the daemon is actually running.
	Model string

	// Version is signpost's version, for Actor.
	Version string
}

// Actor implements Backend.
func (i *Inferd) Actor() string {
	v := i.Version
	if v == "" {
		v = "dev"
	}
	m := i.Model
	if m == "" {
		m = "inferd"
	}
	return "signpost/" + v + "+" + m
}

// Complete implements Backend.
//
// One request per connection. The protocol permits reuse, but reuse buys throughput
// signpost does not need — the semantic pass is per-node and budget-bounded, not a hot
// loop — and it costs the guarantee that a half-read stream from one call cannot
// corrupt the next. Connect, ask, read to the terminal frame, close.
func (i *Inferd) Complete(ctx context.Context, req Request) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}

	conn, err := dialInferd(ctx, i.addr())
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = conn.Close() }()

	// Closing the connection is how a request is cancelled — the protocol has no
	// cancel frame — so the context deadline has to reach the socket rather than only
	// being checked between reads.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	body, err := json.Marshal(inferdRequest(req))
	if err != nil {
		return Result{}, fmt.Errorf("model: encoding the inferd request: %w", err)
	}

	w := bufio.NewWriter(conn)
	if err := writeFrame(w, frameJSON, body); err != nil {
		return Result{}, unavailablef("writing to inferd at %s: %v", i.addr(), err)
	}

	return readGeneration(bufio.NewReader(conn), i.addr())
}

func (i *Inferd) addr() string {
	if i.Addr != "" {
		return i.Addr
	}
	return DefaultInferdAddr()
}

// inferdRequest builds a RequestV2.
//
// stream is false because signpost wants a whole document, not a typewriter: with
// streaming off the daemon withholds the incremental deltas and the terminal frame
// carries the same usage and stop_reason, so the only thing dropped is work this
// caller would have thrown away. thinking is likewise off — a reasoning trace comes
// back as separate `thinking` blocks that signpost has no use for, and asking for one
// spends tokens against the budget in §5.
func inferdRequest(req Request) map[string]any {
	messages := make([]map[string]any, 0, 2)
	if req.System != "" {
		messages = append(messages, inferdMessage("system", req.System))
	}
	messages = append(messages, inferdMessage("user", req.User))

	body := map[string]any{
		"wire_version": wireVersion,
		"messages":     messages,
		"stream":       false,
		// Zero for the same reason as the remote backend: an unchanged repository has
		// to produce an unchanged bundle (§4.6).
		"temperature": 0,
		"response_format": map[string]any{
			"type":   "json_schema",
			"schema": req.Schema,
		},
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	return body
}

func inferdMessage(role, text string) map[string]any {
	return map[string]any{
		"role":    role,
		"content": []map[string]any{{"type": "text", "text": text}},
	}
}

// inferdResponse is the subset of ResponseV2 signpost reads.
type inferdResponse struct {
	Type  string `json:"type"`
	Block struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	} `json:"block"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	StopReason string `json:"stop_reason"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

// readGeneration reads response frames to the terminal frame and assembles the answer.
//
// Text deltas are concatenated even with stream false: the field is documented as
// controlling whether intermediate frames are *emitted*, not whether the terminal
// frame carries the text, and a client that ignored deltas would return an empty
// answer against a daemon that chose to send them. `thinking` deltas are dropped
// rather than concatenated — folding a reasoning trace into the answer is precisely
// the bug the protocol separates those block types to prevent.
func readGeneration(r *bufio.Reader, addr string) (Result, error) {
	var text strings.Builder
	for {
		typ, payload, err := readFrame(r)
		if err != nil {
			if errors.Is(err, errCleanClose) {
				return Result{}, unavailablef("inferd at %s closed without a terminal frame", addr)
			}
			return Result{}, unavailablef("reading from inferd at %s: %v", addr, err)
		}
		if typ == frameBlob {
			return Result{}, errors.New("inferd: the daemon sent a blob frame on the response stream")
		}
		if typ != frameJSON {
			return Result{}, fmt.Errorf("inferd: unknown frame type 0x%02x", typ)
		}

		var resp inferdResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			return Result{}, fmt.Errorf("model: the inferd response was not valid JSON: %w", err)
		}

		switch resp.Type {
		case "frame":
			if resp.Block.Type == "text" {
				text.WriteString(resp.Block.Delta)
			}
		case "done":
			return doneResult(resp, text.String())
		case "error":
			return Result{}, inferdError(resp, addr)
		default:
			// Forward-compatibility: the protocol adds response types additively, and
			// an unknown one is not a reason to fail a request that may still
			// terminate normally.
			continue
		}
	}
}

func doneResult(resp inferdResponse, text string) (Result, error) {
	// Reported rather than parsed, for the same reason as the remote backend: a
	// summary cut off at the token limit is often still valid JSON, and committing a
	// truncated claim as though it were complete is the confidently-wrong output the
	// design refuses to emit.
	if resp.StopReason == "max_tokens" {
		return Result{}, errors.New("model: inferd hit the token limit before the response finished")
	}
	content, err := jsonObject(text)
	if err != nil {
		return Result{}, err
	}
	return Result{
		JSON:         content,
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
	}, nil
}

// inferdError maps a terminal error frame to a Go error.
//
// The split follows §5's fail-open rule and the daemon's own failure semantics (its
// ADR 0007: the daemon does not retry or fail over, so the caller decides). A full
// queue, an unavailable backend, or a version mismatch is the daemon declining to
// serve — no amount of signpost changing its request fixes those, so they fail open.
// invalid_request and tool_call_malformed mean signpost sent something wrong, and
// hiding that behind a bundle with no semantic pages would hide a bug.
func inferdError(resp inferdResponse, addr string) error {
	switch resp.Code {
	case "queue_full", "backend_unavailable", "internal", "wire_version_unsupported":
		return unavailablef("inferd at %s returned %s: %s", addr, resp.Code, resp.Message)
	default:
		return fmt.Errorf("model: inferd rejected the request (%s): %s", resp.Code, resp.Message)
	}
}

// dialInferd opens the platform transport, reporting a missing daemon as unavailable.
func dialInferd(ctx context.Context, addr string) (net.Conn, error) {
	conn, err := dialLocal(ctx, addr)
	if err != nil {
		return nil, unavailablef("no inferd daemon at %s: %v", addr, err)
	}
	return conn, nil
}
