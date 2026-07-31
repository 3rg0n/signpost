//go:build windows

package model

import (
	"context"
	"net"
	"os"
	"strings"
	"time"
)

// DefaultInferdAddr is the generation named pipe (protocol-v2 §1).
func DefaultInferdAddr() string { return `\\.\pipe\inferd` }

// dialLocal opens the Windows named pipe.
//
// os.OpenFile rather than a pipe library, because github.com/Microsoft/go-winio is
// the ordinary way to do this and signpost carries no third-party dependencies
// (ADR 0002). What that costs is overlapped I/O; what it buys is that a named pipe
// opened as a file is already a bidirectional byte stream, which is all the protocol
// needs.
//
// The retry loop covers one real condition rather than being defensive padding: a
// named-pipe server accepts one client per instance, so between the daemon accepting a
// connection and binding the next instance, an open fails with ERROR_PIPE_BUSY. That
// window is milliseconds and a single build may open several connections in a row, so
// failing on the first attempt would report "no daemon" about a daemon that is running
// and busy.
func dialLocal(ctx context.Context, addr string) (net.Conn, error) {
	const (
		window = 2 * time.Second
		pause  = 20 * time.Millisecond
	)
	deadline := time.Now().Add(window)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	for {
		// #nosec G304 -- addr is the daemon address from the operator's own configuration,
		// and it names a pipe rather than a path under a root, so os.Root does not apply.
		f, err := os.OpenFile(addr, os.O_RDWR, 0)
		if err == nil {
			return &pipeConn{File: f, addr: addr}, nil
		}
		if !pipeBusy(err) || !time.Now().Before(deadline) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pause):
		}
	}
}

// pipeBusy reports whether the open failed for the transient all-instances-busy
// reason.
//
// Matched on the message text because the alternative is importing golang.org/x/sys
// for one constant (ERROR_PIPE_BUSY = 231). Folded to lower case deliberately:
// Windows capitalises the message as "All pipe instances are busy.", so a
// lower-case-only comparison never matches and the retry silently never fires.
func pipeBusy(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "pipe instances are busy")
}

// pipeConn adapts an os.File to net.Conn so the wire codec is platform-agnostic.
type pipeConn struct {
	*os.File
	addr string
}

func (p *pipeConn) LocalAddr() net.Addr  { return pipeAddr(p.addr) }
func (p *pipeConn) RemoteAddr() net.Addr { return pipeAddr(p.addr) }

type pipeAddr string

func (pipeAddr) Network() string  { return "pipe" }
func (a pipeAddr) String() string { return string(a) }
