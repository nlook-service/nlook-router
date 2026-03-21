package orchestration

import (
	"log"
	"net/http"
	"strings"
	"time"
)

// ModelRegistry maps roles to models with fallback chains.
type ModelRegistry struct {
	roles    map[Role]string
	fallback map[Role][]string
	client   *http.Client
}

// NewModelRegistry creates a registry from config role mappings.
func NewModelRegistry(roles map[Role]string) *ModelRegistry {
	r := &ModelRegistry{
		roles:    roles,
		fallback: defaultFallbacks(),
		client:   &http.Client{Timeout: 2 * time.Second},
	}
	return r
}

// Resolve returns the best available model for a role.
func (r *ModelRegistry) Resolve(role Role) string {
	primary, ok := r.roles[role]
	if !ok {
		log.Printf("orchestration/registry: unknown role %s, fallback to gemma3:4b", role)
		return "gemma3:4b"
	}

	if r.isAvailable(primary) {
		return primary
	}

	chain, ok := r.fallback[role]
	if !ok {
		return primary
	}
	for _, model := range chain {
		if r.isAvailable(model) {
			log.Printf("orchestration/registry: %s unavailable for %s, using fallback %s", primary, role, model)
			return model
		}
	}

	return primary
}

// isAvailable checks whether a model can be used right now.
func (r *ModelRegistry) isAvailable(model string) bool {
	if isClaudeModel(model) {
		return true // Claude CLI assumed available if installed
	}
	// Check Ollama
	resp, err := r.client.Get("http://localhost:11434/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func isClaudeModel(model string) bool {
	m := strings.ToLower(model)
	return strings.Contains(m, "claude") || strings.Contains(m, "haiku") ||
		strings.Contains(m, "sonnet") || strings.Contains(m, "opus")
}

func defaultFallbacks() map[Role][]string {
	return map[Role][]string{
		RoleScout:    {"qwen3:4b", "claude-haiku-4-5-20251001"},
		RoleThinker:  {"gemma3:4b"},
		RoleBuilder:  {"claude-sonnet-4-6"},
		RoleSearcher: {"gemma3:4b"},
	}
}
