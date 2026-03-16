# Nlook Tools Bridge

Thin wrapper over [Agno](https://github.com/agno-agi/agno) tools. Exposes tool list and execution via CLI for the Go router. **Agno source is not modified.**

## Install

tools-bridge는 **agno 패키지를 import**합니다 (`agno.tools`, `agno.utils.functions` 등).  
따라서 **agno를 먼저 설치한 뒤** tools-bridge를 설치해야 합니다.

```bash
# 1) Agno 설치 (둘 중 하나)
pip install agno>=2.5.0
# 또는 로컬 소스로 버전 고정:
pip install -e /path/to/agno/libs/agno

# 2) tools-bridge 설치 (router 레포 tools-bridge/ 에서)
cd tools-bridge
pip install -e .
```

## Usage

```bash
# List available tools (JSON array)
python3 -m tool_bridge --list

# Run a tool
python3 -m tool_bridge --run add --args '{"a": 1, "b": 2}'
# Output: {"status": "success", "result": "...", "error": null}

# Test all tools once (safe args or {}), JSON array of {name, status, error?}
python3 -m tool_bridge -q --test-all
```

## Default toolkits

`tool_bridge/loader.py` 에서 **모든 Agno 툴킷**을 시도합니다. import/초기화에 실패한 항목만 제외되고, 에러 메시지는 stderr로 출력됩니다.  
에러가 나면 아래 환경 설정 가이드를 참고해 환경 변수나 패키지를 맞추면 됩니다.

---

## 환경 설정 가이드

`python3 -m tool_bridge --list` 또는 `--run` 실행 시 **ERROR/WARNING**이 나오면, 해당 툴을 쓰기 위해 아래처럼 설정하면 됩니다.

### 1. "XXX not set" / "must be set" (환경 변수)

에러 메시지에 나온 변수명을 그대로 환경 변수로 설정합니다.

```bash
# 예: CalCom
export CALCOM_API_KEY="your-key"
export CALCOM_EVENT_TYPE_ID="your-event-type-id"

# 예: Discord
export DISCORD_BOT_TOKEN="your-token"

# 예: Zoom
export ZOOM_ACCOUNT_ID="..."
export ZOOM_CLIENT_ID="..."
export ZOOM_CLIENT_SECRET="..."

# 예: Slack, GitHub, OpenWeather 등도 메시지에 나온 변수명으로 설정
export SLACK_BOT_TOKEN="..."
export GITHUB_TOKEN="..."
export OPENWEATHER_API_KEY="..."
```

`.env` 파일을 쓰는 경우 프로젝트 루트에 두고, 실행 전에 `source .env` 또는 도구에서 로드하도록 하면 됩니다.

### 2. "No module named 'xxx'" / "package not installed" (패키지 설치)

에러에 나온 패키지명으로 설치합니다.

```bash
# 예: DuckDB
pip install duckdb

# 예: Docker 클라이언트
pip install docker

# 예: PDF 생성 (reportlab)
pip install reportlab

# 예: Google Maps
pip install googlemaps google-maps-places

# 예: Webex
pip install webexpythonsdk
```

### 3. 자주 쓰는 툴킷별 요약

| 툴킷(대표) | 환경 변수 예시 | 비고 |
|------------|----------------|------|
| CalCom | `CALCOM_API_KEY`, `CALCOM_EVENT_TYPE_ID` | |
| Discord | `DISCORD_BOT_TOKEN` | 봇 토큰 |
| Financial Datasets | `FINANCIAL_DATASETS_API_KEY` | |
| GitHub | `GITHUB_TOKEN` 등 | |
| Google (Maps/Drive/Gmail 등) | 해당 서비스 OAuth 또는 API 키 | `googlemaps`, `google-maps-places` 등 패키지 필요 시 `pip install` |
| Models Lab | `MODELS_LAB_API_KEY` | |
| Slack | `SLACK_BOT_TOKEN` 등 | |
| Unsplash | `UNSPLASH_ACCESS_KEY` | |
| WhatsApp | `WHATSAPP_ACCESS_TOKEN`, `WHATSAPP_PHONE_NUMBER_ID` | |
| Webex | — | `pip install webexpythonsdk` |
| Zendesk | 계정(사용자명/비밀번호/회사명) | 인자 또는 환경 변수 |
| Zoom | `ZOOM_ACCOUNT_ID`, `ZOOM_CLIENT_ID`, `ZOOM_CLIENT_SECRET` | |

정확한 변수명·필수 인자는 실행 시 stderr에 찍히는 에러 메시지를 보면 됩니다.

### 4. 로그 없이 JSON만 쓰고 싶을 때

```bash
python3 -m tool_bridge -q --list
python3 -m tool_bridge -q --run add --args '{"a":1,"b":2}'
```

`-q`(또는 `--quiet`)를 쓰면 stderr 로그는 숨기고 stdout의 JSON만 사용할 수 있습니다.

---

## Go에서 테스트 (Golang)

라우터 레포에서 Go로 tools-bridge 연동을 테스트할 수 있습니다.  
`internal/tools` 패키지의 `CLIBridge`가 `python3 -m tool_bridge --list` / `--run`을 호출하는지 검증합니다.

**전제:** agno + tools-bridge 설치 완료 (`make tools-setup` 또는 수동 `pip install -e tools-bridge`).

```bash
# router 레포 루트에서
cd /path/to/router

# tools-bridge 연동만 테스트 (ListTools, Execute add)
go test -mod=mod -v ./internal/tools/ -run TestCLIBridge -count=1
```

- `TestCLIBridge_ListTools_integration`: `--list` 호출 후 툴 목록에 `add`가 있는지 확인.
- `TestCLIBridge_Execute_integration`: `--run add --args '{"a":1,"b":2}'` 호출 후 결과가 3인지 확인.

tools-bridge가 없거나 agno가 없으면 해당 테스트는 `t.Skipf`로 건너뜁니다.  
vendor 사용 중이면 `go test`에 `-mod=mod`를 붙이거나, `make tools-go-test`를 사용하세요.
