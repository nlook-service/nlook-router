package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nlook-service/nlook-router/internal/apiclient"
	"github.com/nlook-service/nlook-router/internal/tools"
)

func TestHealthHandler(t *testing.T) {
	s := New(":0", &Status{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.healthHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status want 200 got %d", rec.Code)
	}
	var out map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["status"] != "ok" {
		t.Errorf("status want ok got %s", out["status"])
	}
}

func TestStatusHandler(t *testing.T) {
	status := &Status{RouterID: "r1", Connected: true}
	s := New(":0", status)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	s.statusHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status want 200 got %d", rec.Code)
	}
	var out Status
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.RouterID != status.RouterID || out.Connected != status.Connected {
		t.Errorf("status want %+v got %+v", status, out)
	}
}

func TestToolsHandler_NotConfigured(t *testing.T) {
	s := New(":0", &Status{})
	req := httptest.NewRequest(http.MethodGet, "/tools", nil)
	rec := httptest.NewRecorder()
	s.toolsHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status want 503 got %d", rec.Code)
	}
}

func TestToolsHandler_ReturnsList(t *testing.T) {
	s := New(":0", &Status{})
	s.SetToolsLister(&tools.StaticLister{
		Tools: []apiclient.ToolMeta{
			{Name: "add", Description: "Add two numbers"},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/tools", nil)
	rec := httptest.NewRecorder()
	s.toolsHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status want 200 got %d body %s", rec.Code, rec.Body.String())
	}
	var list []apiclient.ToolMeta
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "add" {
		t.Errorf("want one tool 'add' got %v", list)
	}
}

func TestToolsHandler_MethodNotAllowed(t *testing.T) {
	s := New(":0", &Status{})
	s.SetToolsLister(&tools.StaticLister{Tools: []apiclient.ToolMeta{}})
	req := httptest.NewRequest(http.MethodPost, "/tools", nil)
	rec := httptest.NewRecorder()
	s.toolsHandler(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status want 405 got %d", rec.Code)
	}
}
