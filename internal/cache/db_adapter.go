package cache

import (
	"context"
	"log"
	"time"

	"github.com/nlook-service/nlook-router/internal/db"
)

// NewStoreWithDB creates a cache store backed by the unified DB layer.
// The returned Store has the same public API as NewStore().
func NewStoreWithDB(storage db.DB) *Store {
	s := &Store{
		documents: make(map[int64]*Document),
		tasks:     make(map[int64]*Task),
		db:        storage,
	}
	s.loadFromDB()
	return s
}

func (s *Store) loadFromDB() {
	if s.db == nil {
		return
	}
	ctx := context.Background()

	docs, err := s.db.ListDocuments(ctx, db.DocumentFilter{})
	if err != nil {
		log.Printf("cache/db: load documents error: %v", err)
	} else {
		for _, d := range docs {
			s.documents[d.ID] = &Document{
				ID:        d.ID,
				Title:     d.Title,
				Content:   d.Content,
				Tags:      d.Tags,
				UpdatedAt: d.UpdatedAt,
			}
		}
	}

	tasks, err := s.db.ListTasks(ctx, db.TaskFilter{})
	if err != nil {
		log.Printf("cache/db: load tasks error: %v", err)
	} else {
		for _, t := range tasks {
			s.tasks[t.ID] = &Task{
				ID:        t.ID,
				Title:     t.Title,
				Status:    t.Status,
				Priority:  t.Priority,
				Notes:     t.Notes,
				DueDate:   t.DueDate,
				UpdatedAt: t.UpdatedAt,
			}
		}
	}

	log.Printf("cache/db: loaded %d documents, %d tasks", len(s.documents), len(s.tasks))
}

func (s *Store) syncDocumentToDB(doc *Document) {
	if s.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.db.UpsertDocument(ctx, &db.CachedDocument{
			ID:        doc.ID,
			Title:     doc.Title,
			Content:   doc.Content,
			Tags:      doc.Tags,
			UpdatedAt: doc.UpdatedAt,
		})
	}()
}

func (s *Store) syncDeleteDocumentFromDB(id int64) {
	if s.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.db.DeleteDocument(ctx, id)
	}()
}

func (s *Store) syncTaskToDB(task *Task) {
	if s.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.db.UpsertTask(ctx, &db.CachedTask{
			ID:        task.ID,
			Title:     task.Title,
			Status:    task.Status,
			Priority:  task.Priority,
			Notes:     task.Notes,
			DueDate:   task.DueDate,
			UpdatedAt: task.UpdatedAt,
		})
	}()
}

func (s *Store) syncDeleteTaskFromDB(id int64) {
	if s.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.db.DeleteTask(ctx, id)
	}()
}
