package session

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/nlook-service/nlook-router/internal/db"
)

// NewStoreWithDB creates a session store backed by the unified DB layer.
func NewStoreWithDB(storage db.DB, ttl time.Duration) *Store {
	if ttl == 0 {
		ttl = DefaultTTL
	}
	s := &Store{
		sessions: make(map[string]*Session),
		ttl:      ttl,
		stopGC:   make(chan struct{}),
		db:       storage,
	}
	s.loadFromDB()
	go s.gcLoop()
	return s
}

func (s *Store) loadFromDB() {
	if s.db == nil {
		return
	}
	ctx := context.Background()
	active := "active"
	sessions, err := s.db.ListSessions(ctx, db.SessionFilter{State: &active})
	if err != nil {
		log.Printf("session/db: load error: %v", err)
		return
	}
	for _, ds := range sessions {
		sess := dbSessionToLocal(ds)
		s.sessions[sess.ID] = sess
	}
	log.Printf("session/db: loaded %d active sessions", len(s.sessions))
}

func (s *Store) syncSessionToDB(sess *Session) {
	if s.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.db.UpsertSession(ctx, localSessionToDB(sess))
	}()
}

func (s *Store) syncDeleteSessionFromDB(id string) {
	if s.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.db.DeleteSession(ctx, id)
	}()
}

func (s *Store) syncDeleteExpiredFromDB() {
	if s.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.db.DeleteExpiredSessions(ctx, time.Now())
	}()
}

func localSessionToDB(sess *Session) *db.Session {
	ctxData, _ := json.Marshal(sess.Context)
	return &db.Session{
		ID:        sess.ID,
		Type:      string(sess.Type),
		State:     string(sess.State),
		UserID:    sess.UserID,
		AgentIDs:  sess.AgentIDs,
		RunIDs:    sess.RunIDs,
		Context:   ctxData,
		CreatedAt: sess.CreatedAt,
		UpdatedAt: sess.UpdatedAt,
		ExpiresAt: sess.ExpiresAt,
	}
}

func dbSessionToLocal(ds *db.Session) *Session {
	sess := &Session{
		ID:        ds.ID,
		Type:      SessionType(ds.Type),
		State:     SessionState(ds.State),
		UserID:    ds.UserID,
		AgentIDs:  ds.AgentIDs,
		RunIDs:    ds.RunIDs,
		CreatedAt: ds.CreatedAt,
		UpdatedAt: ds.UpdatedAt,
		ExpiresAt: ds.ExpiresAt,
	}
	if len(ds.Context) > 0 {
		var ctx Context
		if err := json.Unmarshal(ds.Context, &ctx); err == nil {
			sess.Context = &ctx
		}
	}
	if sess.Context == nil {
		sess.Context = NewContext()
	}
	return sess
}
