# nlook Router

Local router for [nlook.me](https://nlook.me) — executes workflows on your machine, provides SSH terminal access, and relays real-time communication via WebSocket.

## Features

- **Workflow Execution** — Runs workflow DAG locally with step-by-step data pipeline
- **SSH Terminal** — Web terminal access through your local machine (no sshd needed for localhost)
- **WebSocket Relay** — Real-time communication between nlook.me and your machine
- **Heartbeat** — Automatic registration and health monitoring

## Install

### Go Install
```bash
go install github.com/nlook-service/nlook-router/cmd/nlook-router@latest
```

### Build from Source
```bash
git clone https://github.com/nlook-service/nlook-router.git
cd nlook-router
go build -o nlook-router ./cmd/nlook-router
```

### Make 실행 시 로컬 요구사항

| 도구 | 용도 |
|------|------|
| **make** | Makefile 타깃 실행 (macOS/Linux 기본 또는 `xcode-select --install` / `brew install make`) |
| **Go** (1.21+) | `make build`, `make test`, `make tools-go-test` |
| **Git** | `make vendor-agno` (agno 클론) |
| **Python 3** + **pip** | `make tools-setup`, `make tools-test`, `make tools-go-test` (tools-bridge 사용 시) |

- **라우터만 빌드/실행:** `make build`, `make run` → Go만 있으면 됨.
- **tools-bridge 연동까지:** `make tools-setup` 또는 `make build-with-tools` → Go, Git, Python3, pip 필요.
- **전체 테스트:** `make test` (Go만), `make tools-go-test` (Python 환경 필요).

## Setup

1. Get an API key from [nlook.me](https://nlook.me) → Settings → Routers
2. Configure:
```bash
nlook-router config set api_key YOUR_API_KEY
nlook-router config set api_url https://nlook.me
```

## Usage

### Start Router
```bash
nlook-router router start
```

### Check Status
```bash
nlook-router router status
```

### Workflow Commands
```bash
nlook-router workflow list
nlook-router workflow run <workflow-id>
```

## Configuration

Config file: `~/.nlook/config.yaml`

```yaml
api_url: https://nlook.me
api_key: your-api-key
router_id: ""
port: 3333
```

Environment variables override config:
- `NLOOK_API_URL`
- `NLOOK_API_KEY`

## Architecture

```
nlook.me (Cloud)
  ↕ WebSocket
Local Router (Your Machine)
  ├── Workflow Engine (DAG execution)
  ├── SSH Proxy (localhost shell / remote SSH)
  ├── Heartbeat (registration)
  └── HTTP Server (status: localhost:3333)
```

## Security

- API key authentication for all cloud communication
- SSH host key verification (TOFU — Trust On First Use)
- Output rate limiting (512 KB/s per session)
- Max 10 concurrent sessions
- 30-minute idle timeout

## License

MIT
