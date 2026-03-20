package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteDB implements DB using SQLite (pure Go, no CGO).
type SQLiteDB struct {
	db   *sql.DB
	path string
}

func newSQLiteDB(path string) (*SQLiteDB, error) {
	db, err := sql.Open("sqlite", path+"?_journal=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite single-writer
	sdb := &SQLiteDB{db: db, path: path}
	if err := sdb.Migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return sdb, nil
}

func (s *SQLiteDB) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schemaDDL)
	return err
}

const schemaDDL = `
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	type       TEXT NOT NULL DEFAULT 'chat',
	state      TEXT NOT NULL DEFAULT 'active',
	user_id    INTEGER NOT NULL DEFAULT 0,
	agent_ids  TEXT DEFAULT '[]',
	run_ids    TEXT DEFAULT '[]',
	context    TEXT DEFAULT '{}',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS user_profiles (
	user_id    INTEGER PRIMARY KEY,
	role       TEXT DEFAULT '',
	interests  TEXT DEFAULT '[]',
	notes      TEXT DEFAULT '',
	lang       TEXT DEFAULT '',
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS user_memories (
	id          TEXT PRIMARY KEY,
	user_id     INTEGER NOT NULL DEFAULT 0,
	memory      TEXT NOT NULL,
	topics      TEXT DEFAULT '[]',
	token_count INTEGER DEFAULT 0,
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memories_user ON user_memories(user_id);

CREATE TABLE IF NOT EXISTS user_facts (
	id      INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL DEFAULT 0,
	fact    TEXT NOT NULL,
	UNIQUE(user_id, fact)
);

CREATE TABLE IF NOT EXISTS conversation_summaries (
	conversation_id INTEGER PRIMARY KEY,
	user_id         INTEGER NOT NULL DEFAULT 0,
	summary         TEXT NOT NULL,
	message_count   INTEGER DEFAULT 0,
	created_at      INTEGER NOT NULL,
	updated_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_summaries_user ON conversation_summaries(user_id);

CREATE TABLE IF NOT EXISTS cached_documents (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL DEFAULT 0,
	title      TEXT NOT NULL,
	content    TEXT DEFAULT '',
	tags       TEXT DEFAULT '[]',
	updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_docs_user ON cached_documents(user_id);

CREATE TABLE IF NOT EXISTS cached_tasks (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL DEFAULT 0,
	title      TEXT NOT NULL,
	status     TEXT DEFAULT 'pending',
	priority   TEXT DEFAULT '',
	notes      TEXT DEFAULT '',
	due_date   TEXT DEFAULT '',
	updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_user ON cached_tasks(user_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON cached_tasks(status);

CREATE TABLE IF NOT EXISTS trace_events (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	event_id    TEXT NOT NULL,
	session_id  TEXT NOT NULL,
	span_id     TEXT DEFAULT '',
	parent_span TEXT DEFAULT '',
	type        TEXT NOT NULL,
	name        TEXT NOT NULL,
	level       TEXT DEFAULT 'info',
	timestamp   INTEGER NOT NULL,
	duration_ms INTEGER DEFAULT 0,
	metadata    TEXT DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_traces_session ON trace_events(session_id);
CREATE INDEX IF NOT EXISTS idx_traces_time ON trace_events(timestamp);

CREATE TABLE IF NOT EXISTS chat_messages (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	conversation_id INTEGER NOT NULL,
	user_id         INTEGER NOT NULL DEFAULT 0,
	session_id      TEXT DEFAULT '',
	role            TEXT NOT NULL,
	content         TEXT NOT NULL,
	model           TEXT DEFAULT '',
	token_count     INTEGER DEFAULT 0,
	created_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_conv ON chat_messages(conversation_id);
CREATE INDEX IF NOT EXISTS idx_chat_user ON chat_messages(user_id);
`

// --- helpers ---

