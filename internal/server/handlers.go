package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/nlook-service/nlook-router/internal/ollama"
)

func (s *Server) healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) statusHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(s.status)
}

func (s *Server) toolsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"error":"method not allowed"}`))
		return
	}
	if s.toolsLister == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"tools not configured"}`))
		return
	}
	list, err := s.toolsLister.ListTools(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(list)
}

func (s *Server) modelStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	engineType := "ollama"
	if s.llmEngine != nil {
		engineType = string(s.llmEngine.Type())
	}

	client := ollama.NewClient()
	if !client.IsRunning(r.Context()) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"model": "", "engine": engineType, "running": false,
		})
		return
	}

	// Find the active model
	modelName := ""
	if s.llmEngine != nil && s.llmEngine.Model() != "" {
		modelName = s.llmEngine.Model()
	} else {
		models, err := client.List(r.Context())
		if err == nil {
			for _, m := range models {
				if name := m.Name; name != "" {
					modelName = name
					break
				}
			}
		}
	}

	if modelName == "" {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"model": "", "engine": engineType, "running": true,
		})
		return
	}

	detail, err := client.Show(r.Context(), modelName)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"model": modelName, "engine": engineType, "running": true,
		})
		return
	}

	sizeStr := fmt.Sprintf("%.1f GB", float64(detail.Size)/(1024*1024*1024))

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"model":          detail.Name,
		"engine":         engineType,
		"size":           sizeStr,
		"parameter_size": detail.ParameterSize,
		"format":         detail.Format,
		"quantization":   detail.QuantizationLevel,
		"family":         detail.Family,
		"running":        true,
	})
}

// sessionsHandler handles GET /sessions — list active sessions.
func (s *Server) sessionsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.sessions == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"sessions not configured"}`))
		return
	}

	type sessionEntry struct {
		ID        string   `json:"id"`
		Type      string   `json:"type"`
		State     string   `json:"state"`
		UserID    int64    `json:"user_id"`
		AgentIDs  []string `json:"agent_ids,omitempty"`
		RunIDs    []int64  `json:"run_ids,omitempty"`
		CreatedAt string   `json:"created_at"`
		UpdatedAt string   `json:"updated_at"`
		ExpiresAt string   `json:"expires_at"`
	}

	sessions := s.sessions.List()
	entries := make([]sessionEntry, 0, len(sessions))
	for _, sess := range sessions {
		entries = append(entries, sessionEntry{
			ID:        sess.ID,
			Type:      string(sess.Type),
			State:     string(sess.State),
			UserID:    sess.UserID,
			AgentIDs:  sess.AgentIDs,
			RunIDs:    sess.RunIDs,
			CreatedAt: sess.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt: sess.UpdatedAt.Format("2006-01-02T15:04:05Z"),
			ExpiresAt: sess.ExpiresAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"sessions": entries})
}

// sessionDetailHandler handles GET /sessions/{id} and GET /sessions/{id}/traces.
func (s *Server) sessionDetailHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Parse path: /sessions/{id} or /sessions/{id}/traces
	path := strings.TrimPrefix(r.URL.Path, "/sessions/")
	parts := strings.SplitN(path, "/", 2)
	sessionID := parts[0]

	if sessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"session_id required"}`))
		return
	}

	// /sessions/{id}/traces
	if len(parts) == 2 && parts[1] == "traces" {
		s.handleSessionTraces(w, sessionID)
		return
	}

	// /sessions/{id}
	if s.sessions == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	sess := s.sessions.Get(sessionID)
	if sess == nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"session not found"}`))
		return
	}
	_ = json.NewEncoder(w).Encode(sess)
}

func (s *Server) handleSessionTraces(w http.ResponseWriter, sessionID string) {
	if s.traceWriter == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"tracing not configured"}`))
		return
	}

	events, err := s.traceWriter.ReadEvents(sessionID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id": sessionID,
		"events":     events,
		"count":      len(events),
	})
}
