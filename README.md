# nlook Router

Local execution engine for [nlook.me](https://nlook.me) — runs workflows, AI chat, agent terminals, and scheduled jobs on your machine.

## Install

```bash
# Go Install
go install github.com/nlook-service/nlook-router/cmd/nlook-router@latest

# Build from Source
git clone https://github.com/nlook-service/nlook-router.git
cd nlook-router && go build -o nlook-router ./cmd/nlook-router
```

### Requirements

| Tool | Purpose |
|------|---------|
| **Go** (1.21+) | Build & run |
| **Git** | Submodule / clone |
| **Python 3** + **pip** | tools-bridge (optional) |

## Setup

```bash
nlook-router config set api_key YOUR_API_KEY
nlook-router config set api_url https://nlook.me
nlook-router router start
```

Config: `~/.nlook/config.yaml` (overridable via `NLOOK_API_URL`, `NLOOK_API_KEY`)

---

## Architecture Overview

```mermaid
graph TB
    subgraph Cloud["nlook.me Cloud"]
        API[REST API]
        WSG[WebSocket Gateway]
        UI[Web UI]
    end

    subgraph Router["Local Router (your machine)"]
        WS[WebSocket Client]

        subgraph Handlers["Message Handlers"]
            CH[Chat Handler]
            EX[Execution Service]
            AH[Agent Proxy]
            SH[SSH Proxy]
            SY[Sync Handler]
        end

        subgraph Core["Core Engine"]
            WE[Workflow Engine]
            SE[Step Executor]
            SR[Skill Runner]
            GE[Group Executor]
            SC[Scheduler]
            EV[Eval Framework]
        end

        subgraph Reasoning["Reasoning Layer"]
            RM[Reasoning Manager]
            GR[Gemma Reasoner]
            CR[Claude Reasoner]
            DR[DeepSeek Reasoner]
            DC[Default CoT]
        end

        subgraph LLM["LLM Layer"]
            LE[LLM Engine]
            OL[Ollama]
            VL[vLLM]
            GM[Gemini/Claude fallback]
        end

        subgraph Storage["Persistence"]
            DB[(SQLite / File)]
            SS[Session Store]
            CS[Cache Store]
            VS[Vector Store]
            MS[Memory Store]
            TR[Trace Collector]
        end

        HTTP[HTTP Server :3333]
    end

    UI -->|user action| WSG
    WSG <-->|WebSocket| WS
    API <-->|REST| WS

    WS --> CH & EX & AH & SH & SY

    CH --> RM
    CH --> SS & CS & VS & MS
    EX --> WE
    WE --> SE --> SR
    SE --> GE
    SC --> EX
    EV -.->|StepEvalHook.OnStepComplete| SE
    WE -.->|OnRunFinished| EV

    SR --> RM
    RM --> GR & CR & DR & DC
    RM --> LE
    LE --> OL & VL & GM

    SE --> TR
    CH --> TR
    CH --> DB
    EX --> DB
    SY --> CS & VS
```

---

## Data Flow: Cloud to Router

```mermaid
sequenceDiagram
    participant C as nlook.me Cloud
    participant W as WebSocket Client
    participant R as Router Handlers
    participant E as Engine / LLM
    participant D as DB / Store

    C->>W: Connect (wss://nlook.me/api/routers/ws)
    W->>C: Register (router_id, tools, models)

    loop Heartbeat (30s)
        W->>C: heartbeat (status, usage)
    end

    C->>W: sync:documents / sync:tasks
    W->>R: Sync Handler
    R->>D: Update Cache + Vector Index

    C->>W: chat:request
    W->>R: Chat Handler
    R->>E: LLM ChatCompletion
    E-->>R: token stream
    R-->>W: chat:delta (streaming)
    W-->>C: chat:response (final)
    R->>D: Save conversation + memory

    C->>W: run:dispatch
    W->>R: Execution Service
    R->>E: WorkflowEngine.Execute()
    E-->>R: step:complete (per node)
    R-->>W: step:complete → run:status
    W-->>C: run:status (completed)
```

---

## Workflow Execution Flow

```mermaid
flowchart TB
    subgraph Trigger["Trigger"]
        T1[/"WebSocket: run:dispatch"/]
        T2[/"Scheduler: cron fires"/]
        T3[/"CLI: workflow run"/]
    end

    subgraph ExecService["Execution Service"]
        DI[DispatchRun]
        Q[Run Queue]
        W[Worker Pool]
    end

    subgraph Engine["Workflow Engine"]
        LOAD[Load WorkflowDetail]
        DAG[Parse DAG + Topological Sort]

        subgraph Loop["For each node in order"]
            SE[StepExecutor.Execute]
            BI[Build Input from parents]
            EN[Execute Node]
            HK[Fire StepHooks]
            SO[Store Output in RunContext]
        end
    end

    subgraph NodeTypes["Node Execution"]
        SK["skill/function → SkillRunner"]
        AG["agent → SkillRunner"]
        CD["condition → GroupExecutor"]
        LP["loop → GroupExecutor"]
        PL["parallel → GroupExecutor"]
    end

    subgraph SkillTypes["Skill Types"]
        PR{reasoning\nenabled?}
        PR1[prompt → Reasoning Manager → structured result]
        PR2[prompt → LLM 1-shot call]
        AP[api → HTTP request]
        TL[tool → Python bridge]
        MC[mcp → nlook API]
    end

    subgraph Output["Output"]
        CL[/"Report to Cloud (step:complete)\n+ ReasoningData"/]
        RC[RunContext.nodeOutputs]
        EH[/"Eval Hook (optional)"/]
        RS[/"run:status (final)"/]
    end

    T1 & T2 & T3 --> DI --> Q --> W
    W --> LOAD --> DAG --> SE
    SE --> BI --> EN --> HK --> SO
    SO -->|next node| SE

    EN --> SK & AG & CD & LP & PL
    SK & AG --> PR & AP & TL & MC
    PR -->|yes| PR1
    PR -->|no| PR2

    SE --> CL
    HK -.-> EH
    SO -->|all done| RS
```

---

## Chat Flow

```mermaid
flowchart LR
    subgraph Input
        REQ[/"chat:request\n{query, session_id, user_id}"/]
    end

    subgraph ChatHandler["Chat Handler"]
        INT[Intent Detection]
        DOC[Document Ref Extraction]
        PB[Prompt Builder]
        RZ{Reasoning\nEnabled?}
        RSN[Reasoning Manager]
        LLM[LLM Call]
        POST[Post-Response]
    end

    subgraph PromptBuilder["System Prompt Assembly"]
        P1["[System] base instructions"]
        P2["[User Profile + Memory]"]
        P3["[RAG Context] vector search top-5"]
        P4["[Data Summary] cached docs/tasks"]
        P5["[Recent Chat] last 6 + summary"]
        P6["[Language] auto-detected"]
    end

    subgraph Response
        DELTA[/"chat:delta (token stream)\n+ delta_type: thinking|step|content"/]
        RESP[/"chat:response (final)\n+ reasoning: ReasoningData"/]
        SAVE[DB: save conversation]
        MEM[Memory: extract facts]
        TRACE[Trace: log events + reasoning steps]
    end

    REQ --> INT --> DOC --> PB
    PB --> P1 & P2 & P3 & P4 & P5 & P6
    PB --> RZ
    RZ -->|":thinking" model| RSN --> DELTA
    RZ -->|normal| LLM --> DELTA
    DELTA --> RESP
    RSN & LLM --> POST --> SAVE & MEM & TRACE
```

---

## Scheduler Flow

```mermaid
flowchart TB
    START[Scheduler.Start] --> SYNC[Fetch schedules from cloud]
    SYNC --> CRON[Register cron jobs]

    LOOP[/"Every 30s: re-sync schedules"/] --> SYNC

    CRON -->|cron fires| CREATE[Create Run via API]
    CREATE --> DISPATCH[ExecutionService.DispatchRun]

    DISPATCH --> WF{Execution Type?}
    WF -->|workflow| ENGINE[WorkflowEngine.Execute]
    WF -->|api| HTTP[Direct HTTP call]
    WF -->|agent| AGENT[Agent execution]

    ENGINE & HTTP & AGENT --> REPORT[Report run:status to cloud]
```

---

## Eval Framework (Where It Fits)

```mermaid
flowchart TB
    subgraph Triggers["Eval Trigger"]
        CLI[/"CLI: eval run <set-id>"/]
        FUTURE[/"Future: REST API / run:dispatch"/]
    end

    subgraph EvalRunner["Eval Runner"]
        LOAD[Load EvalSet + Cases]

        subgraph ChatEval["Chat Eval (Phase 1)"]
            GEN[Generate Answer via LLM]
            SCORE[AccuracyEvaluator scores 1-10]
            PERF[Measure latency + tokens]
        end

        subgraph StepEval["Workflow Step Eval (Phase 2 — server+router)"]
            PREP["PrepareWorkflowEval(evalSetID, executor)\n→ creates StepEvalHook, attaches via AddHook()"]
            HOOK["StepEvalHook.OnStepComplete(StepEvent)\n→ collects StepCompleteData per node"]
            WE2["WorkflowEngine.Execute()\n→ fires hooks after each step"]
            CAP["OnRunFinished callback\n→ triggers FinalizeWorkflowEval()"]
            SCORE2[Score against expected per NodeID]
            FIN["FinalizeWorkflowEval(hook, runID, wfID)\n→ aggregates EvalResult"]
        end

        STATS[Calculate avg / stddev]
    end

    subgraph Persist["Persistence"]
        DB[(eval_sets\neval_cases\neval_runs\neval_results)]
        OUT[/"CLI output: score table"/]
    end

    CLI --> LOAD
    LOAD -->|target_type=chat| GEN --> SCORE --> PERF --> STATS
    LOAD -->|target_type=workflow| PREP --> HOOK --> WE2 --> CAP --> SCORE2 --> FIN --> STATS
    STATS --> DB --> OUT
```

### Eval DB Schema

```
eval_sets ──1:N──> eval_cases (node_id for step targeting)
eval_sets ──1:N──> eval_runs  ──1:N──> eval_results
```

### CLI Quick Start

```bash
# Create eval set
nlook-router eval create my-chat-test --type chat

# Add test cases
nlook-router eval add <set-id> --input "What is nlook?" --expected "nlook is a..."

# Add step-level case (workflow eval)
# Import via JSON with node_id field for per-step targeting

# Run evaluation
nlook-router eval run <set-id> --model qwen2:4b --evaluator qwen2:4b --iterations 3

# View results
nlook-router eval results <run-id>
```

---

## Package Structure

```
internal/
├── apiclient/      # REST client → nlook.me API
├── agentproxy/     # Claude Code CLI terminal sessions
├── cache/          # Document/task cache (synced from cloud)
├── chat/           # AI chat: intent → prompt → LLM → response
├── cli/            # Cobra CLI commands
├── config/         # YAML config loading
├── db/             # DB interface (SQLite / file-based)
├── embedding/      # Vector embeddings for RAG
├── engine/         # Workflow DAG engine + StepExecutor + StepHook + OnRunFinished
├── eval/           # Evaluation framework (accuracy + step-level via StepEvalHook)
├── executor/       # Run dispatch (WebSocket + polling)
├── gemini/         # Gemini API client (cloud fallback)
├── heartbeat/      # Router registration + health ping
├── llm/            # LLM engine abstraction (vLLM / Ollama)
├── mcp/            # MCP tool client (nlook API tools)
├── memory/         # Long-term user memory (fact extraction)
├── ollama/         # Ollama client
├── reasoning/      # Reasoning engine (CoT + native thinking)
├── scheduler/      # Cron-based workflow scheduling
├── server/         # Local HTTP server (:3333)
├── session/        # Session store with TTL
├── sshproxy/       # SSH terminal relay
├── tokenizer/      # Token counting & budgeting
├── tools/          # Built-in tools + Python bridge
├── tracing/        # Execution event tracing
├── usage/          # Token usage tracking
└── ws/             # WebSocket client to cloud
```

---

## Local HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Liveness check |
| GET | `/status` | Router ID, connection status |
| GET | `/status/model` | Current LLM model info |
| GET | `/tools` | Available tools list |
| GET | `/sessions` | Active sessions |
| GET | `/sessions/{id}/traces` | Execution trace events |

All endpoints listen on `127.0.0.1:3333` (localhost only).

---

## Configuration

```yaml
# ~/.nlook/config.yaml
api_url: https://nlook.me
api_key: your-api-key
router_id: ""                    # auto-generated
port: 3333

llm_engine: "ollama"             # or "vllm"
ai_model: "qwen2:4b"

db:
  driver: "sqlite"               # or "file"
  data_dir: "~/.nlook"

eval:
  evaluator_model: ""            # defaults to ai_model
  default_iterations: 1
  max_iterations: 10
  timeout_seconds: 120

agent:
  workspaces: []
  max_sessions: 5
  session_timeout: 60m
  allowed_commands: ["claude"]
```

Env overrides: `NLOOK_API_URL`, `NLOOK_API_KEY`, `NLOOK_LLM_ENGINE`, `NLOOK_AI_MODEL`, `VLLM_BASE_URL`

---

## Startup Sequence

```mermaid
flowchart LR
    A[Load Config] --> B[Init Stores]
    B --> C[Init LLM Engine]
    C --> D[Init Handlers]
    D --> E[Connect WebSocket]
    E --> F[Start Scheduler]
    F --> G[Start HTTP Server]
    G --> H[Wait for shutdown]

    B --> B1[Session Store]
    B --> B2[Cache + Vector]
    B --> B3[Memory Store]
    B --> B4[DB Layer]

    D --> D1[Chat Handler]
    D --> D2[Execution Service]
    D --> D3[Agent Proxy]
    D --> D4[SSH Proxy]
```

---

## Reasoning Architecture

The reasoning layer enables LLMs to **think step-by-step before answering**, improving accuracy on complex tasks. All reasoning data is returned as structured JSON in every response.

### How It Works

```
                          ┌─ Gemma <think> tag extraction (primary)
Request ─→ Reasoning ─────┼─ Claude extended thinking
           Manager        ├─ DeepSeek R1 <think> tag
                          └─ Default CoT loop (any model)
```

### Activation

| Context | How to enable |
|---------|--------------|
| Chat | Use model name with `:thinking` suffix: `gemma3:12b:thinking` |
| Workflow Skill | Set `reasoning_enabled: true` in skill config |
| Workflow Agent | Set `reasoning_enabled: true` in agent config |

### Structured Response Data

When reasoning is enabled, all responses include a `reasoning` field:

```json
{
  "content": "final answer text",
  "reasoning": {
    "enabled": true,
    "provider": "gemma",
    "model": "gemma3:12b",
    "steps": [
      {"title": "Step 1", "reasoning": "...", "confidence": 0.9, "next_action": "continue"},
      {"title": "Step 2", "reasoning": "...", "confidence": 0.85, "next_action": "final_answer"}
    ],
    "thinking_text": "raw <think> content",
    "total_ms": 2340,
    "tokens_used": 512,
    "step_count": 2,
    "avg_confidence": 0.875
  }
}
```

### Streaming Delta Types

During chat streaming, `delta_type` indicates the content category:

| delta_type | Description |
|-----------|-------------|
| `thinking` | Model's internal reasoning process |
| `step` | Completed reasoning step (includes structured `reasoning_step`) |
| `content` | Final answer text (default, backward-compatible) |

### Design Docs

- Plan: `docs/01-plan/features/router-reasoning-architecture.plan.md`
- Design: `docs/02-design/features/router-reasoning-architecture.design.md`

---

## Security

- API key authentication for all cloud communication
- WebSocket over TLS (wss://)
- SSH host key verification (TOFU)
- Output rate limiting (512 KB/s per session)
- Agent command whitelist (`allowed_commands`)
- Local HTTP server binds to localhost only

## License

MIT