func toEpoch(t time.Time) int64    { return t.Unix() }
func fromEpoch(epoch int64) time.Time { return time.Unix(epoch, 0) }

func jsonMarshal(v interface{}) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func jsonUnmarshalStrings(s string) []string {
	var result []string
	json.Unmarshal([]byte(s), &result)
	return result
}

func jsonUnmarshalInt64s(s string) []int64 {
	var result []int64
	json.Unmarshal([]byte(s), &result)
	return result
}

func jsonUnmarshalMap(s string) map[string]interface{} {
	var result map[string]interface{}
	json.Unmarshal([]byte(s), &result)
	return result
}

// --- Session ---

func (s *SQLiteDB) UpsertSession(ctx context.Context, sess *Session) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, type, state, user_id, agent_ids, run_ids, context, created_at, updated_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type=excluded.type, state=excluded.state, user_id=excluded.user_id,
			agent_ids=excluded.agent_ids, run_ids=excluded.run_ids, context=excluded.context,
			updated_at=excluded.updated_at, expires_at=excluded.expires_at`,
		sess.ID, sess.Type, sess.State, sess.UserID,
		jsonMarshal(sess.AgentIDs), jsonMarshal(sess.RunIDs), string(sess.Context),
		toEpoch(sess.CreatedAt), toEpoch(sess.UpdatedAt), toEpoch(sess.ExpiresAt))
	return err
}

func (s *SQLiteDB) GetSession(ctx context.Context, id string) (*Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, type, state, user_id, agent_ids, run_ids, context, created_at, updated_at, expires_at FROM sessions WHERE id=?`, id)
	return scanSession(row)
}

func scanSession(row *sql.Row) (*Session, error) {
	var sess Session
	var agentIDs, runIDs, ctxData string
	var createdAt, updatedAt, expiresAt int64
	err := row.Scan(&sess.ID, &sess.Type, &sess.State, &sess.UserID, &agentIDs, &runIDs, &ctxData, &createdAt, &updatedAt, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sess.AgentIDs = jsonUnmarshalStrings(agentIDs)
	sess.RunIDs = jsonUnmarshalInt64s(runIDs)
	sess.Context = []byte(ctxData)
	sess.CreatedAt = fromEpoch(createdAt)
	sess.UpdatedAt = fromEpoch(updatedAt)
	sess.ExpiresAt = fromEpoch(expiresAt)
	return &sess, nil
}

func (s *SQLiteDB) ListSessions(ctx context.Context, f SessionFilter) ([]*Session, error) {
	q := "SELECT id, type, state, user_id, agent_ids, run_ids, context, created_at, updated_at, expires_at FROM sessions WHERE 1=1"
	var args []interface{}
	if f.UserID != nil {
		q += " AND user_id=?"
		args = append(args, *f.UserID)
	}
	if f.Type != nil {
		q += " AND type=?"
		args = append(args, *f.Type)
	}
	if f.State != nil {
		q += " AND state=?"
		args = append(args, *f.State)
	}
	if f.After != nil {
		q += " AND created_at>=?"
		args = append(args, toEpoch(*f.After))
	}
	if f.Before != nil {
		q += " AND created_at<=?"
		args = append(args, toEpoch(*f.Before))
	}
	q += " ORDER BY updated_at DESC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Session
	for rows.Next() {
		var sess Session
		var agentIDs, runIDs, ctxData string
		var createdAt, updatedAt, expiresAt int64
		if err := rows.Scan(&sess.ID, &sess.Type, &sess.State, &sess.UserID, &agentIDs, &runIDs, &ctxData, &createdAt, &updatedAt, &expiresAt); err != nil {
			return nil, err
		}
		sess.AgentIDs = jsonUnmarshalStrings(agentIDs)
		sess.RunIDs = jsonUnmarshalInt64s(runIDs)
		sess.Context = []byte(ctxData)
		sess.CreatedAt = fromEpoch(createdAt)
		sess.UpdatedAt = fromEpoch(updatedAt)
		sess.ExpiresAt = fromEpoch(expiresAt)
		result = append(result, &sess)
	}
	return result, nil
}

func (s *SQLiteDB) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id=?", id)
	return err
}

