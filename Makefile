.PHONY: build test run start clean build-all release release-note release-check test-verbose tools-setup tools-test tools-go-test build-with-tools vendor-agno
BIN_DIR := bin
BINARY := nlook-router
RELEASE_DIR := dist
RELEASE_NOTES := RELEASE_NOTES.md

# Tools-bridge: 외부 agno 저장소를 내부 디렉터리(vendor)에 두고 사용
VENDOR_DIR := vendor
AGNO_DIR := $(VENDOR_DIR)/agno
AGNO_REPO_URL ?= https://github.com/agno-agi/agno.git
AGNO_REF ?= main
AGNO_LIBS := $(AGNO_DIR)/libs/agno

# build: vendor 불일치 시 -mod=mod 로 모듈 캐시 사용 (make start 등 로컬 실행용)
build:
	@mkdir -p $(BIN_DIR)
	go build -mod=mod -o $(BIN_DIR)/$(BINARY) ./cmd/nlook-router

# run.js 가 다운로드하는 이름과 맞춤: nlook-router-<platform>-<arch>[.exe]
# GitHub Release 에 올릴 플랫폼별 바이너리 빌드
build-all:
	@mkdir -p $(RELEASE_DIR)
	GOOS=darwin  GOARCH=arm64 go build -o $(RELEASE_DIR)/$(BINARY)-darwin-arm64    ./cmd/nlook-router
	GOOS=darwin  GOARCH=amd64 go build -o $(RELEASE_DIR)/$(BINARY)-darwin-x64      ./cmd/nlook-router
	GOOS=linux   GOARCH=amd64 go build -o $(RELEASE_DIR)/$(BINARY)-linux-x64        ./cmd/nlook-router
	GOOS=windows GOARCH=amd64 go build -o $(RELEASE_DIR)/$(BINARY)-win32-x64.exe    ./cmd/nlook-router
	@echo "✅ 플랫폼별 빌드 완료: $(RELEASE_DIR)/"
	@echo "   GitHub Release v<VERSION> 에 위 파일들을 업로드하세요."

# test: 전체 Go 테스트. 실패 시 터미널에 출력되는 메시지에서 원인 확인.
# vendor 경고가 나오면: make test ARGS="-mod=mod" 또는 go test -mod=mod ./...
test:
	go test $(ARGS) ./...

# test-verbose: 테스트별 이름·실패 상세 출력 (오류 확인용)
test-verbose:
	go test -mod=mod -v $(ARGS) ./...

run: build
	./$(BIN_DIR)/$(BINARY) $(ARGS)

# start: 로컬 라우터 실행 (build 후 실행). make start 또는 make start ARGS="router start"
start: build
	./$(BIN_DIR)/$(BINARY) router start

# vendor/agno: 외부 agno 저장소를 내부 디렉터리(vendor/agno)에 클론 (없을 때만)
# 버전 고정: make vendor-agno AGNO_REF=v2.5.9
vendor-agno:
	@if [ ! -d "$(AGNO_DIR)/.git" ]; then \
		mkdir -p $(VENDOR_DIR) && \
		echo "Cloning agno into $(AGNO_DIR) (ref=$(AGNO_REF))..." && \
		git clone --depth 1 --branch $(AGNO_REF) $(AGNO_REPO_URL) $(AGNO_DIR) || \
		( git clone --depth 1 $(AGNO_REPO_URL) $(AGNO_DIR) && cd $(AGNO_DIR) && git fetch --depth 1 origin $(AGNO_REF) 2>/dev/null && git checkout $(AGNO_REF) || true ); \
	else \
		echo "agno already at $(AGNO_DIR)"; \
	fi
	@if [ ! -d "$(AGNO_LIBS)" ]; then echo "Error: $(AGNO_LIBS) not found. Check AGNO_REF= (e.g. main, v2.5.9)"; exit 1; fi

