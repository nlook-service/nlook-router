# 배포 및 Release 방법

## 요약

1. **버전 올리기** → `internal/cli/run_daemon.go` 의 `version` 상수 수정
2. **릴리스 노트** → `make release-note` 후 `RELEASE_NOTES.md` 편집 (선택)
3. **태그 푸시** → `make release VERSION=x.y.z` 실행
4. **자동 빌드** → GitHub Actions 가 GoReleaser로 빌드 후 GitHub Release 에 업로드

---

## 1. 버전 수정

릴리스할 버전으로 소스의 version 을 맞춥니다.

```bash
# internal/cli/run_daemon.go
const version = "0.2.9"   # 예: 0.2.8 → 0.2.9
```

---

## 2. 릴리스 노트 (선택)

```bash
make release-note
# RELEASE_NOTES.md 생성됨 → 변경 사항 편집 후 커밋
```

---

## 3. 커밋 및 태그 푸시

```bash
git add internal/cli/run_daemon.go RELEASE_NOTES.md
git commit -m "chore: release v0.2.9"
make release VERSION=0.2.9
```

- `router-v0.2.9` 태그가 생성되고 `origin` 에 푸시됩니다.
- GitHub Actions 가 태그 푸시를 감지하고 **GoReleaser** 를 실행합니다.
- 빌드된 바이너리(nlook-router_linux_amd64.tar.gz 등)가 **Releases** 에 올라갑니다.

---

## 4. 배포물 내용

- **Release 에 포함되는 것**: Go 로 빌드된 **nlook-router 바이너리** (linux/darwin/windows, amd64/arm64)
- **포함되지 않는 것**: `vendor/agno`, `tools-bridge` (Python). 툴 사용 시 사용자가 레포 클론 후 `make tools-setup` 또는 `make build-with-tools` 로 별도 준비.

---

## 5. 수동으로 빌드만 할 때

```bash
make build-all
# dist/ 에 nlook-router-{os}-{arch}[.exe] 생성
# GitHub Release 페이지에서 직접 업로드 가능
```

---

## 6. 트러블슈팅

- **Actions 에서 GoReleaser 실패**: 저장소 설정 → Actions → Workflow permissions 가 “Read and write permissions” 인지 확인.
- **태그가 이미 있을 때**: `git tag -d router-v0.2.9` 후 다시 `make release VERSION=0.2.9` (이미 원격에 푸시된 태그는 수정 불가).
- **버전 불일치**: 바이너리의 버전은 `run_daemon.go` 의 `version` 값입니다. 태그와 동일하게 맞춘 뒤 릴리스하세요.
