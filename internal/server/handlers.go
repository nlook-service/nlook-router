package server

import (
	"encoding/json"
	"fmt"
	"net/http"

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
