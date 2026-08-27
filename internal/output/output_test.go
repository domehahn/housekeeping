package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderTableEmptyAndFooter(t *testing.T) {
	var out bytes.Buffer
	err := RenderTable(&out, Table{Headers: []string{"ID", "Name"}, Footer: "0 results"})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"ID", "Name", "(no results)", "0 results"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output %q does not contain %q", got, want)
		}
	}
}

func TestMachineFormats(t *testing.T) {
	data := struct {
		Value string `json:"value" yaml:"value"`
	}{Value: "ok"}
	var jsonOut, yamlOut bytes.Buffer
	if err := RenderJSON(&jsonOut, data); err != nil {
		t.Fatal(err)
	}
	if err := RenderYAML(&yamlOut, data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonOut.String(), `"value": "ok"`) || !strings.Contains(yamlOut.String(), "value: ok") {
		t.Fatalf("unexpected machine output: JSON=%q YAML=%q", jsonOut.String(), yamlOut.String())
	}
}
