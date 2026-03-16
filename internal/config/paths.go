package config

import (
	"os"
	"path/filepath"
)

// ConfigDir returns the directory for router config (e.g. ~/.nlook).
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".nlook"
	}
	return filepath.Join(home, ".nlook")
}

// ConfigPath returns the default config file path.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}
