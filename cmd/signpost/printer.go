package main

import (
	"fmt"
	"io"
)

// printer writes to an io.Writer and latches the first error.
//
// A CLI that ignores write errors gets one thing wrong that matters: `signpost
// export | head` closes the pipe partway through, and every subsequent write fails.
// Ignoring that means exiting 0 after producing a truncated export, which a script
// downstream reads as a successful run over a smaller repository.
//
// Latching rather than returning per call is what keeps a report readable as a
// sequence of lines: the first failure is kept, later writes become no-ops, and the
// caller checks once at the end.
type printer struct {
	w   io.Writer
	err error
}

func newPrinter(w io.Writer) *printer { return &printer{w: w} }

func (p *printer) printf(format string, args ...any) {
	if p.err != nil {
		return
	}
	_, p.err = fmt.Fprintf(p.w, format, args...)
}

// Err returns the first write error, wrapped so a broken pipe is identifiable as
// what it is rather than surfacing as a bare "file already closed".
func (p *printer) Err() error {
	if p.err == nil {
		return nil
	}
	return fmt.Errorf("writing output: %w", p.err)
}
