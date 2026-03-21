package tools

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestSearchWeb(t *testing.T) {
	key := os.Getenv("SERPER_API_KEY")
	if key == "" {
		key = "56a86d31ed90875485f301d95626b2f26e8f3098"
		os.Setenv("SERPER_API_KEY", key)
	}
	w := NewWebTools()

	start := time.Now()
	result, err := w.SearchWeb(context.Background(), "오늘 BTS 공연 몇시")
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		t.Fatalf("SearchWeb failed: %v", err)
	}
	fmt.Printf("SearchWeb: %dms, %d chars\n", elapsed, len(result))
	if len(result) > 500 {
		fmt.Println(result[:500] + "...")
	} else {
		fmt.Println(result)
	}

	if elapsed > 5000 {
		t.Errorf("SearchWeb too slow: %dms", elapsed)
	}
}

func TestReadURL(t *testing.T) {
	w := NewWebTools()

	start := time.Now()
	result, err := w.ReadURL(context.Background(), "https://www.agno.com/")
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		t.Fatalf("ReadURL failed: %v", err)
	}
	fmt.Printf("ReadURL: %dms, %d chars\n", elapsed, len(result))
	if len(result) > 500 {
		fmt.Println(result[:500] + "...")
	} else {
		fmt.Println(result)
	}

	if elapsed > 10000 {
		t.Errorf("ReadURL too slow: %dms", elapsed)
	}
}

func TestHtmlToText(t *testing.T) {
	html := `<html><head><title>Test</title><script>var x=1;</script><style>.a{color:red}</style></head>
	<body><h1>Hello</h1><p>World &amp; <b>Bold</b></p><!-- comment --></body></html>`

	text := htmlToText(html)
	if len(text) == 0 {
		t.Error("empty text")
	}
	if !contains(text, "Hello") || !contains(text, "World") || !contains(text, "Bold") {
		t.Errorf("missing content: %s", text)
	}
	if contains(text, "var x") || contains(text, "color:red") || contains(text, "comment") {
		t.Errorf("should strip script/style/comment: %s", text)
	}
	fmt.Println("htmlToText:", text)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
