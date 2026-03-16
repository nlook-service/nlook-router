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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"workflows": []Workflow{
				{ID: 1, Title: "w1"},
				{ID: 2, Title: "w2"},
			},
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

// TestClient_RegisterRouter_WithTools verifies that when Tools are set on RegisterPayload,
// they are sent to the server in the request body. This demonstrates how the router
// sends available tools to the server on register.
func TestClient_RegisterRouter_WithTools(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/routers/register" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(server.URL, "key")
	ctx := context.Background()
	payload := &RegisterPayload{
		RouterID: "r1",
		Version:  "0.1",
		Tools: []ToolMeta{
			{Name: "add", Description: "Add two numbers", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "number"}, "b": map[string]interface{}{"type": "number"}}}},
			{Name: "subtract", Description: "Subtract two numbers"},
		},
	}
	err := client.RegisterRouter(ctx, payload)
	if err != nil {
		t.Fatal(err)
	}
	if receivedBody == nil {
		t.Fatal("server did not receive body")
	}
	if receivedBody["router_id"] != "r1" || receivedBody["version"] != "0.1" {
		t.Errorf("want router_id=r1 version=0.1 got %v", receivedBody)
	}
	tools, ok := receivedBody["tools"].([]interface{})
	if !ok || len(tools) != 2 {
		t.Fatalf("want tools array of length 2 got %T %v", receivedBody["tools"], receivedBody["tools"])
	}
	first := tools[0].(map[string]interface{})
	if first["name"] != "add" || first["description"] != "Add two numbers" {
		t.Errorf("first tool want name=add description=Add two numbers got %v", first)
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

// TestRegisterPayload_WithTools_Serializes verifies that RegisterPayload with Tools
// serializes to JSON so the server receives the tools field.
func TestRegisterPayload_WithTools_Serializes(t *testing.T) {
	payload := &RegisterPayload{
		RouterID: "router-1",
		Version:  "1.0",
		Tools: []ToolMeta{
			{Name: "calculator_add", Description: "Add two numbers", Parameters: map[string]interface{}{"type": "object"}},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["router_id"] != "router-1" || decoded["version"] != "1.0" {
		t.Errorf("want router_id and version in JSON got %v", decoded)
	}
	tools, ok := decoded["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("want tools array with one element got %v", decoded["tools"])
	}
	t0 := tools[0].(map[string]interface{})
	if t0["name"] != "calculator_add" || t0["description"] != "Add two numbers" {
		t.Errorf("tool entry want name=calculator_add got %v", t0)
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
