package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/3rg0n/signpost/internal/export"
)

// `signpost graph export` renders the graph in a format another tool reads.
func runExport(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("graph export", flag.ContinueOnError)
	// One writer for the whole of this command's usage, so the prose above and
	// PrintDefaults' flag list cannot land on different streams.
	help := helpStream(args, out, errOut)
	fs.SetOutput(help)
	fs.Usage = func() {
		u := newPrinter(help)
		u.printf("usage: signpost graph export [flags] [path]\n")
		u.printf("\nRender the graph. Formats: %s.\n\nFlags:\n", formatList())
		fs.PrintDefaults()
	}
	var pf pipelineFlags
	pf.register(fs)
	format := fs.String("format", string(export.FormatMermaid), "output format: "+formatList())
	outPath := fs.String("o", "", "write to this file instead of stdout")
	noCluster := fs.Bool("no-cluster", false, "skip clustering, so no subgraphs are drawn")
	quiet := fs.Bool("quiet", false, "suppress the coverage report on stderr")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	f, err := parseFormat(*format)
	if err != nil {
		return err
	}
	path, err := repoPath(fs)
	if err != nil {
		return err
	}

	a, err := analyse(context.Background(), path, pf)
	if err != nil {
		return err
	}
	if !*quiet {
		reportCoverage(errOut, a)
	}
	if !*noCluster {
		a.Graph().Clusters()
	}

	// Rendered to a buffer first, then written once. An export is either the whole
	// graph or nothing: a file half-written by a failure partway through is worse
	// than no file, because it looks like a valid export of a smaller repository.
	var buf bytes.Buffer
	if err := export.Write(&buf, a.Graph(), f); err != nil {
		return err
	}
	if *outPath == "" {
		_, err := out.Write(buf.Bytes())
		return err
	}
	return writeFile(*outPath, buf.Bytes())
}

// writeFile writes atomically: a temp file in the destination directory, then a
// rename. The destination is routinely a committed artifact, and an interrupted
// run must not leave a truncated one behind for CI to diff.
func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".signpost-export-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		// Removing a file that was already renamed away fails, which is the success
		// path and not worth reporting.
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		// The write error is what the caller needs; a close failure on a file that is
		// about to be removed adds nothing.
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// 0o644 rather than the 0o600 CreateTemp gives: an export is a committed
	// artifact other tools and CI steps read, and a file only its creator can read
	// is a surprise in a checkout. It contains module names and file paths already
	// present in the repository, so there is nothing here to keep private.
	// #nosec G302 -- world-readable is the intent for a committed artifact.
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func parseFormat(s string) (export.Format, error) {
	want := export.Format(strings.ToLower(strings.TrimSpace(s)))
	for _, f := range export.Formats() {
		if f == want {
			return f, nil
		}
	}
	return "", fmt.Errorf("%w: unknown format %q; want one of %s", errUsage, s, formatList())
}

func formatList() string {
	fs := export.Formats()
	names := make([]string, len(fs))
	for i, f := range fs {
		names[i] = string(f)
	}
	return strings.Join(names, ", ")
}
