package tools

import "context"

// Bridge provides tool list and execution (e.g. via Python tools-bridge CLI or HTTP).
type Bridge interface {
	Lister
	Executor
}

// Executor runs a single tool by name with given arguments.
// Returns JSON bytes (e.g. {"status":"success","result":...} or {"status":"failure","error":...}).
type Executor interface {
	Execute(ctx context.Context, name string, args map[string]interface{}) ([]byte, error)
}
