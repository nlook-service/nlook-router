package config

import "os"

// Env keys for overrides.
const (
	EnvAPIURL = "NLOOK_API_URL"
	EnvAPIKey = "NLOOK_API_KEY"
)

// ApplyEnv overrides config with environment variables when set.
func ApplyEnv(c *Config) {
	if v := os.Getenv(EnvAPIURL); v != "" {
		c.APIURL = v
	}
	if v := os.Getenv(EnvAPIKey); v != "" {
		c.APIKey = v
	}
}
