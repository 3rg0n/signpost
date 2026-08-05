package view

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// openBrowser asks the desktop to open url in whatever the user's default browser is.
//
// **The URL is validated before it becomes an argument, and that is the whole of the
// safety here.** This hands a string to a shell-adjacent launcher on two of the three
// platforms: Windows' `rundll32 url.dll,FileProtocolHandler` and the `xdg-open` shell
// script. Both are documented to act on what the string *says* rather than on what it
// was meant to be, so a URL carrying anything other than a loopback HTTP address is a
// command-execution surface. Serve only ever passes an address it just bound on
// 127.0.0.1, so the check should never fire — it is here because the day it stops
// being true is the day it matters, and a caller cannot see this constraint from the
// signature.
func openBrowser(url string) error {
	if err := checkURL(url); err != nil {
		return err
	}
	name, args := opener(url)
	if name == "" {
		return fmt.Errorf("no way to open a browser on %s", runtime.GOOS)
	}
	// #nosec G204 -- checkURL restricts url to http://<loopback>:<port>/ and every
	// other argument is a literal. See the comment above for why that check exists.
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.Command(name, args...)
	// Started and not waited on. The launcher exits immediately on Windows and macOS,
	// but xdg-open can exec the browser in the foreground and live as long as it does —
	// waiting would block the server that has not started serving yet. The cost is a
	// launcher failure that happens after fork being invisible, which is why the URL is
	// printed regardless.
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reaped so the process does not stay a zombie for the lifetime of the server. In
	// its own goroutine because of the xdg-open case above.
	go func() { _ = cmd.Wait() }()
	return nil
}

// opener names the launcher for this platform.
//
// Every platform uses its own indirection rather than a browser name, so the user's
// configured default is what opens: BROWSER or the mimeapps association on Linux, the
// Launch Services binding on macOS, the registry association on Windows.
func opener(url string) (string, []string) {
	switch runtime.GOOS {
	case "windows":
		// Not `cmd /c start`: `start` parses its argument, and `&` in a URL splits the
		// command line. rundll32's FileProtocolHandler takes the whole string as one
		// argument and hands it to the shell's protocol association, which is what a
		// double-clicked link does.
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		return "open", []string{url}
	default:
		// xdg-open covers the desktop environments that matter and falls back to $BROWSER
		// itself. A machine without it gets the printed URL, which is why the failure is
		// not fatal.
		return "xdg-open", []string{url}
	}
}

// checkURL admits only the shape Serve builds: http, a loopback host, a port, and the
// root path.
//
// Written as an explicit character-and-shape check rather than net/url parsing plus a
// scheme comparison. url.Parse accepts a great deal that is not a URL and normalises
// some of it, so a parse-then-inspect check has to enumerate what to reject; this
// enumerates what to accept, which is one address family and one path.
func checkURL(url string) error {
	rest, ok := strings.CutPrefix(url, "http://")
	if !ok {
		return fmt.Errorf("refusing to open %q: not an http:// URL", url)
	}
	host, ok := strings.CutSuffix(rest, "/")
	if !ok {
		return fmt.Errorf("refusing to open %q: expected a trailing /", url)
	}
	if !checkHost(host) {
		return fmt.Errorf("refusing to open %q: not a loopback address", url)
	}
	// checkHost accepts the name "localhost", which resolves through the host's
	// resolver. Serve never builds one — it prints the address it bound — so a name
	// here means something upstream changed, and the point of this function is to
	// notice that rather than to be liberal.
	if strings.ContainsAny(host, `"'\ ;&|<>$`+"`\n\r\t") {
		return fmt.Errorf("refusing to open %q: the address contains a shell character", url)
	}
	return nil
}
