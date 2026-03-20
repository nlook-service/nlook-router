package db

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// MigrateFileToSQLite imports existing JSON files into an SQLite database.
// JSON files are backed up with .bak suffix (not deleted).
func MigrateFileToSQLite(dataDir string) error {
	sqlitePath := filepath.Join(dataDir, "nlook.db")
	sdb, err := newSQLiteDB(sqlitePath)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	defer sdb.Close()

	ctx := context.Background()
	var totalCount int

	// 1. memory.json
	if n, err := migrateMemory(ctx, sdb, filepath.Join(dataDir, "memory.json")); err != nil {
		log.Printf("migrate: memory.json: %v", err)
	} else {
		totalCount += n
	}

	// 2. cache.json
	if n, err := migrateCache(ctx, sdb, filepath.Join(dataDir, "cache.json")); err != nil {
		log.Printf("migrate: cache.json: %v", err)
	} else {
		totalCount += n
	}

	// 3. sessions.json
	if n, err := migrateSessions(ctx, sdb, filepath.Join(dataDir, "sessions.json")); err != nil {
		log.Printf("migrate: sessions.json: %v", err)
	} else {
		totalCount += n
	}

	log.Printf("migrate: completed — %d records imported to %s", totalCount, sqlitePath)
	return nil
}

func migrateMemory(ctx context.Context, sdb *SQLiteDB, path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var mf struct {
		Profile   UserProfile                     `json:"profile"`
		Summaries map[int64]*ConversationSummary  `json:"summaries"`
		Facts     []string                        `json:"facts"`
		Memories  []UserMemory                    `json:"memories"`
	}
	if err := json.Unmarshal(data, &mf); err != nil {
		return 0, fmt.Errorf("parse memory.json: %w", err)
	}

	count := 0

	// Profile
	if mf.Profile.Role != "" || len(mf.Profile.Interests) > 0 {
		if err := sdb.UpsertUserProfile(ctx, &mf.Profile); err != nil {
			return count, fmt.Errorf("profile: %w", err)
		}
		count++
	}

	// Memories
	for _, m := range mf.Memories {
		if err := sdb.UpsertMemory(ctx, &m); err != nil {
			return count, fmt.Errorf("memory %s: %w", m.ID, err)
		}
		count++
	}

	// Summaries
	for _, s := range mf.Summaries {
		if err := sdb.UpsertSummary(ctx, s); err != nil {
			return count, fmt.Errorf("summary %d: %w", s.ConversationID, err)
		}
		count++
	}

	// Facts
	for _, f := range mf.Facts {
		if err := sdb.AddFact(ctx, 0, f); err != nil {
			return count, fmt.Errorf("fact: %w", err)
		}
		count++
	}

	backup(path)
	log.Printf("migrate: memory.json — %d records", count)
	return count, nil
}

func migrateCache(ctx context.Context, sdb *SQLiteDB, path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var cf struct {
		Documents map[int64]*CachedDocument `json:"documents"`
		Tasks     map[int64]*CachedTask     `json:"tasks"`
	}
	if err := json.Unmarshal(data, &cf); err != nil {
		return 0, fmt.Errorf("parse cache.json: %w", err)
	}

	count := 0
	for _, d := range cf.Documents {
		if err := sdb.UpsertDocument(ctx, d); err != nil {
			return count, fmt.Errorf("doc %d: %w", d.ID, err)
		}
		count++
	}
	for _, t := range cf.Tasks {
		if err := sdb.UpsertTask(ctx, t); err != nil {
			return count, fmt.Errorf("task %d: %w", t.ID, err)
		}
		count++
	}

	backup(path)
	log.Printf("migrate: cache.json — %d records", count)
	return count, nil
}

func migrateSessions(ctx context.Context, sdb *SQLiteDB, path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var sessions map[string]*Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		return 0, fmt.Errorf("parse sessions.json: %w", err)
	}

	count := 0
	now := time.Now()
	for _, s := range sessions {
		// Skip expired sessions
		if s.ExpiresAt.Before(now) {
			continue
		}
		if err := sdb.UpsertSession(ctx, s); err != nil {
			return count, fmt.Errorf("session %s: %w", s.ID, err)
		}
		count++
	}

	backup(path)
	log.Printf("migrate: sessions.json — %d records", count)
	return count, nil
}

func backup(path string) {
	bakPath := path + ".bak"
	if _, err := os.Stat(bakPath); err == nil {
		return // backup already exists
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	os.WriteFile(bakPath, data, 0644)
}
