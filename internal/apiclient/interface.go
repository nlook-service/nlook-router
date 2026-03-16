package apiclient

import "context"

// Workflow is a minimal workflow representation from the API.
type Workflow struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// WorkflowDetail contains all data needed for execution.
type WorkflowDetail struct {
	ID     int64           `json:"id"`
	Title  string          `json:"title"`
	Nodes  []WorkflowNode  `json:"nodes"`
	Edges  []WorkflowEdge  `json:"edges"`
	Skills []WorkflowSkill `json:"skills"`
	Agents []WorkflowAgent `json:"agents"`
}

// WorkflowNode is a single node in a workflow graph.
type WorkflowNode struct {
	NodeID   string                 `json:"node_id"`
	NodeType string                 `json:"node_type"`
	RefID    int64                  `json:"ref_id"`
	ParentID string                 `json:"parent_id"`
	Data     map[string]interface{} `json:"data"`
}

// WorkflowEdge connects two nodes in a workflow graph.
type WorkflowEdge struct {
	EdgeID       string `json:"edge_id"`
	SourceNodeID string `json:"source_node_id"`
	TargetNodeID string `json:"target_node_id"`
	Label        string `json:"label"`
}

// WorkflowSkill is a reusable skill referenced by workflow nodes.
type WorkflowSkill struct {
	ID        int64                  `json:"id"`
	Name      string                 `json:"name"`
	SkillType string                 `json:"skill_type"`
	Content   string                 `json:"content"`
	Config    map[string]interface{} `json:"config"`
}

// WorkflowAgent is an AI agent referenced by workflow nodes.
type WorkflowAgent struct {
	ID           int64                  `json:"id"`
	Name         string                 `json:"name"`
	Model        string                 `json:"model"`
	SystemPrompt string                 `json:"system_prompt"`
	Temperature  float64                `json:"temperature"`
	MaxTokens    int                    `json:"max_tokens"`
	Config       map[string]interface{} `json:"config"`
}

// RunInfo represents a pending workflow run.
type RunInfo struct {
	ID          int64                  `json:"id"`
	WorkflowID  int64                  `json:"workflow_id"`
	UserID      int64                  `json:"user_id"`
	RunType     string                 `json:"run_type"`
	AgentID     int64                  `json:"agent_id"`
	TraceID     string                 `json:"trace_id"`
	Input       map[string]interface{} `json:"input"`
	EndpointURL string                 `json:"endpoint_url,omitempty"`
	HTTPMethod  string                 `json:"http_method,omitempty"`
}

// StepLogRef holds the ID of a created step log entry.
type StepLogRef struct {
	ID int64 `json:"id"`
}

// ToolMeta describes a single tool available on the router (e.g. from Agno bridge).
// Used when sending available tools to the server on register/heartbeat.
type ToolMeta struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Category    string                 `json:"category,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// RegisterPayload is sent to the server to register this router.
type RegisterPayload struct {
	RouterID string      `json:"router_id"`
	Version  string      `json:"version"`
	Tools    []ToolMeta  `json:"tools,omitempty"`
}

// Schedule represents a workflow schedule from the server.
type Schedule struct {
	ID             int64                  `json:"id"`
	WorkflowID     int64                  `json:"workflow_id"`
	ExecutionType  string                 `json:"execution_type"`
	AgentID        int64                  `json:"agent_id"`
	EndpointURL    string                 `json:"endpoint_url"`
	HTTPMethod     string                 `json:"http_method"`
	Name           string                 `json:"name"`
	CronExpression string                 `json:"cron_expression"`
	Timezone       string                 `json:"timezone"`
	Enabled        bool                   `json:"enabled"`
	Input          map[string]interface{} `json:"input,omitempty"`
	NextRunAt      string                 `json:"next_run_at,omitempty"`
	LastRunAt      string                 `json:"last_run_at,omitempty"`
}

// Interface defines the nlook API client contract for testing and DI.
type Interface interface {
	ListWorkflows(ctx context.Context) ([]Workflow, error)
	GetWorkflow(ctx context.Context, id int64) (*Workflow, error)
	RegisterRouter(ctx context.Context, payload *RegisterPayload) error
	Heartbeat(ctx context.Context, payload *RegisterPayload) error

	GetWorkflowDetail(ctx context.Context, id int64) (*WorkflowDetail, error)
	GetPendingRuns(ctx context.Context, workflowID int64) ([]RunInfo, error)
	UpdateRunStatus(ctx context.Context, workflowID, runID int64, status string, output map[string]interface{}, errMsg string) error
	StartStep(ctx context.Context, workflowID, runID int64, nodeID, nodeType string) (*StepLogRef, error)
	CompleteStep(ctx context.Context, workflowID, runID, logID int64, status string, output map[string]interface{}, errMsg string, logLines []string) error

	// Schedule operations
	GetSchedules(ctx context.Context, workflowID int64) ([]Schedule, error)
	CreateRun(ctx context.Context, workflowID int64, input map[string]interface{}, triggerType string, scheduleID int64) (*RunInfo, error)
	CreateRunWithParams(ctx context.Context, params CreateRunParams) (*RunInfo, error)

	// Agent operations
	GetAgentDetail(ctx context.Context, agentID int64) (*WorkflowAgent, error)
}
