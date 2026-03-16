package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nlook-service/nlook-router/internal/apiclient"
)

// TestStaticLister_ListTools verifies that StaticLister returns the configured tools.
func TestStaticLister_ListTools(t *testing.T) {
	ctx := context.Background()
	lister := &StaticLister{
		Tools: []apiclient.ToolMeta{
			{Name: "add", Description: "Add two numbers", Parameters: map[string]interface{}{"type": "object"}},
			{Name: "subtract", Description: "Subtract two numbers"},
		},
	}
	list, err := lister.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 tools got %d", len(list))
	}
	if list[0].Name != "add" || list[0].Description != "Add two numbers" {
		t.Errorf("first tool want name=add got %+v", list[0])
	}
	if list[1].Name != "subtract" {
		t.Errorf("second tool want name=subtract got %s", list[1].Name)
	}
}

// TestStaticLister_ListTools_Nil returns nil when Tools is nil.
func TestStaticLister_ListTools_Nil(t *testing.T) {
	ctx := context.Background()
	lister := &StaticLister{}
	list, err := lister.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if list != nil {
		t.Errorf("want nil list when Tools not set got %v", list)
	}
}

// TestToolsUsage_RegisterPayloadWithLister demonstrates how to use a Lister
// to populate RegisterPayload.Tools for sending available tools to the server.
// This is the intended usage in run_daemon or heartbeat.
func TestToolsUsage_RegisterPayloadWithLister(t *testing.T) {
	ctx := context.Background()
	lister := &StaticLister{
		Tools: []apiclient.ToolMeta{
			{Name: "calculator_add", Description: "Add two numbers"},
			{Name: "calculator_multiply", Description: "Multiply two numbers"},
		},
	}

	tools, err := lister.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}

	payload := &apiclient.RegisterPayload{
		RouterID: "test-router",
		Version:  "0.1.0",
		Tools:    tools,
	}

	// Serialize and assert server would receive tools
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		RouterID string                   `json:"router_id"`
		Version  string                   `json:"version"`
		Tools    []apiclient.ToolMeta     `json:"tools"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RouterID != "test-router" || len(decoded.Tools) != 2 {
		t.Errorf("decoded payload: router_id=%s tools count=%d", decoded.RouterID, len(decoded.Tools))
	}
	if decoded.Tools[0].Name != "calculator_add" {
		t.Errorf("first tool name want calculator_add got %s", decoded.Tools[0].Name)
	}
}