# tools-setup: agno(vendor) + tools-bridge 설치. 한 번에 툴 연동 준비.
tools-setup: vendor-agno
	@echo "Installing agno from $(AGNO_LIBS)..."
	pip install -e "$(AGNO_LIBS)"
	@echo "Installing tools-bridge..."
	pip install -e tools-bridge
	@echo "✅ tools-setup 완료. config.yaml 에 tools_bridge_dir: \"tools-bridge\" 설정 후 라우터 기동."

# tools-test: 툴 목록(처음 5개) + add 한 번 실행 후 종료. (head 사용 시 BrokenPipeError 나므로 JSON 전체를 읽어 5개만 출력)
tools-test: tools-setup
	@python3 -m tool_bridge -q --list 2>/dev/null | python3 -c "import json,sys; L=json.load(sys.stdin); print(json.dumps(L[:5], indent=2))"
	@echo "---"
	@python3 -m tool_bridge -q --run add --args '{"a":1,"b":2}'
	@echo "✅ tools-test 완료."

# tools-go-test: Go에서 tools-bridge 연동 테스트 (ListTools, Execute, TestAll)
# 실패 시: 위에 출력되는 FAIL 메시지와 어느 툴/테스트에서 실패했는지 확인.
# 로그 저장: make tools-go-test 2>&1 | tee tools-test.log
tools-go-test: tools-setup
	@go test -mod=mod -v ./internal/tools/ -run TestCLIBridge -count=1 -timeout 120s

# build-with-tools: Go 빌드 + tools-setup 한 번에
build-with-tools: build tools-setup
	@echo "✅ 빌드 및 툴 연동 준비 완료."

clean:
	rm -rf $(BIN_DIR) $(RELEASE_DIR)

# GitHub Release 배포: 태그 푸시 시 .github/workflows/release-router.yml 이 빌드 후 자산 업로드
# 1) internal/cli/run_daemon.go 의 version 상수를 릴리스 버전으로 수정
# 2) make release-note 로 릴리스 노트 작성 (선택)
# 3) make release VERSION=x.y.z 로 태그 생성 및 푸시
# 자세한 절차: docs/RELEASE.md
release-check:
	@VERSION=$(VERSION); \
	if [ -z "$$VERSION" ]; then echo "사용법: make release-check VERSION=x.y.z"; exit 1; fi; \
	grep -q "const version = \"$$VERSION\"" internal/cli/run_daemon.go || \
		( echo "⚠️  internal/cli/run_daemon.go 의 version 을 \"$$VERSION\" 으로 수정한 뒤 release 하세요."; exit 1 ); \
	echo "✅ version=$$VERSION 일치"

release-note:
	@if [ ! -f $(RELEASE_NOTES) ]; then \
		echo "## 변경 사항" > $(RELEASE_NOTES); \
		echo "" >> $(RELEASE_NOTES); \
		echo "- " >> $(RELEASE_NOTES); \
		echo "" >> $(RELEASE_NOTES); \
		echo "다운로드 후 \`~/.nlook/router/\` 에 두거나 \`npx nlook-router\` 로 실행하세요." >> $(RELEASE_NOTES); \
		echo "✅ $(RELEASE_NOTES) 생성됨. 편집 후 커밋하세요."; \
	else \
		echo "$(RELEASE_NOTES) 이미 존재합니다."; \
	fi

release:
	@VERSION=$(VERSION); \
	if [ -z "$$VERSION" ]; then \
		echo "사용법: make release VERSION=x.y.z"; \
		echo "예: make release VERSION=0.2.9"; \
		exit 1; \
	fi; \
	$(MAKE) release-check VERSION=$$VERSION; \
	TAG="router-v$$VERSION"; \
	echo "태그 생성 및 푸시: $$TAG"; \
	echo "푸시 후 GitHub Actions 가 빌드 후 Release 에 바이너리를 올립니다."; \
	git tag -a "$$TAG" -m "nlook-router $$VERSION" && git push origin "$$TAG"
