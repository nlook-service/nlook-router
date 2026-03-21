package reasoning

import (
	"fmt"
	"strings"
)

// extractThinkTag parses <think>...</think> from model output.
// Returns (thinkingContent, answerContent).
func extractThinkTag(raw string) (string, string) {
	const startTag = "<think>"
	const endTag = "</think>"

	startIdx := strings.Index(raw, startTag)
	if startIdx == -1 {
		return "", raw
	}

	endIdx := strings.Index(raw, endTag)
	if endIdx == -1 {
		return strings.TrimSpace(raw[startIdx+len(startTag):]), ""
	}

	thinking := strings.TrimSpace(raw[startIdx+len(startTag) : endIdx])
	answer := strings.TrimSpace(raw[endIdx+len(endTag):])
	return thinking, answer
}

// thinkingToSteps converts raw thinking text into structured reasoning steps.
// Splits by double newline; each block becomes one step.
func thinkingToSteps(thinking string) []Step {
	if thinking == "" {
		return nil
	}

	blocks := strings.Split(thinking, "\n\n")
	steps := make([]Step, 0, len(blocks))
	for i, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		action := ActionContinue
		if i == len(blocks)-1 {
			action = ActionFinalAnswer
		}
		steps = append(steps, Step{
			Title:      fmt.Sprintf("Step %d", i+1),
			Reasoning:  block,
			NextAction: action,
		})
	}
	return steps
}

// inThinkingBlock checks if the accumulated text is currently inside an unclosed <think> tag.
func inThinkingBlock(text string) bool {
	lastOpen := strings.LastIndex(text, "<think>")
	lastClose := strings.LastIndex(text, "</think>")
	return lastOpen > lastClose
}
