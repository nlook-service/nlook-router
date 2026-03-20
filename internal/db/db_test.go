package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nlook-service/nlook-router/internal/eval"
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
			t.Run("Eval", func(t *testing.T) { testEval(t, d) })
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

func testEval(t *testing.T, d DB) {
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	// Skip for FileDB (not supported)
	if _, ok := d.(*FileDB); ok {
		t.Skip("eval not supported in file mode")
	}

	// --- EvalSet CRUD ---
	set := &eval.EvalSet{
		ID:         "set-1",
		Name:       "chat-accuracy",
		TargetType: "chat",
		Model:      "qwen3:4b",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := d.UpsertEvalSet(ctx, set); err != nil {
		t.Fatalf("upsert eval set: %v", err)
	}

	got, err := d.GetEvalSet(ctx, "set-1")
	if err != nil {
		t.Fatalf("get eval set: %v", err)
	}
	if got == nil || got.Name != "chat-accuracy" || got.TargetType != "chat" {
		t.Fatalf("unexpected eval set: %+v", got)
	}

	sets, err := d.ListEvalSets(ctx)
	if err != nil || len(sets) != 1 {
		t.Fatalf("list eval sets: err=%v len=%d", err, len(sets))
	}

	// Update
	set.Name = "chat-accuracy-v2"
	set.UpdatedAt = now.Add(time.Minute)
	if err := d.UpsertEvalSet(ctx, set); err != nil {
		t.Fatalf("upsert eval set (update): %v", err)
	}
	got, _ = d.GetEvalSet(ctx, "set-1")
	if got.Name != "chat-accuracy-v2" {
		t.Fatalf("update failed, name=%s", got.Name)
	}

	// --- EvalCase CRUD ---
	case1 := &eval.EvalCase{
		ID:             "case-1",
		EvalSetID:      "set-1",
		Input:          "오늘 할일 보여줘",
		ExpectedOutput: "할일 목록을 조회합니다",
		Context:        "",
		Metadata:       `{"category":"task-query"}`,
		CreatedAt:      now,
	}
	case2 := &eval.EvalCase{
		ID:             "case-2",
		EvalSetID:      "set-1",
		Input:          "새 문서 만들어줘",
		ExpectedOutput: "새 문서를 생성합니다",
		CreatedAt:      now.Add(time.Second),
	}
	if err := d.InsertEvalCase(ctx, case1); err != nil {
		t.Fatalf("insert case 1: %v", err)
	}
	if err := d.InsertEvalCase(ctx, case2); err != nil {
		t.Fatalf("insert case 2: %v", err)
	}

	cases, err := d.ListEvalCases(ctx, "set-1")
	if err != nil || len(cases) != 2 {
		t.Fatalf("list cases: err=%v len=%d", err, len(cases))
	}
	if cases[0].Input != "오늘 할일 보여줘" {
		t.Fatalf("unexpected first case input: %s", cases[0].Input)
	}

	if err := d.DeleteEvalCase(ctx, "case-2"); err != nil {
		t.Fatalf("delete case: %v", err)
	}
	cases, _ = d.ListEvalCases(ctx, "set-1")
	if len(cases) != 1 {
		t.Fatalf("expected 1 case after delete, got %d", len(cases))
	}

	// --- EvalRun CRUD ---
	run := &eval.EvalRun{
		ID:             "run-1",
		EvalSetID:      "set-1",
		EvaluatorModel: "qwen3:4b",
		TargetModel:    "qwen3:4b",
		Status:         "running",
		NumIterations:  2,
		TotalCases:     2,
		StartedAt:      now,
	}
	if err := d.InsertEvalRun(ctx, run); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	gotRun, err := d.GetEvalRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if gotRun == nil || gotRun.Status != "running" {
		t.Fatalf("unexpected run: %+v", gotRun)
	}

	run.Status = "completed"
	run.CompletedCases = 2
	run.AvgScore = 8.5
	run.StdDev = 0.7
	run.CompletedAt = now.Add(30 * time.Second)
	if err := d.UpdateEvalRun(ctx, run); err != nil {
		t.Fatalf("update run: %v", err)
	}
	gotRun, _ = d.GetEvalRun(ctx, "run-1")
	if gotRun.Status != "completed" || gotRun.AvgScore != 8.5 {
		t.Fatalf("update run failed: status=%s avg=%.1f", gotRun.Status, gotRun.AvgScore)
	}

	runs, err := d.ListEvalRuns(ctx, "set-1")
	if err != nil || len(runs) != 1 {
		t.Fatalf("list runs: err=%v len=%d", err, len(runs))
	}

	// --- EvalResult CRUD ---
	result := &eval.EvalResult{
		ID:             "result-1",
		EvalRunID:      "run-1",
		EvalCaseID:     "case-1",
		Iteration:      1,
		ActualOutput:   "오늘의 할일 목록입니다",
		AccuracyScore:  8,
		AccuracyReason: "핵심 의도 일치, 표현 차이",
		LatencyMs:      1200,
		TokensIn:       150,
		TokensOut:      320,
		CreatedAt:      now,
	}
	if err := d.InsertEvalResult(ctx, result); err != nil {
		t.Fatalf("insert result: %v", err)
	}

	results, err := d.ListEvalResults(ctx, "run-1")
	if err != nil || len(results) != 1 {
		t.Fatalf("list results: err=%v len=%d", err, len(results))
	}
	if results[0].AccuracyScore != 8 || results[0].LatencyMs != 1200 {
		t.Fatalf("unexpected result: score=%d latency=%d", results[0].AccuracyScore, results[0].LatencyMs)
	}

	// --- Cascade delete ---
	if err := d.DeleteEvalSet(ctx, "set-1"); err != nil {
		t.Fatalf("delete eval set: %v", err)
	}
	got, _ = d.GetEvalSet(ctx, "set-1")
	if got != nil {
		t.Fatal("eval set should be deleted")
	}
	cases, _ = d.ListEvalCases(ctx, "set-1")
	if len(cases) != 0 {
		t.Fatalf("cases should be deleted after set delete, got %d", len(cases))
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