func (s *SQLiteDB) DeleteExpiredSessions(ctx context.Context, before time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at<?", toEpoch(before))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// --- User Profile ---

func (s *SQLiteDB) UpsertUserProfile(ctx context.Context, p *UserProfile) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_profiles (user_id, role, interests, notes, lang, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			role=excluded.role, interests=excluded.interests, notes=excluded.notes,
			lang=excluded.lang, updated_at=excluded.updated_at`,
		p.UserID, p.Role, jsonMarshal(p.Interests), p.Notes, p.Lang, toEpoch(p.UpdatedAt))
	return err
}

func (s *SQLiteDB) GetUserProfile(ctx context.Context, userID int64) (*UserProfile, error) {
	row := s.db.QueryRowContext(ctx, "SELECT user_id, role, interests, notes, lang, updated_at FROM user_profiles WHERE user_id=?", userID)
	var p UserProfile
	var interests string
	var updatedAt int64
	err := row.Scan(&p.UserID, &p.Role, &interests, &p.Notes, &p.Lang, &updatedAt)
	if err == sql.ErrNoRows {
		return &UserProfile{UserID: userID}, nil
	}
	if err != nil {
		return nil, err
	}
	p.Interests = jsonUnmarshalStrings(interests)
	p.UpdatedAt = fromEpoch(updatedAt)
	return &p, nil
}

// --- User Memory ---

func (s *SQLiteDB) UpsertMemory(ctx context.Context, m *UserMemory) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_memories (id, user_id, memory, topics, token_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			memory=excluded.memory, topics=excluded.topics, token_count=excluded.token_count,
			updated_at=excluded.updated_at`,
		m.ID, m.UserID, m.Memory, jsonMarshal(m.Topics), m.TokenCount,
		toEpoch(m.CreatedAt), toEpoch(m.UpdatedAt))
	return err
}

func (s *SQLiteDB) ListMemories(ctx context.Context, userID int64, limit int) ([]*UserMemory, error) {
	q := "SELECT id, user_id, memory, topics, token_count, created_at, updated_at FROM user_memories WHERE user_id=? ORDER BY updated_at DESC"
	var args []interface{}
	args = append(args, userID)
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*UserMemory
	for rows.Next() {
		var m UserMemory
		var topics string
		var createdAt, updatedAt int64
		if err := rows.Scan(&m.ID, &m.UserID, &m.Memory, &topics, &m.TokenCount, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		m.Topics = jsonUnmarshalStrings(topics)
		m.CreatedAt = fromEpoch(createdAt)
		m.UpdatedAt = fromEpoch(updatedAt)
		result = append(result, &m)
	}
	return result, nil
}

func (s *SQLiteDB) DeleteMemory(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM user_memories WHERE id=?", id)
	return err
}

func (s *SQLiteDB) CountMemories(ctx context.Context, userID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_memories WHERE user_id=?", userID).Scan(&count)
	return count, err
}

func (s *SQLiteDB) TotalMemoryTokens(ctx context.Context, userID int64) (int, error) {
	var total int
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(token_count),0) FROM user_memories WHERE user_id=?", userID).Scan(&total)
	return total, err
}

