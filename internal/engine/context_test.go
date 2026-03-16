package engine

import (
	"sync"
	"testing"
)

func TestNewRunContext_defaults(t *testing.T) {
	rctx := NewRunContext(1, 2, 3, nil)
	if rctx.RunID != 1 {
		t.Errorf("RunID: got %d, want 1", rctx.RunID)
	}
	if rctx.WorkflowID != 2 {
		t.Errorf("WorkflowID: got %d, want 2", rctx.WorkflowID)
	}
	if rctx.UserID != 3 {
		t.Errorf("UserID: got %d, want 3", rctx.UserID)
	}
	// nil input should be initialised to an empty map
	if rctx.Input == nil {
		t.Error("Input should not be nil when nil is passed")
	}
}

func TestNewRunContext_withInput(t *testing.T) {
	input := map[string]interface{}{"key": "value"}
	rctx := NewRunContext(10, 20, 30, input)
	if rctx.Input["key"] != "value" {
		t.Errorf("Input[key]: got %v, want 'value'", rctx.Input["key"])
	}
}

func TestRunContext_SetAndGetNodeOutput(t *testing.T) {
	rctx := NewRunContext(1, 1, 1, nil)

	output := map[string]interface{}{"result": "hello"}
	rctx.SetNodeOutput("node-1", output)

	got := rctx.GetNodeOutput("node-1")
	if got == nil {
		t.Fatal("expected non-nil output for node-1")
	}
	if got["result"] != "hello" {
		t.Errorf("output[result]: got %v, want 'hello'", got["result"])
	}
}

func TestRunContext_GetNodeOutput_missing(t *testing.T) {
	rctx := NewRunContext(1, 1, 1, nil)
	got := rctx.GetNodeOutput("nonexistent")
	if got != nil {
		t.Errorf("expected nil for missing node, got %v", got)
	}
}

func TestRunContext_SetNodeOutput_overwrite(t *testing.T) {
	rctx := NewRunContext(1, 1, 1, nil)
	rctx.SetNodeOutput("node-1", map[string]interface{}{"v": 1})
	rctx.SetNodeOutput("node-1", map[string]interface{}{"v": 2})

	got := rctx.GetNodeOutput("node-1")
	if got["v"] != 2 {
		t.Errorf("expected overwritten value 2, got %v", got["v"])
	}
}

func TestRunContext_ConcurrentAccess(t *testing.T) {
	rctx := NewRunContext(1, 1, 1, nil)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Concurrent writers
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			nodeID := "node-concurrent"
			rctx.SetNodeOutput(nodeID, map[string]interface{}{"i": i})
		}(i)
	}

	// Concurrent readers
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = rctx.GetNodeOutput("node-concurrent")
		}()
	}

	wg.Wait()
	// If we get here without a race condition panic, the test passes.
}

func TestRunContext_MultipleNodes(t *testing.T) {
	rctx := NewRunContext(1, 1, 1, nil)

	rctx.SetNodeOutput("node-a", map[string]interface{}{"from": "a"})
	rctx.SetNodeOutput("node-b", map[string]interface{}{"from": "b"})

	a := rctx.GetNodeOutput("node-a")
	b := rctx.GetNodeOutput("node-b")

	if a["from"] != "a" {
		t.Errorf("node-a: got %v, want 'a'", a["from"])
	}
	if b["from"] != "b" {
		t.Errorf("node-b: got %v, want 'b'", b["from"])
	}
}
