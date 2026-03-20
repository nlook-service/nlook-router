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

    CH --> LE
    CH --> SS & CS & VS & MS
    EX --> WE
    WE --> SE --> SR
    SE --> GE
    SC --> EX
    EV --> SR
    EV -.->|StepHook| SE

    SR --> LE
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
        PR[prompt → LLM call]
        AP[api → HTTP request]
        TL[tool → Python bridge]
        MC[mcp → nlook API]
    end

    subgraph Output["Output"]
        CL[/"Report to Cloud (step:complete)"/]
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
        DELTA[/"chat:delta (token stream)"/]
        RESP[/"chat:response (final)"/]
        SAVE[DB: save conversation]
        MEM[Memory: extract facts]
        TRACE[Trace: log events]
    end

    REQ --> INT --> DOC --> PB
    PB --> P1 & P2 & P3 & P4 & P5 & P6
    PB --> LLM --> DELTA --> RESP
    LLM --> POST --> SAVE & MEM & TRACE
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

        subgraph StepEval["Workflow Step Eval (Phase 2)"]
            PREP[PrepareWorkflowEval]
            HOOK[StepEvalHook attached to StepExecutor]
            WE2[WorkflowEngine executes]
            CAP[Capture per-step output]
            SCORE2[Score against expected per NodeID]
            FIN[FinalizeWorkflowEval]
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
├── engine/         # Workflow DAG engine + StepExecutor + StepHook
├── eval/           # Evaluation framework (accuracy + step-level)
├── executor/       # Run dispatch (WebSocket + polling)
├── gemini/         # Gemini API client (cloud fallback)
├── heartbeat/      # Router registration + health ping
├── llm/            # LLM engine abstraction (vLLM / Ollama)
├── mcp/            # MCP tool client (nlook API tools)
├── memory/         # Long-term user memory (fact extraction)
├── ollama/         # Ollama client
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

## Security

- API key authentication for all cloud communication
- WebSocket over TLS (wss://)
- SSH host key verification (TOFU)
- Output rate limiting (512 KB/s per session)
- Agent command whitelist (`allowed_commands`)
- Local HTTP server binds to localhost only

## License

MIT
