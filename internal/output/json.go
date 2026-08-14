package output

import (
	"encoding/json"
	"io"
)

// RenderJSON writes data as indented JSON with no trailing formatting
// codes, safe for machine consumption.
func RenderJSON(w io.Writer, data any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}
