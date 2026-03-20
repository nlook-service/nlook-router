package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDBImplementations(t *testing.T) {
	for _, tc := range []struct {
		name  string
		newDB func(t *testing.T) DB
	}{
		{"FileDB", newTestFileDB},
		{"SQLiteDB", newTestSQLiteDB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.newDB(t)
			defer d.Close()

			t.Run("Session", func(t *testing.T) { testSession(t, d) })
			t.Run("UserProfile", func(t *testing.T) { testUserProfile(t, d) })
			t.Run("Memory", func(t *testing.T) { testMemory(t, d) })
			t.Run("Summary", func(t *testing.T) { testSummary(t, d) })
			t.Run("Facts", func(t *testing.T) { testFacts(t, d) })
			t.Run("Document", func(t *testing.T) { testDocument(t, d) })
			t.Run("Task", func(t *testing.T) { testTask(t, d) })
			t.Run("Trace", func(t *testing.T) { testTrace(t, d) })
			t.Run("ChatMessage", func(t *testing.T) { testChatMessage(t, d) })
		})
	}
}

func newTestFileDB(t *testing.T) DB {
	dir := t.TempDir()
	d, err := newFileDB(dir)
	if err != nil {
		t.Fatalf("newFileDB: %v", err)
	}
	return d
}

func newTestSQLiteDB(t *testing.T) DB {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := newSQLiteDB(path)
	if err != nil {
		t.Fatalf("newSQLiteDB: %v", err)
	}
	return d
}

