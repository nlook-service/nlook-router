package tools

import "github.com/nlook-service/nlook-router/internal/apiclient"

// BuiltInTools returns the default set of tools available on every router.
// Names match actual Agno toolkit function names for direct bridge execution.
func BuiltInTools() []apiclient.ToolMeta {
	return []apiclient.ToolMeta{
		// Web Search (SerperTools)
		{
			Name:        "search_web",
			Description: "Search the web and return results",
			Category:    "web",
		},
		{
			Name:        "search_news",
			Description: "Search news articles",
			Category:    "web",
		},
		{
			Name:        "read_url",
			Description: "Fetch and read content from a URL",
			Category:    "web",
		},
		{
			Name:        "scrape_webpage",
			Description: "Scrape web pages and extract structured data",
			Category:    "web",
		},

		// Code / Script (PythonTools, ShellTools)
		{
			Name:        "run_python_code",
			Description: "Execute Python code and return results",
			Category:    "code",
		},
		{
			Name:        "run_shell",
			Description: "Run a shell command",
			Category:    "code",
		},

		// Calculator (CalculatorTools)
		{
			Name:        "add",
			Description: "Add two numbers",
			Category:    "math",
		},

		// File System (FileTools)
		{
			Name:        "read_file",
			Description: "Read file contents from local filesystem",
			Category:    "file",
		},
		{
			Name:        "save_file",
			Description: "Write content to a file on local filesystem",
			Category:    "file",
		},
		{
			Name:        "list_files",
			Description: "List files in a directory",
			Category:    "file",
		},
		{
			Name:        "search_files",
			Description: "Search files by pattern",
			Category:    "file",
		},
		{
			Name:        "search_content",
			Description: "Search content within files",
			Category:    "file",
		},

		// HackerNews
		{
			Name:        "get_top_hackernews_stories",
			Description: "Get top stories from HackerNews",
			Category:    "web",
		},
	}
}

// MergeTools merges two tool lists. Later tools override earlier ones by name.
func MergeTools(base, override []apiclient.ToolMeta) []apiclient.ToolMeta {
	seen := make(map[string]int, len(base))
	result := make([]apiclient.ToolMeta, len(base))
	copy(result, base)

	for i, t := range result {
		seen[t.Name] = i
	}

	for _, t := range override {
		if idx, exists := seen[t.Name]; exists {
			result[idx] = t // override
		} else {
			seen[t.Name] = len(result)
			result = append(result, t)
		}
	}

	return result
}
