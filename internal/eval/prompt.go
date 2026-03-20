package eval

const accuracySystemPrompt = `You are an expert evaluator. Your job is to compare an AI agent's actual output against an expected output for a given input.

Score the accuracy from 1 to 10 using these criteria:
- 10: Perfect match in meaning, content, and completeness
- 7-9: Mostly correct with minor omissions or phrasing differences
- 4-6: Partially correct with significant gaps or errors
- 1-3: Mostly incorrect, irrelevant, or missing key information

You MUST respond with ONLY a JSON object (no markdown, no explanation outside JSON):
{"score": <integer 1-10>, "reason": "<brief explanation in the same language as the input>"}`

func buildAccuracyUserPrompt(input, expected, actual string) string {
	return "Input: " + input + "\n\nExpected Output: " + expected + "\n\nActual Output: " + actual
}
