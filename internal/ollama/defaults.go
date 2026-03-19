package ollama

import "strings"

// ModelDefaults holds optimal generation parameters per model family.
// Source: unsloth studio inference_defaults.json
type ModelDefaults struct {
	Temperature      float64
	TopP             float64
	TopK             int
	RepetitionPenalty float64
	PresencePenalty  float64
	NumPredict       int
}

var modelFamilyDefaults = map[string]ModelDefaults{
	"qwen3.5":       {Temperature: 0.7, TopP: 0.8, TopK: 20, RepetitionPenalty: 1.0, PresencePenalty: 1.5, NumPredict: 4096},
	"qwen3":         {Temperature: 0.6, TopP: 0.95, TopK: 20, RepetitionPenalty: 1.0, NumPredict: 4096},
	"qwen2.5":       {Temperature: 0.7, TopP: 0.8, TopK: 20, RepetitionPenalty: 1.0, NumPredict: 4096},
	"llama3":        {Temperature: 1.5, TopP: 0.95, TopK: -1, RepetitionPenalty: 1.0, NumPredict: 4096},
	"llama3.1":      {Temperature: 1.5, TopP: 0.95, TopK: -1, RepetitionPenalty: 1.0, NumPredict: 4096},
	"llama3.2":      {Temperature: 1.5, TopP: 0.95, TopK: -1, RepetitionPenalty: 1.0, NumPredict: 4096},
	"gemma":         {Temperature: 1.0, TopP: 0.95, TopK: 64, RepetitionPenalty: 1.0, NumPredict: 4096},
	"gemma2":        {Temperature: 1.0, TopP: 0.95, TopK: 64, RepetitionPenalty: 1.0, NumPredict: 4096},
	"gemma3":        {Temperature: 0.8, TopP: 0.95, TopK: 64, RepetitionPenalty: 1.0, NumPredict: 4096},
	"mistral":       {Temperature: 0.7, TopP: 0.95, TopK: -1, RepetitionPenalty: 1.0, NumPredict: 4096},
	"deepseek":      {Temperature: 0.6, TopP: 0.95, TopK: -1, RepetitionPenalty: 1.0, NumPredict: 4096},
	"deepseek-r1":   {Temperature: 0.6, TopP: 0.95, TopK: -1, RepetitionPenalty: 1.0, NumPredict: 4096},
	"phi":           {Temperature: 0.8, TopP: 0.95, TopK: -1, RepetitionPenalty: 1.0, NumPredict: 4096},
}

// Default for unknown models
var defaultModelDefaults = ModelDefaults{
	Temperature: 0.7, TopP: 0.9, TopK: 40, RepetitionPenalty: 1.0, NumPredict: 4096,
}

// GetModelDefaults returns optimal parameters for a model by matching family prefix.
func GetModelDefaults(model string) ModelDefaults {
	lower := strings.ToLower(model)

	// Try longest match first
	for _, prefix := range []string{
		"qwen3.5", "qwen3", "qwen2.5",
		"llama3.2", "llama3.1", "llama3",
		"gemma3", "gemma2", "gemma",
		"deepseek-r1", "deepseek",
		"mistral", "phi",
	} {
		if strings.HasPrefix(lower, prefix) || strings.Contains(lower, prefix) {
			if d, ok := modelFamilyDefaults[prefix]; ok {
				return d
			}
		}
	}
	return defaultModelDefaults
}
