package tools

import (
	"context"

	"github.com/nlook-service/nlook-router/internal/apiclient"
)

// StaticLister returns a fixed list of tools. Useful for tests or config-driven tool list.
type StaticLister struct {
	Tools []apiclient.ToolMeta
}

// ListTools returns the configured tool list.
func (s *StaticLister) ListTools(ctx context.Context) ([]apiclient.ToolMeta, error) {
	if s.Tools == nil {
		return nil, nil
	}
	return s.Tools, nil
}
