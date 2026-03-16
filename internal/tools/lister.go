package tools

import (
	"context"

	"github.com/nlook-service/nlook-router/internal/apiclient"
)

// Lister returns the list of tools available on this router (e.g. from Agno bridge or config).
// Used to populate RegisterPayload.Tools when registering or heartbeating to the server.
type Lister interface {
	ListTools(ctx context.Context) ([]apiclient.ToolMeta, error)
}