func (s *SQLiteDB) ReplaceAllMemories(ctx context.Context, userID int64, memories []*UserMemory) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_memories WHERE user_id=?", userID); err != nil {
		return err
	}
	for _, m := range memories {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_memories (id, user_id, memory, topics, token_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			m.ID, userID, m.Memory, jsonMarshal(m.Topics), m.TokenCount,
			toEpoch(m.CreatedAt), toEpoch(m.UpdatedAt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- Conversation Summary ---

func (s *SQLiteDB) UpsertSummary(ctx context.Context, cs *ConversationSummary) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO conversation_summaries (conversation_id, user_id, summary, message_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(conversation_id) DO UPDATE SET
			summary=excluded.summary, message_count=excluded.message_count, updated_at=excluded.updated_at`,
		cs.ConversationID, cs.UserID, cs.Summary, cs.MessageCount,
		toEpoch(cs.CreatedAt), toEpoch(cs.UpdatedAt))
	return err
}

func (s *SQLiteDB) GetSummary(ctx context.Context, convID int64) (*ConversationSummary, error) {
	row := s.db.QueryRowContext(ctx, "SELECT conversation_id, user_id, summary, message_count, created_at, updated_at FROM conversation_summaries WHERE conversation_id=?", convID)
	var cs ConversationSummary
	var createdAt, updatedAt int64
	err := row.Scan(&cs.ConversationID, &cs.UserID, &cs.Summary, &cs.MessageCount, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cs.CreatedAt = fromEpoch(createdAt)
	cs.UpdatedAt = fromEpoch(updatedAt)
	return &cs, nil
}

func (s *SQLiteDB) ListSummaries(ctx context.Context, userID int64, limit int) ([]*ConversationSummary, error) {
	q := "SELECT conversation_id, user_id, summary, message_count, created_at, updated_at FROM conversation_summaries WHERE user_id=? ORDER BY updated_at DESC"
	var args []interface{}
	args = append(args, userID)
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*ConversationSummary
	for rows.Next() {
		var cs ConversationSummary
		var createdAt, updatedAt int64
		if err := rows.Scan(&cs.ConversationID, &cs.UserID, &cs.Summary, &cs.MessageCount, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		cs.CreatedAt = fromEpoch(createdAt)
		cs.UpdatedAt = fromEpoch(updatedAt)
		result = append(result, &cs)
	}
	return result, nil
}

func (s *SQLiteDB) DeleteOldestSummary(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM conversation_summaries WHERE conversation_id = (
			SELECT conversation_id FROM conversation_summaries WHERE user_id=? ORDER BY created_at ASC LIMIT 1
		)`, userID)
	return err
}

// --- Legacy Facts ---

func (s *SQLiteDB) ListFacts(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT fact FROM user_facts WHERE user_id=? ORDER BY id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var fact string
		if err := rows.Scan(&fact); err != nil {
			return nil, err
		}
		result = append(result, fact)
	}
	return result, nil
}

func (s *SQLiteDB) AddFact(ctx context.Context, userID int64, fact string) error {
	_, err := s.db.ExecContext(ctx, "INSERT OR IGNORE INTO user_facts (user_id, fact) VALUES (?, ?)", userID, fact)
	return err
}

// --- Cached Documents ---

func (s *SQLiteDB) UpsertDocument(ctx context.Context, doc *CachedDocument) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cached_documents (id, user_id, title, content, tags, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			user_id=excluded.user_id, title=excluded.title, content=excluded.content,
			tags=excluded.tags, updated_at=excluded.updated_at`,
		doc.ID, doc.UserID, doc.Title, doc.Content, jsonMarshal(doc.Tags), toEpoch(doc.UpdatedAt))
	return err
}

func (s *SQLiteDB) GetDocument(ctx context.Context, id int64) (*CachedDocument, error) {
	row := s.db.QueryRowContext(ctx, "SELECT id, user_id, title, content, tags, updated_at FROM cached_documents WHERE id=?", id)
	var doc CachedDocument
	var tags string
	var updatedAt int64
	err := row.Scan(&doc.ID, &doc.UserID, &doc.Title, &doc.Content, &tags, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	doc.Tags = jsonUnmarshalStrings(tags)
	doc.UpdatedAt = fromEpoch(updatedAt)
	return &doc, nil
}

func (s *SQLiteDB) ListDocuments(ctx context.Context, f DocumentFilter) ([]*CachedDocument, error) {
	q := "SELECT id, user_id, title, content, tags, updated_at FROM cached_documents WHERE 1=1"
	var args []interface{}
	if f.UserID != nil {
		q += " AND user_id=?"
		args = append(args, *f.UserID)
	}
	q += " ORDER BY updated_at DESC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*CachedDocument
	for rows.Next() {
		var doc CachedDocument
		var tags string
		var updatedAt int64
		if err := rows.Scan(&doc.ID, &doc.UserID, &doc.Title, &doc.Content, &tags, &updatedAt); err != nil {
			return nil, err
		}
		doc.Tags = jsonUnmarshalStrings(tags)
		doc.UpdatedAt = fromEpoch(updatedAt)
		result = append(result, &doc)
	}
	return result, nil
}

func (s *SQLiteDB) DeleteDocument(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM cached_documents WHERE id=?", id)
	return err
}

func (s *SQLiteDB) SearchDocuments(ctx context.Context, query string, limit int) ([]*CachedDocument, error) {
	keywords := strings.Fields(strings.ToLower(query))
	if len(keywords) == 0 {
		return s.ListDocuments(ctx, DocumentFilter{Limit: limit})
	}
	// Use SQL LIKE for keyword search, score in Go for simplicity
	allDocs, err := s.ListDocuments(ctx, DocumentFilter{})
	if err != nil {
		return nil, err
	}
	type scored struct {
		doc   *CachedDocument
		score int
	}
	var results []scored
	for _, d := range allDocs {
		score := 0
		titleLower := strings.ToLower(d.Title)
		contentLower := strings.ToLower(d.Content)
		for _, kw := range keywords {
			if strings.Contains(titleLower, kw) {
				score += 3
			}
			if strings.Contains(contentLower, kw) {
				score++
			}
			for _, tag := range d.Tags {
				if strings.Contains(strings.ToLower(tag), kw) {
					score += 2
				}
			}
		}
		if score > 0 {
			results = append(results, scored{doc: d, score: score})
		}
	}
	// Sort by score desc
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	docs := make([]*CachedDocument, 0, limit)
	for i, r := range results {
		if limit > 0 && i >= limit {
			break
		}
		docs = append(docs, r.doc)
	}
	return docs, nil
}

// --- Cached Tasks ---

func (s *SQLiteDB) UpsertTask(ctx context.Context, task *CachedTask) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cached_tasks (id, user_id, title, status, priority, notes, due_date, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			user_id=excluded.user_id, title=excluded.title, status=excluded.status,
			priority=excluded.priority, notes=excluded.notes, due_date=excluded.due_date,
			updated_at=excluded.updated_at`,
		task.ID, task.UserID, task.Title, task.Status, task.Priority, task.Notes, task.DueDate, toEpoch(task.UpdatedAt))
	return err
}

