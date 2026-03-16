package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds router and API settings.
type Config struct {
	APIURL   string `yaml:"api_url"`
	APIKey   string `yaml:"api_key"`
	RouterID string `yaml:"router_id"`
	Port     int    `yaml:"port"`

	// ToolsBridgeDir is the path to the tools-bridge directory (for CLI bridge). If empty, tools list/execute are not available.
	ToolsBridgeDir string `yaml:"tools_bridge_dir,omitempty"`
}

// Default returns a config with default values.
func Default() *Config {
	return &Config{
		APIURL: "https://nlook.me",
		Port:   3333,
	}
}

// Load reads config from path. If the file does not exist, returns Default().
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if c.APIURL == "" {
		c.APIURL = Default().APIURL
	}
	if c.Port == 0 {
		c.Port = Default().Port
	}
	return &c, nil
}

// Save writes config to path. Creates parent directory if needed.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("mkdir config dir: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}
