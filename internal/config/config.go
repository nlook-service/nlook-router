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

	// Web search API key (Serper). Falls back to DuckDuckGo if missing/expired.
	SerperAPIKey string `yaml:"serper_api_key,omitempty"`

	// Reasoning models: self-evaluation selects the appropriate tier
	ReasoningModel      string `yaml:"reasoning_model,omitempty"`       // default/medium: e.g. "claude-sonnet-4-6"
	ReasoningModelLight string `yaml:"reasoning_model_light,omitempty"` // light tasks: e.g. "claude-haiku-4-5-20251001"
	ReasoningModelDeep  string `yaml:"reasoning_model_deep,omitempty"`  // deep analysis: e.g. "claude-opus-4-6"

	// Agent terminal settings (Claude Code CLI execution in workspaces)
	Agent AgentConfig `yaml:"agent,omitempty"`

	// DB settings
	DB DBConfig `yaml:"db,omitempty"`

	// Eval settings
	Eval EvalConfig `yaml:"eval,omitempty"`

	// Tool result compression settings
	Compression CompressionConfig `yaml:"compression,omitempty"`

	// Semantic router settings (score-based model cascade)
	SemanticRouter SemanticRouterConfig `yaml:"semantic_router,omitempty"`
}

// SemanticRouterConfig configures the score-based routing system.
type SemanticRouterConfig struct {
	Enabled       bool   `yaml:"enabled"`
	EmbedProvider string `yaml:"embed_provider,omitempty"` // "openai" | "ollama"
	EmbedModel    string `yaml:"embed_model,omitempty"`    // "text-embedding-3-small"
	OpenAIAPIKey  string `yaml:"openai_api_key,omitempty"`
	FallbackModel string `yaml:"fallback_model,omitempty"` // "snowflake-arctic-embed2"
	Thresholds    struct {
		Tier1 float64 `yaml:"tier1_local"`
		Tier2 float64 `yaml:"tier2_fast"`
		Tier3 float64 `yaml:"tier3_balanced"`
	} `yaml:"thresholds,omitempty"`
	Models struct {
		Tier1 string `yaml:"tier1"`
		Tier2 string `yaml:"tier2"`
		Tier3 string `yaml:"tier3"`
		Tier4 string `yaml:"tier4"`
	} `yaml:"models,omitempty"`
	Feedback struct {
		LearningInterval string `yaml:"learning_interval,omitempty"`
		MinSamples       int    `yaml:"min_samples,omitempty"`
	} `yaml:"feedback,omitempty"`
}

// CompressionConfig holds settings for tool result compression.
type CompressionConfig struct {
	Enabled      bool   `yaml:"enabled"`                // default: true
	MaxTokens    int    `yaml:"max_tokens,omitempty"`    // per-result token budget, default: 800
	LLMModel     string `yaml:"llm_model,omitempty"`     // model for LLM compression
	LLMThreshold int    `yaml:"llm_threshold,omitempty"` // use LLM only above this, default: 1200
	RuleMaxItems int    `yaml:"rule_max_items,omitempty"` // max JSON array items, default: 10
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
	// Compression defaults: enabled by default when not configured
	if c.Compression.MaxTokens == 0 && c.Compression.LLMThreshold == 0 && c.Compression.RuleMaxItems == 0 {
		// No compression config at all → use defaults (enabled)
		c.Compression.Enabled = true
		c.Compression.MaxTokens = 800
		c.Compression.LLMThreshold = 1200
		c.Compression.RuleMaxItems = 10
	} else {
		if c.Compression.MaxTokens == 0 {
			c.Compression.MaxTokens = 800
		}
		if c.Compression.LLMThreshold == 0 {
			c.Compression.LLMThreshold = 1200
		}
		if c.Compression.RuleMaxItems == 0 {
			c.Compression.RuleMaxItems = 10
		}
	}
	// Semantic router defaults
	if c.SemanticRouter.Enabled {
		if c.SemanticRouter.EmbedProvider == "" {
			c.SemanticRouter.EmbedProvider = "openai"
		}
		if c.SemanticRouter.EmbedModel == "" {
			c.SemanticRouter.EmbedModel = "text-embedding-3-small"
		}
		if c.SemanticRouter.OpenAIAPIKey == "" {
			c.SemanticRouter.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
		}
		if c.SemanticRouter.Thresholds.Tier1 == 0 {
			c.SemanticRouter.Thresholds.Tier1 = 0.85
		}
		if c.SemanticRouter.Thresholds.Tier2 == 0 {
			c.SemanticRouter.Thresholds.Tier2 = 0.65
		}
		if c.SemanticRouter.Thresholds.Tier3 == 0 {
			c.SemanticRouter.Thresholds.Tier3 = 0.45
		}
		if c.SemanticRouter.Feedback.MinSamples == 0 {
			c.SemanticRouter.Feedback.MinSamples = 20
		}
		if c.SemanticRouter.Feedback.LearningInterval == "" {
			c.SemanticRouter.Feedback.LearningInterval = "24h"
		}
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
	if c.SerperAPIKey != "" && os.Getenv("SERPER_API_KEY") == "" {
		os.Setenv("SERPER_API_KEY", c.SerperAPIKey)
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
