package output

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// Table is a minimal column-based view model. Callers build one from
// whatever domain/app data they have; output never depends on business
// types directly.
type Table struct {
	Headers []string
	Rows    [][]string
	// Footer, if set, is printed after the rows as a plain line (e.g. a
	// summary count) rather than as part of the aligned column grid.
	Footer string
}

// RenderTable writes a simple, aligned, border-free table - no box-drawing
// characters, no ASCII art, so it stays legible piped through `less`,
// redirected to a file, or read in CI logs.
func RenderTable(w io.Writer, t Table) error {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)

	if len(t.Headers) > 0 {
		if _, err := fmt.Fprintln(tw, joinRow(t.Headers)); err != nil {
			return err
		}
	}
	for _, row := range t.Rows {
		if _, err := fmt.Fprintln(tw, joinRow(row)); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(t.Rows) == 0 {
		if _, err := fmt.Fprintln(w, "(no results)"); err != nil {
			return err
		}
	}
	if t.Footer != "" {
		if _, err := fmt.Fprintln(w, t.Footer); err != nil {
			return err
		}
	}
	return nil
}

func joinRow(cells []string) string {
	out := ""
	for i, c := range cells {
		if i > 0 {
			out += "\t"
		}
		out += c
	}
	return out
}
