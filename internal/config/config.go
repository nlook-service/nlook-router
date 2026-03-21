package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

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

	// LLM engine settings (read from config.yaml, exported as env vars for llm.NewEngine)
	LLMEngine   string `yaml:"llm_engine,omitempty"`   // "vllm" or "ollama"
	AIModel     string `yaml:"ai_model,omitempty"`     // e.g. "qwen3:4b"
	VLLMBaseURL string `yaml:"vllm_base_url,omitempty"` // e.g. "http://localhost:18000"

	// Cloud LLM fallback for complex tasks (SEO, document generation, analysis)
	GeminiAPIKey    string `yaml:"gemini_api_key,omitempty"`    // Gemini API key
	CloudModel      string `yaml:"cloud_model,omitempty"`       // e.g. "gemini-2.0-flash-lite"
	AnthropicAPIKey string `yaml:"anthropic_api_key,omitempty"` // Claude Haiku fallback

	// Reasoning model: used when :thinking mode is enabled (e.g. "claude-sonnet-4-6")
	ReasoningModel string `yaml:"reasoning_model,omitempty"`

	// Agent terminal settings (Claude Code CLI execution in workspaces)
	Agent AgentConfig `yaml:"agent,omitempty"`

	// DB settings
	DB DBConfig `yaml:"db,omitempty"`

	// Eval settings
	Eval EvalConfig `yaml:"eval,omitempty"`
}

// DBConfig holds settings for the unified storage layer.
type DBConfig struct {
	Driver  string `yaml:"driver,omitempty"`   // "file" (default) | "sqlite"
	DataDir string `yaml:"data_dir,omitempty"` // default: ~/.nlook
}

// EvalConfig holds settings for the evaluation framework.
type EvalConfig struct {
	EvaluatorModel    string `yaml:"evaluator_model,omitempty"`    // model used to score outputs
	DefaultIterations int    `yaml:"default_iterations,omitempty"` // default iterations per case
	MaxIterations     int    `yaml:"max_iterations,omitempty"`     // max allowed iterations
	TimeoutSeconds    int    `yaml:"timeout_seconds,omitempty"`    // per-case timeout
}

// AgentConfig holds settings for the agent terminal proxy.
type AgentConfig struct {
	Workspaces      []string      `yaml:"workspaces,omitempty"`
	MaxSessions     int           `yaml:"max_sessions,omitempty"`
	SessionTimeout  time.Duration `yaml:"session_timeout,omitempty"`
	AllowedCommands []string      `yaml:"allowed_commands,omitempty"`
}

// AgentDefaults returns default agent config values.
func AgentDefaults() AgentConfig {
	return AgentConfig{
		MaxSessions:     5,
		SessionTimeout:  60 * time.Minute,
		AllowedCommands: []string{"claude"},
	}
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
	defaults := AgentDefaults()
	if c.Agent.MaxSessions == 0 {
		c.Agent.MaxSessions = defaults.MaxSessions
	}
	if c.Agent.SessionTimeout == 0 {
		c.Agent.SessionTimeout = defaults.SessionTimeout
	}
	if len(c.Agent.AllowedCommands) == 0 {
		c.Agent.AllowedCommands = defaults.AllowedCommands
	}
	if c.Eval.DefaultIterations == 0 {
		c.Eval.DefaultIterations = 1
	}
	if c.Eval.MaxIterations == 0 {
		c.Eval.MaxIterations = 10
	}
	if c.Eval.TimeoutSeconds == 0 {
		c.Eval.TimeoutSeconds = 120
	}
	return &c, nil
}

// ApplyLLMEnv exports LLM-related config fields as environment variables
// so that llm.NewEngine() can pick them up.
func (c *Config) ApplyLLMEnv() {
	log.Printf("config: ApplyLLMEnv called (engine=%q model=%q vllm_url=%q)", c.LLMEngine, c.AIModel, c.VLLMBaseURL)
	if c.LLMEngine != "" && os.Getenv("NLOOK_LLM_ENGINE") == "" {
		os.Setenv("NLOOK_LLM_ENGINE", c.LLMEngine)
	}
	if c.AIModel != "" && os.Getenv("NLOOK_AI_MODEL") == "" {
		os.Setenv("NLOOK_AI_MODEL", c.AIModel)
	}
	if c.VLLMBaseURL != "" && os.Getenv("VLLM_BASE_URL") == "" {
		os.Setenv("VLLM_BASE_URL", c.VLLMBaseURL)
	}
	if c.GeminiAPIKey != "" && os.Getenv("GEMINI_API_KEY") == "" {
		os.Setenv("GEMINI_API_KEY", c.GeminiAPIKey)
	}
	if c.CloudModel != "" && os.Getenv("NLOOK_CLOUD_MODEL") == "" {
		os.Setenv("NLOOK_CLOUD_MODEL", c.CloudModel)
	}
	if c.AnthropicAPIKey != "" && os.Getenv("ANTHROPIC_API_KEY") == "" {
		os.Setenv("ANTHROPIC_API_KEY", c.AnthropicAPIKey)
	}
	log.Printf("config: env after apply: VLLM_BASE_URL=%q NLOOK_LLM_ENGINE=%q NLOOK_AI_MODEL=%q GEMINI=%v",
		os.Getenv("VLLM_BASE_URL"), os.Getenv("NLOOK_LLM_ENGINE"), os.Getenv("NLOOK_AI_MODEL"), os.Getenv("GEMINI_API_KEY") != "")
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
