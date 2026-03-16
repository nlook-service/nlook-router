package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
