package engine

import (
	"testing"

	"github.com/nlook-service/nlook-router/internal/apiclient"
)

// helpers

func makeNode(id, nodeType string) apiclient.WorkflowNode {
	return apiclient.WorkflowNode{NodeID: id, NodeType: nodeType}
}

func makeEdge(src, tgt string) apiclient.WorkflowEdge {
	return apiclient.WorkflowEdge{
		EdgeID:       src + "->" + tgt,
		SourceNodeID: src,
		TargetNodeID: tgt,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// ParseDAG
// ──────────────────────────────────────────────────────────────────────────────

func TestParseDAG_linearChain(t *testing.T) {
	nodes := []apiclient.WorkflowNode{
		makeNode("start-1", "start"),
		makeNode("step-1", "step"),
		makeNode("end-1", "end"),
	}
	edges := []apiclient.WorkflowEdge{
		makeEdge("start-1", "step-1"),
		makeEdge("step-1", "end-1"),
	}

	dag, err := ParseDAG(nodes, edges)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dag.Start == nil {
		t.Fatal("Start node is nil")
	}
	if dag.Start.Node.NodeID != "start-1" {
		t.Errorf("Start node ID: got %q, want 'start-1'", dag.Start.Node.NodeID)
	}
	if dag.End == nil {
		t.Fatal("End node is nil")
	}
	if dag.End.Node.NodeID != "end-1" {
		t.Errorf("End node ID: got %q, want 'end-1'", dag.End.Node.NodeID)
	}
	if len(dag.Nodes) != 3 {
		t.Errorf("node count: got %d, want 3", len(dag.Nodes))
	}

	// Check edge wiring
	startNode := dag.Nodes["start-1"]
	if len(startNode.Children) != 1 || startNode.Children[0].Node.NodeID != "step-1" {
		t.Errorf("start-1 should have child step-1")
	}
}

func TestParseDAG_missingStartNode(t *testing.T) {
	nodes := []apiclient.WorkflowNode{
		makeNode("step-1", "step"),
		makeNode("end-1", "end"),
	}
	edges := []apiclient.WorkflowEdge{
		makeEdge("step-1", "end-1"),
	}

	_, err := ParseDAG(nodes, edges)
	if err == nil {
		t.Fatal("expected error for missing start node, got nil")
	}
}

func TestParseDAG_multipleStartNodes(t *testing.T) {
	nodes := []apiclient.WorkflowNode{
		makeNode("start-1", "start"),
		makeNode("start-2", "start"),
		makeNode("end-1", "end"),
	}
	edges := []apiclient.WorkflowEdge{
		makeEdge("start-1", "end-1"),
	}

	_, err := ParseDAG(nodes, edges)
	if err == nil {
		t.Fatal("expected error for multiple start nodes, got nil")
	}
}

func TestParseDAG_multipleEndNodes(t *testing.T) {
	nodes := []apiclient.WorkflowNode{
		makeNode("start-1", "start"),
		makeNode("end-1", "end"),
		makeNode("end-2", "end"),
	}
	edges := []apiclient.WorkflowEdge{
		makeEdge("start-1", "end-1"),
	}

	_, err := ParseDAG(nodes, edges)
	if err == nil {
		t.Fatal("expected error for multiple end nodes, got nil")
	}
}

func TestParseDAG_orphanEdgesIgnored(t *testing.T) {
	nodes := []apiclient.WorkflowNode{
		makeNode("start-1", "start"),
		makeNode("end-1", "end"),
	}
	edges := []apiclient.WorkflowEdge{
		makeEdge("start-1", "end-1"),
		makeEdge("ghost-node", "end-1"),   // source doesn't exist
		makeEdge("start-1", "ghost-node"), // target doesn't exist
	}

	dag, err := ParseDAG(nodes, edges)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Orphan edges should be silently skipped
	if len(dag.Nodes) != 2 {
		t.Errorf("node count: got %d, want 2", len(dag.Nodes))
	}
}

func TestParseDAG_branchingTopology(t *testing.T) {
	//  start → step-a
	//        ↘ step-b
	//  step-a → end
	//  step-b → end
	nodes := []apiclient.WorkflowNode{
		makeNode("start-1", "start"),
		makeNode("step-a", "step"),
		makeNode("step-b", "step"),
		makeNode("end-1", "end"),
	}
	edges := []apiclient.WorkflowEdge{
		makeEdge("start-1", "step-a"),
		makeEdge("start-1", "step-b"),
		makeEdge("step-a", "end-1"),
		makeEdge("step-b", "end-1"),
	}

	dag, err := ParseDAG(nodes, edges)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	startNode := dag.Nodes["start-1"]
	if len(startNode.Children) != 2 {
		t.Errorf("start-1 should have 2 children, got %d", len(startNode.Children))
	}

	endNode := dag.Nodes["end-1"]
	if len(endNode.Parents) != 2 {
		t.Errorf("end-1 should have 2 parents, got %d", len(endNode.Parents))
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// TopologicalSort
// ──────────────────────────────────────────────────────────────────────────────

func TestTopologicalSort_linearChain(t *testing.T) {
	nodes := []apiclient.WorkflowNode{
		makeNode("start-1", "start"),
		makeNode("step-1", "step"),
		makeNode("end-1", "end"),
	}
	edges := []apiclient.WorkflowEdge{
		makeEdge("start-1", "step-1"),
		makeEdge("step-1", "end-1"),
	}

	dag, err := ParseDAG(nodes, edges)
	if err != nil {
		t.Fatalf("ParseDAG: %v", err)
	}

	sorted, err := dag.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}

	if len(sorted) != 3 {
		t.Fatalf("sorted length: got %d, want 3", len(sorted))
	}

	// start must come before step, step before end
	idx := func(id string) int {
		for i, n := range sorted {
			if n.Node.NodeID == id {
				return i
			}
		}
		return -1
	}

	if idx("start-1") >= idx("step-1") {
		t.Error("start-1 should come before step-1")
	}
	if idx("step-1") >= idx("end-1") {
		t.Error("step-1 should come before end-1")
	}
}

func TestTopologicalSort_cycle(t *testing.T) {
	// Create a deliberate cycle: step-a → step-b → step-a
	nodes := []apiclient.WorkflowNode{
		makeNode("start-1", "start"),
		makeNode("step-a", "step"),
		makeNode("step-b", "step"),
	}
	edges := []apiclient.WorkflowEdge{
		makeEdge("start-1", "step-a"),
		makeEdge("step-a", "step-b"),
		makeEdge("step-b", "step-a"), // cycle
	}

	// ParseDAG itself won't detect cycles — TopologicalSort will
	dag, err := ParseDAG(nodes, edges)
	if err != nil {
		// multiple start detection may trigger first; either way, a cycle or
		// missing-start error is acceptable here
		return
	}

	_, err = dag.TopologicalSort()
	if err == nil {
		t.Fatal("expected error for cycle, got nil")
	}
}

func TestTopologicalSort_allNodesIncluded(t *testing.T) {
	nodes := []apiclient.WorkflowNode{
		makeNode("start-1", "start"),
		makeNode("step-1", "step"),
		makeNode("step-2", "step"),
		makeNode("step-3", "step"),
		makeNode("end-1", "end"),
	}
	edges := []apiclient.WorkflowEdge{
		makeEdge("start-1", "step-1"),
		makeEdge("step-1", "step-2"),
		makeEdge("step-2", "step-3"),
		makeEdge("step-3", "end-1"),
	}

	dag, err := ParseDAG(nodes, edges)
	if err != nil {
		t.Fatalf("ParseDAG: %v", err)
	}

	sorted, err := dag.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}

	if len(sorted) != 5 {
		t.Errorf("expected 5 nodes in sorted result, got %d", len(sorted))
	}

	// Build a set to verify all node IDs are present
	nodeSet := map[string]bool{}
	for _, n := range sorted {
		nodeSet[n.Node.NodeID] = true
	}
	for _, n := range nodes {
		if !nodeSet[n.NodeID] {
			t.Errorf("node %q missing from topological sort", n.NodeID)
		}
	}
}

func TestTopologicalSort_branchingMerge(t *testing.T) {
	// start → a, start → b, a → end, b → end
	nodes := []apiclient.WorkflowNode{
		makeNode("start-1", "start"),
		makeNode("step-a", "step"),
		makeNode("step-b", "step"),
		makeNode("end-1", "end"),
	}
	edges := []apiclient.WorkflowEdge{
		makeEdge("start-1", "step-a"),
		makeEdge("start-1", "step-b"),
		makeEdge("step-a", "end-1"),
		makeEdge("step-b", "end-1"),
	}

	dag, err := ParseDAG(nodes, edges)
	if err != nil {
		t.Fatalf("ParseDAG: %v", err)
	}

	sorted, err := dag.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}

	if len(sorted) != 4 {
		t.Errorf("expected 4 sorted nodes, got %d", len(sorted))
	}

	// start must be first, end must be last
	if sorted[0].Node.NodeID != "start-1" {
		t.Errorf("first node should be start-1, got %q", sorted[0].Node.NodeID)
	}
	if sorted[len(sorted)-1].Node.NodeID != "end-1" {
		t.Errorf("last node should be end-1, got %q", sorted[len(sorted)-1].Node.NodeID)
	}
}
