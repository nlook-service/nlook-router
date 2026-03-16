# 툴 리스트 실행 및 테스트 방법

## API 제공 현황

| 제공처 | API | 설명 |
|--------|-----|------|
| **라우터 (Go)** | `GET http://127.0.0.1:3333/tools` | 사용 가능한 툴 목록 JSON 반환. `tools_bridge_dir` 설정 시에만 동작(미설정 시 503). |
| **라우터 (Go)** | `POST api/routers/register`, `POST api/routers/heartbeat` | 요청 본문에 `tools` 배열 포함해 서버로 전달(툴 목록 등록). |
| **Python tools-bridge** | HTTP API 없음 | CLI만 제공 (`--list`, `--run`). FastAPI 등 HTTP 서버는 미구현. |
| **라우터** | 도구 실행 전용 HTTP API 없음 | 도구 실행은 워크플로 스텝(tool 스킬) 실행 시 내부적으로 브리지 CLI 호출로만 수행됨. |

정리: **툴 목록 조회**는 라우터 `GET /tools`로 API 제공됨. **도구 실행**은 현재 워크플로 실행 경로에서만 되고, “도구 하나만 실행”하는 공개 HTTP API는 없음.

---

## 1. Python 도구 브리지만 테스트 (CLI)

라우터 없이 도구 목록·실행만 확인할 때 사용합니다.

### 설치 (최초 1회)

```bash
# 라우터 레포 루트에서
cd tools-bridge

# Agno 설치 (로컬 클론 사용 시)
pip install -e /path/to/agno/libs/agno
# 또는 PyPI: pip install agno>=2.5.0

# 도구 브리지 설치
pip install -e .
```

### 툴 리스트 조회

```bash
cd /path/to/nlook-router/tools-bridge
python3 -m tool_bridge --list
```

출력 예: `[{"name":"add",...},{"name":"subtract",...},...]` (JSON 배열)

### 도구 실행 테스트

```bash
python3 -m tool_bridge --run add --args '{"a": 1, "b": 2}'
```

출력 예: `{"status":"success","result":"{\"operation\": \"addition\", \"result\": 3}","error":null}`

### Python 단위 테스트

```bash
cd /path/to/nlook-router
python3 tools-bridge/tests/test_cli.py
# 기대: "OK"
```

---

## 2. 라우터에서 툴 리스트 사용

라우터가 도구 브리지를 붙이면:

- **시작 시** `tools-bridge --list`로 툴 목록을 가져와 **서버 register/heartbeat** 시 `payload.tools`로 전달합니다.
- **GET /tools** 로컬 API로 “현재 사용 가능한 툴 목록”을 조회할 수 있습니다.

### 설정

`~/.nlook/config.yaml`에 도구 브리지 경로를 넣습니다.

```yaml
api_url: https://nlook.me
api_key: your-api-key
router_id: ""
port: 3333

# 도구 브리지 디렉터리 (라우터 실행 파일 기준 상대 경로 또는 절대 경로)
tools_bridge_dir: "tools-bridge"
```

- 라우터를 **소스에서 실행**할 때: `tools-bridge`는 `nlook-router` 바이너리와 같은 디렉터리 안의 `tools-bridge` 폴더를 가리키면 됩니다.  
  예: `go run ./cmd/nlook-router router start` 로 실행하면 작업 디렉터리가 기준이므로, **프로젝트 루트**에서 실행하고 `tools_bridge_dir: "tools-bridge"` 로 두면 됩니다.
- **설치된 바이너리**로 실행할 때: 바이너리 위치 기준으로 `tools-bridge`를 두거나, 절대 경로로 지정합니다.  
  예: `tools_bridge_dir: "/path/to/nlook-router/tools-bridge"`

### 라우터 실행

```bash
# 프로젝트 루트에서 (tools-bridge 폴더가 같은 디렉터리에 있을 때)
cd /path/to/nlook-router
nlook-router router start
# 또는
go run ./cmd/nlook-router router start
```

### 툴 리스트 API로 확인 (GET /tools)

라우터가 떠 있는 상태에서:

```bash
curl -s http://127.0.0.1:3333/tools | jq .
```

- **설정됨 + 정상**: HTTP 200, JSON 배열 (add, subtract 등).
- **도구 미설정**: HTTP 503, `{"error":"tools not configured"}`.

### 서버로 툴 리스트 전달

- `tools_bridge_dir`가 설정되어 있으면, 라우터 기동 시 `tools-bridge --list`를 호출해 툴 목록을 가져옵니다.
- 이 목록은 **Register** 및 **Heartbeat** 요청의 `tools` 필드에 담겨 서버로 전송됩니다.
- 서버(백엔드)가 해당 필드를 저장·노출하도록 구현되어 있어야 화면/API에서 “이 라우터가 가진 도구 목록”을 볼 수 있습니다.

---

## 3. Go 테스트로 확인

```bash
cd /path/to/nlook-router

# 도구 패키지 테스트 (StaticLister + CLIBridge 통합, Python 필요)
go test ./internal/tools/ -v -count=1

# 서버 GET /tools 핸들러 테스트
go test ./internal/server/ -v -run TestToolsHandler -count=1

# runTool + mock executor 테스트
go test ./internal/engine/ -v -run TestSkillRunner_RunSkill_tool -count=1
```

CLIBridge 통합 테스트는 `tools-bridge` 디렉터리와 `python3 -m tool_bridge`가 사용 가능할 때만 통과하며, 없으면 자동으로 스킵됩니다.

---

## 요약

| 목적                 | 방법 |
|----------------------|------|
| 툴 목록만 보기       | `python3 -m tool_bridge --list` (tools-bridge 디렉터리에서) |
| 툴 실행만 테스트     | `python3 -m tool_bridge --run add --args '{"a":1,"b":2}'` |
| 라우터에서 툴 목록  | config에 `tools_bridge_dir` 설정 후 라우터 기동 → `curl http://127.0.0.1:3333/tools` |
| 서버로 툴 전달      | 위 설정으로 라우터 기동 시 register/heartbeat에 `tools` 자동 포함 |
| 자동화 테스트       | `go test ./internal/tools/ ./internal/server/ ./internal/engine/ -v -run 'Tools\|Tool\|tool'` |
