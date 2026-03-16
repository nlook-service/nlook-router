package engine

import (
	"fmt"

	"github.com/nlook-service/nlook-router/internal/apiclient"
)

// DAGNode wraps a workflow node with graph traversal metadata.
type DAGNode struct {
	Node     *apiclient.WorkflowNode
	Children []*DAGNode // outgoing edges (source → target)
	Parents  []*DAGNode // incoming edges
}

// DAG represents the directed acyclic graph of a workflow.
type DAG struct {
	Nodes map[string]*DAGNode // key: WorkflowNode.NodeID (client-generated)
	Start *DAGNode
	End   *DAGNode
}

// ParseDAG builds a DAG from workflow nodes and edges.
// Nodes are indexed by their client-generated NodeID.
// Edges connect SourceNodeID → TargetNodeID.
func ParseDAG(nodes []apiclient.WorkflowNode, edges []apiclient.WorkflowEdge) (*DAG, error) {
	dag := &DAG{
		Nodes: make(map[string]*DAGNode, len(nodes)),
	}

	// Index all nodes
	for i := range nodes {
		n := &nodes[i]
		dag.Nodes[n.NodeID] = &DAGNode{Node: n}
	}

	// Build edges
	for _, edge := range edges {
		src, ok := dag.Nodes[edge.SourceNodeID]
		if !ok {
			continue // skip orphan edges
		}
		tgt, ok := dag.Nodes[edge.TargetNodeID]
		if !ok {
			continue
		}
		src.Children = append(src.Children, tgt)
		tgt.Parents = append(tgt.Parents, src)
	}

	// Find start and end nodes
	for _, dn := range dag.Nodes {
		switch dn.Node.NodeType {
		case "start":
			if dag.Start != nil {
				return nil, fmt.Errorf("multiple start nodes found")
			}
			dag.Start = dn
		case "end":
			if dag.End != nil {
				return nil, fmt.Errorf("multiple end nodes found")
			}
			dag.End = dn
		}
	}

	// If no explicit start node, use the first node with no incoming edges (in-degree 0)
	if dag.Start == nil {
		for _, dn := range dag.Nodes {
			if len(dn.Parents) == 0 {
				dag.Start = dn
				break
			}
		}
	}

	if dag.Start == nil {
		return nil, fmt.Errorf("no start node found")
	}

	return dag, nil
}

// TopologicalSort returns nodes in execution order using Kahn's algorithm.
// Start and end nodes are included in the result.
func (d *DAG) TopologicalSort() ([]*DAGNode, error) {
	inDegree := make(map[string]int, len(d.Nodes))
	for id, n := range d.Nodes {
		inDegree[id] = len(n.Parents)
	}

	// Queue starts with nodes that have no incoming edges
	queue := make([]*DAGNode, 0)
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, d.Nodes[id])
		}
	}

	sorted := make([]*DAGNode, 0, len(d.Nodes))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, current)

		for _, child := range current.Children {
			inDegree[child.Node.NodeID]--
			if inDegree[child.Node.NodeID] == 0 {
				queue = append(queue, child)
			}
		}
	}

	if len(sorted) != len(d.Nodes) {
		return nil, fmt.Errorf("workflow contains a cycle: sorted %d of %d nodes", len(sorted), len(d.Nodes))
	}

	return sorted, nil
}
