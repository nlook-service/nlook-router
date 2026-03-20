package db

import (
	"context"
	"time"
)

// DB is the unified storage interface for the router.
// Each method group corresponds to an existing Store package.
type DB interface {
	// --- Session ---
	UpsertSession(ctx context.Context, s *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	ListSessions(ctx context.Context, f SessionFilter) ([]*Session, error)
	DeleteSession(ctx context.Context, id string) error
	DeleteExpiredSessions(ctx context.Context, before time.Time) (int, error)

	// --- User Profile ---
	UpsertUserProfile(ctx context.Context, p *UserProfile) error
	GetUserProfile(ctx context.Context, userID int64) (*UserProfile, error)

	// --- User Memory ---
	UpsertMemory(ctx context.Context, m *UserMemory) error
	ListMemories(ctx context.Context, userID int64, limit int) ([]*UserMemory, error)
	DeleteMemory(ctx context.Context, id string) error
	CountMemories(ctx context.Context, userID int64) (int, error)
	TotalMemoryTokens(ctx context.Context, userID int64) (int, error)
	ReplaceAllMemories(ctx context.Context, userID int64, memories []*UserMemory) error

	// --- Conversation Summary ---
	UpsertSummary(ctx context.Context, s *ConversationSummary) error
	GetSummary(ctx context.Context, convID int64) (*ConversationSummary, error)
	ListSummaries(ctx context.Context, userID int64, limit int) ([]*ConversationSummary, error)
	DeleteOldestSummary(ctx context.Context, userID int64) error

	// --- Legacy Facts ---
	ListFacts(ctx context.Context, userID int64) ([]string, error)
	AddFact(ctx context.Context, userID int64, fact string) error

	// --- Cached Documents (synced from Cloud) ---
	UpsertDocument(ctx context.Context, doc *CachedDocument) error
	GetDocument(ctx context.Context, id int64) (*CachedDocument, error)
	ListDocuments(ctx context.Context, f DocumentFilter) ([]*CachedDocument, error)
	DeleteDocument(ctx context.Context, id int64) error
	SearchDocuments(ctx context.Context, query string, limit int) ([]*CachedDocument, error)

	// --- Cached Tasks (synced from Cloud) ---
	UpsertTask(ctx context.Context, task *CachedTask) error
	GetTask(ctx context.Context, id int64) (*CachedTask, error)
	ListTasks(ctx context.Context, f TaskFilter) ([]*CachedTask, error)
	DeleteTask(ctx context.Context, id int64) error

	// --- Trace Events ---
	WriteTrace(ctx context.Context, event *TraceEvent) error
	ListTraces(ctx context.Context, f TraceFilter) ([]*TraceEvent, error)

	// --- Chat Messages (local AI conversation history) ---
	InsertChatMessage(ctx context.Context, msg *ChatMessage) error
	ListChatMessages(ctx context.Context, convID int64, limit int) ([]*ChatMessage, error)

	// --- Lifecycle ---
	Migrate(ctx context.Context) error
	Close() error
}
