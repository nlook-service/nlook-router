package agentproxy

import (
	"path/filepath"
	"regexp"
)

// dangerousPattern matches shell metacharacters that could enable injection.
var dangerousPattern = regexp.MustCompile("[;|&`$(){}\\[\\]<>!\\\\]")

// SanitizeArgs removes arguments containing dangerous shell metacharacters.
func SanitizeArgs(args []string) []string {
	safe := make([]string, 0, len(args))
	for _, arg := range args {
		if dangerousPattern.MatchString(arg) {
			continue
		}
		safe = append(safe, arg)
	}
	return safe
}

// IsCommandAllowed checks if the base name of cmd is in the allowed list.
func IsCommandAllowed(cmd string, allowed []string) bool {
	base := filepath.Base(cmd)
	for _, a := range allowed {
		if base == a {
			return true
		}
	}
	return false
}
