package agentproxy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateWorkspace checks that the requested path is in the allowed list,
// resolves symlinks to prevent traversal, and verifies .git exists.
func ValidateWorkspace(requested string, allowed []string) (string, error) {
	if len(allowed) == 0 {
		return "", fmt.Errorf("no workspaces configured")
	}

	resolved, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return "", fmt.Errorf("resolve workspace path: %w", err)
	}
	resolved = filepath.Clean(resolved)

	for _, a := range allowed {
		aResolved, err := filepath.EvalSymlinks(a)
		if err != nil {
			continue
		}
		aResolved = filepath.Clean(aResolved)
		if resolved == aResolved || strings.HasPrefix(resolved, aResolved+string(filepath.Separator)) {
			if _, err := os.Stat(filepath.Join(resolved, ".git")); err != nil {
				return "", fmt.Errorf("workspace %s has no .git directory", resolved)
			}
			return resolved, nil
		}
	}
	return "", fmt.Errorf("workspace %s is not in the allowed list", requested)
}