func (s *SQLiteDB) GetTask(ctx context.Context, id int64) (*CachedTask, error) {
	row := s.db.QueryRowContext(ctx, "SELECT id, user_id, title, status, priority, notes, due_date, updated_at FROM cached_tasks WHERE id=?", id)
	var task CachedTask
	var updatedAt int64
	err := row.Scan(&task.ID, &task.UserID, &task.Title, &task.Status, &task.Priority, &task.Notes, &task.DueDate, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	task.UpdatedAt = fromEpoch(updatedAt)
	return &task, nil
}

func (s *SQLiteDB) ListTasks(ctx context.Context, f TaskFilter) ([]*CachedTask, error) {
	q := "SELECT id, user_id, title, status, priority, notes, due_date, updated_at FROM cached_tasks WHERE 1=1"
	var args []interface{}
	if f.UserID != nil {
		q += " AND user_id=?"
		args = append(args, *f.UserID)
	}
	if f.Status != nil {
		q += " AND status=?"
		args = append(args, *f.Status)
	}
	if f.Priority != nil {
		q += " AND priority=?"
		args = append(args, *f.Priority)
	}
	q += " ORDER BY updated_at DESC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*CachedTask
	for rows.Next() {
		var task CachedTask
		var updatedAt int64
		if err := rows.Scan(&task.ID, &task.UserID, &task.Title, &task.Status, &task.Priority, &task.Notes, &task.DueDate, &updatedAt); err != nil {
			return nil, err
		}
		task.UpdatedAt = fromEpoch(updatedAt)
		result = append(result, &task)
	}
	return result, nil
}

func (s *SQLiteDB) DeleteTask(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM cached_tasks WHERE id=?", id)
	return err
}

// --- Trace Events ---

func (s *SQLiteDB) WriteTrace(ctx context.Context, event *TraceEvent) error {
	metadata := "{}"
	if event.Metadata != nil {
		metadata = jsonMarshal(event.Metadata)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO trace_events (event_id, session_id, span_id, parent_span, type, name, level, timestamp, duration_ms, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.EventID, event.SessionID, event.SpanID, event.ParentSpan,
		event.Type, event.Name, event.Level,
		toEpoch(event.Timestamp), event.DurationMs, metadata)
	return err
}

func (s *SQLiteDB) ListTraces(ctx context.Context, f TraceFilter) ([]*TraceEvent, error) {
	q := "SELECT event_id, session_id, span_id, parent_span, type, name, level, timestamp, duration_ms, metadata FROM trace_events WHERE 1=1"
	var args []interface{}
	if f.SessionID != nil {
		q += " AND session_id=?"
		args = append(args, *f.SessionID)
	}
	if f.EventType != nil {
		q += " AND type=?"
		args = append(args, *f.EventType)
	}
	if f.After != nil {
		q += " AND timestamp>=?"
		args = append(args, toEpoch(*f.After))
	}
	if f.Before != nil {
		q += " AND timestamp<=?"
		args = append(args, toEpoch(*f.Before))
	}
	q += " ORDER BY timestamp ASC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*TraceEvent
	for rows.Next() {
		var e TraceEvent
		var ts int64
		var meta string
		if err := rows.Scan(&e.EventID, &e.SessionID, &e.SpanID, &e.ParentSpan, &e.Type, &e.Name, &e.Level, &ts, &e.DurationMs, &meta); err != nil {
			return nil, err
		}
		e.Timestamp = fromEpoch(ts)
		e.Metadata = jsonUnmarshalMap(meta)
		result = append(result, &e)
	}
	return result, nil
}

// --- Chat Messages ---

func (s *SQLiteDB) InsertChatMessage(ctx context.Context, msg *ChatMessage) error {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO chat_messages (conversation_id, user_id, session_id, role, content, model, token_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ConversationID, msg.UserID, msg.SessionID, msg.Role, msg.Content, msg.Model, msg.TokenCount, toEpoch(msg.CreatedAt))
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	msg.ID = id
	return nil
}

func (s *SQLiteDB) ListChatMessages(ctx context.Context, convID int64, limit int) ([]*ChatMessage, error) {
	q := "SELECT id, conversation_id, user_id, session_id, role, content, model, token_count, created_at FROM chat_messages WHERE conversation_id=? ORDER BY created_at ASC"
	var args []interface{}
	args = append(args, convID)
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*ChatMessage
	for rows.Next() {
		var msg ChatMessage
		var createdAt int64
		if err := rows.Scan(&msg.ID, &msg.ConversationID, &msg.UserID, &msg.SessionID, &msg.Role, &msg.Content, &msg.Model, &msg.TokenCount, &createdAt); err != nil {
			return nil, err
		}
		msg.CreatedAt = fromEpoch(createdAt)
		result = append(result, &msg)
	}
	return result, nil
}

// --- Lifecycle ---

func (s *SQLiteDB) Close() error {
	return s.db.Close()
}
