# 에이전트에 툴 적용하는 방법

라우터에서 “에이전트가 툴을 쓰는” 흐름은 **두 가지**로 나뉩니다.

---

## 1. 워크플로 스텝에서 툴 사용 (현재 구현됨)

에이전트/워크플로 **스텝**으로 “도구 실행” 노드를 두고, 그 스텝이 **tool 스킬**을 쓰면 이미 툴 브리지가 연결되어 있습니다.

### 동작 흐름

```
워크플로 실행 → step 노드(skill_id = 툴 스킬) → StepExecutor.executeSkillNode
  → SkillRunner.RunSkill(skill_type="tool") → runTool
  → toolExecutor.Execute(ctx, tool_name, input)  ← CLIBridge (tools-bridge)
  → 결과를 스텝 output으로 반환
```

### 적용 방법

1. **라우터 설정**  
   `~/.nlook/config.yaml`에 도구 브리지 경로를 넣습니다.

   ```yaml
   tools_bridge_dir: "tools-bridge"
   ```

2. **서버/워크플로에서 스킬 정의**  
   - 스킬 타입: **`tool`**  
   - 스킬 이름: 브리지에 있는 도구 이름과 맞추면 됨 (예: `add`, `subtract`)  
   - 설정에 **`tool_name`** 이 있으면 그 값을, 없으면 **스킬 이름**을 도구 이름으로 사용합니다.

   예 (서버 API/DB 기준 개념):

   - `skill_type`: `"tool"`
   - `name`: `"add"` (또는 표시용 이름)
   - `config`: `{"tool_name": "add"}`

3. **워크플로에서 스텝 노드 연결**  
   - 해당 스킬을 참조하는 **step** 노드를 두고,  
   - 이전 노드에서 넘어온 `input`에 도구 인자(예: `a`, `b`)가 들어오도록 연결합니다.

4. **input 형식**  
   `runTool`은 `input`을 그대로 도구 인자로 넘깁니다.  
   예: 이전 노드 output이 `{"a": 1, "b": 2}`이면 `add` 도구에 `a=1, b=2`로 전달됩니다.

### 정리

- **에이전트**라고 하더라도, 지금은 “워크플로 안의 **step 노드**”로 실행되는 부분만 툴을 씁니다.
- 워크플로 편집기에서 “도구 실행” 스텝을 추가하고, 스킬을 `tool` 타입 + `tool_name`(또는 name)으로 맞추면, 라우터가 기동 시 주입한 `toolExecutor`(CLIBridge)로 해당 툴이 실행됩니다.

---

## 2. 단독 에이전트 런(Agent Run)에서 툴 사용 (미구현)

**RunType = "agent"** 인 런은 `executeAgentRun`에서 처리하는데, 현재는 **스텁**이라 LLM 호출이나 툴 호출이 없습니다.

### 현재 코드 위치

- `internal/executor/service.go`  
  - `executeAgentRun`: `AgentID`만 확인 후 "agent execution not yet implemented" 로 실패 처리.

### 툴을 적용하려면 필요한 처리

1. **에이전트 설정 조회**  
   - 서버 API로 `AgentID`에 해당하는 에이전트 상세(모델, system prompt, 사용할 툴 목록 등)를 가져옵니다.

2. **사용 가능 툴 목록**  
   - 라우터에 설정된 **도구 브리지** `ListTools()` 결과를 사용하거나,  
   - 에이전트 설정에 “이 에이전트가 쓸 툴 이름 목록”이 있으면 그에 맞춰 필터링합니다.

3. **LLM 호출 + tool_calls 루프**  
   - 사용할 모델(OpenAI/Anthropic/Ollama 등)로 **채팅 API** 호출 시,  
     `tools` 파라미터에 위에서 정한 툴 목록(이름, 설명, parameters 스키마)을 넘깁니다.
   - 응답에 **tool_calls**가 있으면:
     - 각 `tool_call`의 `name` / `arguments`로 **브리지 `Execute(ctx, name, arguments)`** 호출.
     - 결과를 메시지에 넣고 다시 LLM 호출 (반복).
   - `tool_calls`가 없으면 최종 응답으로 런 완료 처리.

4. **runTool과의 공유**  
   - 실제 실행은 지금처럼 **같은 `tools.Bridge`(CLIBridge)** 의 `Execute`를 쓰면 됩니다.  
   - 즉, “에이전트 런에 툴 적용” = **executeAgentRun 안에 LLM 루프 + 툴 목록 + tool_calls 시 Bridge.Execute 호출**을 넣는 작업입니다.

### 요약

| 구분 | 현재 | 툴 적용 시 할 일 |
|------|------|------------------|
| 워크플로 step + tool 스킬 | 구현됨. `runTool` → Bridge.Execute | 설정만 맞추면 됨 (위 1절) |
| 단독 에이전트 런 (RunType agent) | 스텁 (미구현) | executeAgentRun에 에이전트 조회 + ListTools + LLM 루프 + tool_calls 시 Execute 호출 구현 |

---

## 3. 요약

- **지금 에이전트/워크플로에서 툴을 쓰는 방법**  
  → 워크플로에 **step 노드**를 두고, 그 스텝의 스킬을 **type=tool**, **tool_name=도구이름**(또는 name=도구이름)으로 설정.  
  → 라우터에 `tools_bridge_dir` 설정 후 기동하면, 해당 스텝 실행 시 자동으로 툴 브리지가 호출됩니다.

- **단독 “에이전트 런”에서 툴을 쓰게 하려면**  
  → `executeAgentRun`을 구현할 때, 에이전트 설정 + **동일 툴 브리지 ListTools/Execute**를 붙여서 LLM tool_calls 루프를 넣으면 됩니다.
