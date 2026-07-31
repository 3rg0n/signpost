//go:build !windows

package model

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultInferdAddr resolves the generation socket path.
//
// The chain mirrors the daemon's own resolution (protocol-v2 §1.1), first hit wins,
// because a client that looks somewhere the daemon does not bind reports "no daemon"
// about a running one:
//
//  1. $XDG_RUNTIME_DIR/inferd/inferd.sock — Linux, when systemd-logind set it
//  2. $HOME/.inferd/run/inferd.sock — sessions without logind
//  3. /tmp/inferd/inferd.sock — last resort
//
// On macOS the directory is $TMPDIR/inferd.
func DefaultInferdAddr() string {
	const name = "inferd.sock"
	if runtime.GOOS == "darwin" {
		return filepath.Join(os.TempDir(), "inferd", name)
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "inferd", name)
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".inferd", "run", name)
	}
	return filepath.Join("/tmp", "inferd", name)
}

// dialLocal opens the Unix domain socket.
//
// No retry loop here, unlike Windows: a UDS listener has a backlog, so a connect
// arriving while the daemon is serving another client queues rather than failing, and
// the busy window the named-pipe path has to paper over does not exist.
func dialLocal(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", addr)
}
