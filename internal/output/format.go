// Package output renders CLI results in one of three formats: a
// human-friendly table (the default), JSON, or YAML. Table rendering never
// emits ANSI escape codes, so `--output table` output is already
// NO_COLOR-safe and CI-friendly without any special handling; JSON/YAML
// output contains no formatting codes by construction.
package output

import (
	"fmt"
	"io"
)

type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
)

// ParseFormat validates a --output flag value, defaulting to table.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case "", FormatTable:
		return FormatTable, nil
	case FormatJSON:
		return FormatJSON, nil
	case FormatYAML:
		return FormatYAML, nil
	default:
		return "", fmt.Errorf("output: unknown format %q (supported: table, json, yaml)", s)
	}
}

// Render writes data using the requested format. table is used only for
// FormatTable; data is used for FormatJSON/FormatYAML, and should be a
// plain, JSON/YAML-friendly struct built by the caller (not a domain or
// app type directly, so presentation shape can evolve independently of
// business types).
func Render(w io.Writer, format Format, table Table, data any) error {
	switch format {
	case FormatJSON:
		return RenderJSON(w, data)
	case FormatYAML:
		return RenderYAML(w, data)
	default:
		return RenderTable(w, table)
	}
}
