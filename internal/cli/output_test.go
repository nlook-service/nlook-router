package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestPrintJSON(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()

	err := PrintJSON(map[string]int{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()
	if !strings.Contains(out, "a") || !strings.Contains(out, "1") {
		t.Errorf("PrintJSON output unexpected: %s", out)
	}
}

func TestPrintTable_JSONOutput(t *testing.T) {
	old := os.Stdout
	oldJSON := JSONOutput
	defer func() { os.Stdout = old; JSONOutput = oldJSON }()

	r, w, _ := os.Pipe()
	os.Stdout = w
	JSONOutput = true

	err := PrintTable(
		[]string{"ID", "Name"},
		[][]string{{"1", "foo"}},
		[]map[string]string{{"id": "1", "name": "foo"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()
	if !strings.Contains(out, "id") || !strings.Contains(out, "foo") {
		t.Errorf("PrintTable json output unexpected: %s", out)
	}
}

func TestPrintTable_TableOutput(t *testing.T) {
	old := os.Stdout
	oldJSON := JSONOutput
	defer func() { os.Stdout = old; JSONOutput = oldJSON }()

	r, w, _ := os.Pipe()
	os.Stdout = w
	JSONOutput = false

	err := PrintTable(
		[]string{"ID", "Name"},
		[][]string{{"1", "foo"}, {"2", "bar"}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()
	if !strings.Contains(out, "ID") || !strings.Contains(out, "Name") {
		t.Errorf("table header missing: %s", out)
	}
	if !strings.Contains(out, "1") || !strings.Contains(out, "foo") {
		t.Errorf("table row missing: %s", out)
	}
}

func TestMask(t *testing.T) {
	if mask("ab") != "****" {
		t.Errorf("short string want **** got %s", mask("ab"))
	}
	if mask("abcdef") != "ab****ef" {
		t.Errorf("want ab****ef got %s", mask("abcdef"))
	}
}
