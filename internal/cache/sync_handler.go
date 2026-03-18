package cache

import (
	"encoding/json"
	"log"
	"time"
)

// SyncHandler processes sync WebSocket messages and updates the cache store.
type SyncHandler struct {
	store *Store
}

// NewSyncHandler creates a sync handler.
func NewSyncHandler(store *Store) *SyncHandler {
	return &SyncHandler{store: store}
}

// HandleMessage processes sync messages. Returns true if handled.
func (h *SyncHandler) HandleMessage(msgType string, payload json.RawMessage) bool {
	switch msgType {
	case "sync:document":
		h.handleDocumentSync(payload)
		return true
	case "sync:document:delete":
		h.handleDocumentDelete(payload)
		return true
	case "sync:task":
		h.handleTaskSync(payload)
		return true
	case "sync:task:delete":
		h.handleTaskDelete(payload)
		return true
	case "sync:bulk":
		h.handleBulkSync(payload)
		return true
	default:
		return false
	}
}

func (h *SyncHandler) handleDocumentSync(payload json.RawMessage) {
	var doc struct {
		ID        int64    `json:"id"`
		Title     string   `json:"title"`
		Content   string   `json:"content"`
		Tags      []string `json:"tags"`
		UpdatedAt string   `json:"updated_at"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		log.Printf("sync:document unmarshal error: %v", err)
		return
	}

	updatedAt, _ := time.Parse(time.RFC3339, doc.UpdatedAt)
	h.store.SetDocument(&Document{
		ID:        doc.ID,
		Title:     doc.Title,
		Content:   doc.Content,
		Tags:      doc.Tags,
		UpdatedAt: updatedAt,
	})
	log.Printf("sync: cached document id=%d title=%s", doc.ID, doc.Title)
}

func (h *SyncHandler) handleDocumentDelete(payload json.RawMessage) {
	var msg struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return
	}
	h.store.RemoveDocument(msg.ID)
	log.Printf("sync: removed document id=%d", msg.ID)
}

func (h *SyncHandler) handleTaskSync(payload json.RawMessage) {
	var task struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		Priority  string `json:"priority"`
		Notes     string `json:"notes"`
		DueDate   string `json:"due_date"`
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.Unmarshal(payload, &task); err != nil {
		log.Printf("sync:task unmarshal error: %v", err)
		return
	}

	updatedAt, _ := time.Parse(time.RFC3339, task.UpdatedAt)
	h.store.SetTask(&Task{
		ID:        task.ID,
		Title:     task.Title,
		Status:    task.Status,
		Priority:  task.Priority,
		Notes:     task.Notes,
		DueDate:   task.DueDate,
		UpdatedAt: updatedAt,
	})
	log.Printf("sync: cached task id=%d title=%s status=%s", task.ID, task.Title, task.Status)
}

func (h *SyncHandler) handleTaskDelete(payload json.RawMessage) {
	var msg struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return
	}
	h.store.RemoveTask(msg.ID)
	log.Printf("sync: removed task id=%d", msg.ID)
}

// BulkSyncPayload is sent on initial connection for full sync.
type BulkSyncPayload struct {
	Documents []struct {
		ID        int64    `json:"id"`
		Title     string   `json:"title"`
		Content   string   `json:"content"`
		Tags      []string `json:"tags"`
		UpdatedAt string   `json:"updated_at"`
	} `json:"documents"`
	Tasks []struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		Priority  string `json:"priority"`
		Notes     string `json:"notes"`
		DueDate   string `json:"due_date"`
		UpdatedAt string `json:"updated_at"`
	} `json:"tasks"`
}

func (h *SyncHandler) handleBulkSync(payload json.RawMessage) {
	var bulk BulkSyncPayload
	if err := json.Unmarshal(payload, &bulk); err != nil {
		log.Printf("sync:bulk unmarshal error: %v", err)
		return
	}

	for _, d := range bulk.Documents {
		updatedAt, _ := time.Parse(time.RFC3339, d.UpdatedAt)
		h.store.SetDocument(&Document{
			ID: d.ID, Title: d.Title, Content: d.Content,
			Tags: d.Tags, UpdatedAt: updatedAt,
		})
	}
	for _, t := range bulk.Tasks {
		updatedAt, _ := time.Parse(time.RFC3339, t.UpdatedAt)
		h.store.SetTask(&Task{
			ID: t.ID, Title: t.Title, Status: t.Status,
			Priority: t.Priority, Notes: t.Notes, DueDate: t.DueDate,
			UpdatedAt: updatedAt,
		})
	}
	log.Printf("sync: bulk loaded %d documents, %d tasks", len(bulk.Documents), len(bulk.Tasks))
}
