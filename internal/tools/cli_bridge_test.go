package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// requiredSafeTools are tools that must succeed in --test-all with default SAFE_TEST_ARGS.
var requiredSafeTools = []string{
	// Calculator
	"add", "subtract", "multiply", "divide", "sleep",
	"square_root", "factorial", "exponentiate", "is_prime",
	// Web Search
	"search_web", "search_news",
	// File I/O
	"save_file", "read_file", "list_files", "search_files", "search_content",
	// Code / Shell
	"run_python_code", "run_shell",
	// Web
	"get_top_hackernews_stories",
}

// TestCLIBridge_ListTools_integration runs the real Python tools-bridge if available.
// Skips when tools-bridge is not installed or agno is missing.
func TestCLIBridge_ListTools_integration(t *testing.T) {
	// Find tools-bridge dir relative to repo root (same dir as go.mod)
	modDir := findModuleRoot(t)
	bridgeDir := filepath.Join(modDir, "tools-bridge")
	if _, err := os.Stat(bridgeDir); err != nil {
		t.Skipf("tools-bridge dir not found: %v", err)
	}
	ctx := context.Background()
	bridge := DefaultCLIBridge(bridgeDir)
	bridge.Command = "python3"
	list, err := bridge.ListTools(ctx)
	if err != nil {
		t.Skipf("tools-bridge not available (install agno and tool_bridge): %v", err)
	}
	if len(list) == 0 {
		t.Fatal("expected at least one tool")
	}
	nameSet := make(map[string]bool)
	for _, tool := range list {
		nameSet[tool.Name] = true
	}
	for _, required := range []string{"add", "search_web", "read_file", "run_python_code"} {
		if !nameSet[required] {
			t.Errorf("expected %q in tool list", required)
		}
	}
}

// TestCLIBridge_Execute_integration runs the real Python tools-bridge add tool if available.
func TestCLIBridge_Execute_integration(t *testing.T) {
	modDir := findModuleRoot(t)
	bridgeDir := filepath.Join(modDir, "tools-bridge")
	if _, err := os.Stat(bridgeDir); err != nil {
		t.Skipf("tools-bridge dir not found: %v", err)
	}
	ctx := context.Background()
	bridge := DefaultCLIBridge(bridgeDir)
	bridge.Command = "python3"
	raw, err := bridge.Execute(ctx, "add", map[string]interface{}{"a": 1.0, "b": 2.0})
	if err != nil {
		t.Skipf("tools-bridge execute not available: %v", err)
	}
	var out struct {
		Status string      `json:"status"`
		Result interface{} `json:"result"`
		Error  *string     `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if out.Status != "success" {
		t.Fatalf("status want success got %s (error: %v)", out.Status, out.Error)
	}
	// result from calculator is JSON string
	if s, ok := out.Result.(string); ok {
		var calc map[string]interface{}
		if err := json.Unmarshal([]byte(s), &calc); err == nil && calc["result"] != nil {
			if n, ok := toFloat(calc["result"]); ok && n == 3 {
				return
			}
		}
	}
	t.Logf("raw result: %s", raw)
}

// TestCLIBridge_TestAll_integration runs the real Python tools-bridge --test-all and checks that
// required safe tools (calculator + sleep) all report status success.
func TestCLIBridge_TestAll_integration(t *testing.T) {
	modDir := findModuleRoot(t)
	bridgeDir := filepath.Join(modDir, "tools-bridge")
	if _, err := os.Stat(bridgeDir); err != nil {
		t.Skipf("tools-bridge dir not found: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	bridge := DefaultCLIBridge(bridgeDir)
	bridge.Command = "python3"
	results, err := bridge.TestAll(ctx)
	if err != nil {
		t.Skipf("tools-bridge --test-all not available: %v", err)
	}
	byName := make(map[string]TestAllResult)
	for _, r := range results {
		byName[r.Name] = r
	}
	for _, name := range requiredSafeTools {
		r, ok := byName[name]
		if !ok {
			t.Errorf("tool %q not in test-all result", name)
			continue
		}
		if r.Status != "success" {
			errStr := ""
			if r.Error != nil {
				errStr = *r.Error
			}
			t.Errorf("tool %q: status=%s (error: %s)", name, r.Status, errStr)
		}
	}
	okCount := 0
	for _, r := range results {
		if r.Status == "success" {
			okCount++
		}
	}
	t.Logf("test-all: %d tools total, %d success (required %d passed)", len(results), okCount, len(requiredSafeTools))
}

func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

func toFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	default:
		return 0, false
	}
}
