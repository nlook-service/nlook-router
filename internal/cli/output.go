package cli

import (
	"encoding/json"
	"os"
)

// JSONOutput is true when --json is set.
var JSONOutput bool

// PrintJSON writes v as JSON to stdout.
func PrintJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// PrintTable prints headers and rows as a simple table (or JSON if JSONOutput).
func PrintTable(headers []string, rows [][]string, asJSON interface{}) error {
	if JSONOutput && asJSON != nil {
		return PrintJSON(asJSON)
	}
	// Simple text: header line then rows
	for _, h := range headers {
		os.Stdout.WriteString(h)
		os.Stdout.WriteString("\t")
	}
	os.Stdout.WriteString("\n")
	for _, row := range rows {
		for _, c := range row {
			os.Stdout.WriteString(c)
			os.Stdout.WriteString("\t")
		}
		os.Stdout.WriteString("\n")
	}
	return nil
}