func testSession(t *testing.T, d DB) {
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	sess := &Session{
		ID:        "sess-1",
		Type:      "chat",
		State:     "active",
		UserID:    42,
		AgentIDs:  []string{"agent-a"},
		RunIDs:    []int64{100},
		Context:   []byte(`{"key":"val"}`),
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}

	if err := d.UpsertSession(ctx, sess); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := d.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.ID != "sess-1" || got.UserID != 42 {
		t.Fatalf("unexpected session: %+v", got)
	}

	list, err := d.ListSessions(ctx, SessionFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}

	if err := d.DeleteSession(ctx, "sess-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ = d.GetSession(ctx, "sess-1")
	if got != nil {
		t.Fatal("session should be deleted")
	}
}

func testUserProfile(t *testing.T, d DB) {
	ctx := context.Background()
	p := &UserProfile{
		UserID:    1,
		Role:      "developer",
		Interests: []string{"Go", "AI"},
		Notes:     "test",
		Lang:      "ko",
		UpdatedAt: time.Now().Truncate(time.Second),
	}
	if err := d.UpsertUserProfile(ctx, p); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := d.GetUserProfile(ctx, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Role != "developer" {
		t.Fatalf("unexpected role: %s", got.Role)
	}
}

func testMemory(t *testing.T, d DB) {
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	m := &UserMemory{
		ID:         "mem-1",
		UserID:     0,
		Memory:     "user likes Go",
		Topics:     []string{"programming"},
		TokenCount: 10,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := d.UpsertMemory(ctx, m); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	list, err := d.ListMemories(ctx, 0, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}

	count, _ := d.CountMemories(ctx, 0)
	if count != 1 {
		t.Fatalf("count: %d", count)
	}

	tokens, _ := d.TotalMemoryTokens(ctx, 0)
	if tokens != 10 {
		t.Fatalf("tokens: %d", tokens)
	}

	if err := d.DeleteMemory(ctx, "mem-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = d.ListMemories(ctx, 0, 10)
	if len(list) != 0 {
		t.Fatal("memory should be deleted")
	}
}

func testSummary(t *testing.T, d DB) {
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	s := &ConversationSummary{
		ConversationID: 100,
		UserID:         0,
		Summary:        "talked about Go",
		MessageCount:   5,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := d.UpsertSummary(ctx, s); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := d.GetSummary(ctx, 100)
	if err != nil || got == nil {
		t.Fatalf("get: err=%v got=%v", err, got)
	}
	if got.Summary != "talked about Go" {
		t.Fatalf("unexpected summary: %s", got.Summary)
	}
}

func testFacts(t *testing.T, d DB) {
	ctx := context.Background()
	if err := d.AddFact(ctx, 0, "likes coffee"); err != nil {
		t.Fatalf("add: %v", err)
	}
	// Add duplicate — should not error
	if err := d.AddFact(ctx, 0, "likes coffee"); err != nil {
		t.Fatalf("add dup: %v", err)
	}
	facts, err := d.ListFacts(ctx, 0)
	if err != nil || len(facts) != 1 {
		t.Fatalf("list: err=%v len=%d", err, len(facts))
	}
}

func testDocument(t *testing.T, d DB) {
	ctx := context.Background()
	doc := &CachedDocument{
		ID:        1001,
		Title:     "Test Doc",
		Content:   "hello world",
		Tags:      []string{"test"},
		UpdatedAt: time.Now().Truncate(time.Second),
	}
	if err := d.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := d.GetDocument(ctx, 1001)
	if err != nil || got == nil {
		t.Fatalf("get: err=%v got=%v", err, got)
	}
	if got.Title != "Test Doc" {
		t.Fatalf("unexpected title: %s", got.Title)
	}

	results, err := d.SearchDocuments(ctx, "hello", 5)
	if err != nil || len(results) != 1 {
		t.Fatalf("search: err=%v len=%d", err, len(results))
	}

	if err := d.DeleteDocument(ctx, 1001); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func testTask(t *testing.T, d DB) {
	ctx := context.Background()
	task := &CachedTask{
		ID:        2001,
		Title:     "Fix bug",
		Status:    "pending",
		Priority:  "high",
		UpdatedAt: time.Now().Truncate(time.Second),
	}
	if err := d.UpsertTask(ctx, task); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	status := "pending"
	list, err := d.ListTasks(ctx, TaskFilter{Status: &status})
	if err != nil || len(list) != 1 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}

	if err := d.DeleteTask(ctx, 2001); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func testTrace(t *testing.T, d DB) {
	ctx := context.Background()
	event := &TraceEvent{
		EventID:   "evt-1",
		SessionID: "sess-trace",
		Type:      "llm_call",
		Name:      "test",
		Level:     "info",
		Timestamp: time.Now().Truncate(time.Second),
		Metadata:  map[string]interface{}{"model": "test"},
	}
	if err := d.WriteTrace(ctx, event); err != nil {
		t.Fatalf("write: %v", err)
	}

	sid := "sess-trace"
	list, err := d.ListTraces(ctx, TraceFilter{SessionID: &sid})
	if err != nil || len(list) != 1 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}
}

func testChatMessage(t *testing.T, d DB) {
	ctx := context.Background()
	msg := &ChatMessage{
		ConversationID: 500,
		UserID:         1,
		Role:           "user",
		Content:        "hello AI",
		CreatedAt:      time.Now().Truncate(time.Second),
	}
	if err := d.InsertChatMessage(ctx, msg); err != nil {
		t.Fatalf("insert: %v", err)
	}

	list, err := d.ListChatMessages(ctx, 500, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}
	if list[0].Content != "hello AI" {
		t.Fatalf("unexpected content: %s", list[0].Content)
	}
}

func TestFactory(t *testing.T) {
	dir := t.TempDir()

	// Test file driver (default)
	d, err := New("", dir)
	if err != nil {
		t.Fatalf("New file: %v", err)
	}
	d.Close()

	// Test sqlite driver
	d, err = New("sqlite", dir)
	if err != nil {
		t.Fatalf("New sqlite: %v", err)
	}
	d.Close()

	// Verify sqlite file was created
	if _, err := os.Stat(filepath.Join(dir, "nlook.db")); err != nil {
		t.Fatalf("nlook.db not created: %v", err)
	}

	// Test unsupported driver
	_, err = New("postgres", dir)
	if err == nil {
		t.Fatal("expected error for unsupported driver")
	}
}
