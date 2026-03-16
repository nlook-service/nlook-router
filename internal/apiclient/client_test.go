package apiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_ListWorkflows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/router/v1/workflows" || r.Method != http.MethodGet {
			t.Errorf("unexpected path or method: %s %s", r.Method, r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("want Authorization Bearer test-key got %s", auth)
		}
		_ = json.NewEncoder(w).Encode([]Workflow{
			{ID: 1, Title: "w1"},
			{ID: 2, Title: "w2"},
		})
	}))
	defer server.Close()

	client := New(server.URL, "test-key")
	ctx := context.Background()
	list, err := client.ListWorkflows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 workflows got %d", len(list))
	}
	if list[0].ID != 1 || list[0].Title != "w1" {
		t.Errorf("first workflow want id=1 title=w1 got %+v", list[0])
	}
}

func TestClient_GetWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/router/v1/workflows/42" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(Workflow{ID: 42, Title: "single"})
	}))
	defer server.Close()

	client := New(server.URL, "")
	ctx := context.Background()
	w, err := client.GetWorkflow(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	if w.ID != 42 || w.Title != "single" {
		t.Errorf("want id=42 title=single got %+v", w)
	}
}

func TestClient_RegisterRouter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/routers/register" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL, "key")
	ctx := context.Background()
	err := client.RegisterRouter(ctx, &RegisterPayload{RouterID: "r1", Version: "0.1"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestClient_Heartbeat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/routers/heartbeat" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()

	client := New(server.URL, "")
	ctx := context.Background()
	err := client.Heartbeat(ctx, &RegisterPayload{RouterID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestClient_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer server.Close()

	client := New(server.URL, "")
	ctx := context.Background()
	_, err := client.ListWorkflows(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError got %T", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("StatusCode want 404 got %d", apiErr.StatusCode)
	}
	if string(apiErr.Body) != "not found" {
		t.Errorf("Body want 'not found' got %s", apiErr.Body)
	}
}
