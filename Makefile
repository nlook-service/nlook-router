.PHONY: build test run clean build-all release release-note
BIN_DIR := bin
BINARY := nlook-router
RELEASE_DIR := dist
RELEASE_NOTES := RELEASE_NOTES.md

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/nlook-router

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

test:
	go test ./...

run: build
	./$(BIN_DIR)/$(BINARY) $(ARGS)

clean:
	rm -rf $(BIN_DIR) $(RELEASE_DIR)

# GitHub Release 배포: 태그 푸시 시 .github/workflows/release-router.yml 이 빌드 후 자산 업로드
# 1) 버전 맞춤 (router / packages/router-cli package.json)
# 2) make release-note 로 릴리스 노트 작성
# 3) make release VERSION=x.y.z 로 태그 생성 및 푸시
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
		echo "예: make release VERSION=0.1.1"; \
		exit 1; \
	fi; \
	TAG="router-v$$VERSION"; \
	echo "태그 생성 및 푸시: $$TAG"; \
	echo "푸시 후 GitHub Actions 가 빌드 후 Release 에 바이너리를 올립니다."; \
	git tag -a "$$TAG" -m "nlook-router $$VERSION" && git push origin "$$TAG"
