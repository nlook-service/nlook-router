package tools

import "github.com/nlook-service/nlook-router/internal/apiclient"

// BuiltInTools returns the default set of tools available on every router.
func BuiltInTools() []apiclient.ToolMeta {
	return []apiclient.ToolMeta{
		// HTTP / API
		{
			Name:        "web_search",
			Description: "Search the web and return results",
			Category:    "web",
		},
		{
			Name:        "web_fetch",
			Description: "Fetch content from a URL",
			Category:    "web",
		},
		{
			Name:        "http_request",
			Description: "Make HTTP requests (GET, POST, PUT, DELETE) to external APIs",
			Category:    "api",
		},

		// Code / Script
		{
			Name:        "code_interpreter",
			Description: "Execute Python code and return results",
			Category:    "code",
		},
		{
			Name:        "calculator",
			Description: "Evaluate mathematical expressions",
			Category:    "code",
		},

		// File System
		{
			Name:        "file_read",
			Description: "Read file contents from local filesystem",
			Category:    "file",
		},
		{
			Name:        "file_write",
			Description: "Write content to a file on local filesystem",
			Category:    "file",
		},
		{
			Name:        "csv_reader",
			Description: "Parse and query CSV/Excel files",
			Category:    "file",
		},

		// Database
		{
			Name:        "sql_query",
			Description: "Execute SQL queries on configured databases",
			Category:    "database",
		},
		{
			Name:        "vector_search",
			Description: "Semantic search over vector database",
			Category:    "database",
		},

		// Message / Notification
		{
			Name:        "send_email",
			Description: "Send email notifications",
			Category:    "message",
		},
		{
			Name:        "send_slack",
			Description: "Send messages to Slack channels",
			Category:    "message",
		},

		// Web / Browser
		{
			Name:        "browser_scrape",
			Description: "Scrape web pages and extract structured data",
			Category:    "web",
		},

		// Image
		{
			Name:        "image_gen",
			Description: "Generate images from text descriptions",
			Category:    "code",
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
