package memory

import (
	"context"
	"testing"
	"time"
)

func TestSummarizeStrategySkipsSingleMemory(t *testing.T) {
	// SummarizeStrategy should return single memory unchanged
	s := &SummarizeStrategy{client: nil, model: "test"}

	input := []UserMemory{
		{ID: "m1", Memory: "single memory", TokenCount: 10},
	}

	result, err := s.Optimize(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(result))
	}
	if result[0].ID != "m1" {
		t.Error("expected original memory to be returned unchanged")
	}
}

func TestCountMemoryTokens(t *testing.T) {
	memories := []UserMemory{
		{Memory: "fact one", TokenCount: 10},
		{Memory: "fact two", TokenCount: 20},
		{Memory: "fact three", TokenCount: 0}, // should be estimated
	}

	total := countMemoryTokens(memories)
	if total < 30 {
		t.Errorf("expected >= 30 tokens, got %d", total)
	}
}

func TestCountMemoryTokensEmpty(t *testing.T) {
	total := countMemoryTokens(nil)
	if total != 0 {
		t.Errorf("expected 0 for nil, got %d", total)
	}
}

func TestUserMemoryStruct(t *testing.T) {
	now := time.Now()
	m := UserMemory{
		ID:         "test-id",
		Memory:     "User prefers Go",
		Topics:     []string{"language", "preference"},
		UserID:     42,
		TokenCount: 5,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if m.ID != "test-id" {
		t.Errorf("expected test-id, got %s", m.ID)
	}
	if len(m.Topics) != 2 {
		t.Errorf("expected 2 topics, got %d", len(m.Topics))
	}
	if m.UserID != 42 {
		t.Errorf("expected user ID 42, got %d", m.UserID)
	}
}
