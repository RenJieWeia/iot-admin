# 本地/CI 通用护栏（不绑定具体 CI 平台）。
# 说明：
#   - 所有目标只做校验，**不执行任何 git 写操作**（不 add / 不 commit）。
#   - 若构建缓存在只读路径，可先 `export GOCACHE=/tmp/gocache` 再调用 go-check。
.PHONY: web-check web-build dist-check go-check check

# 前端静态检查 + 单测
web-check:
	cd web && npx tsc --noEmit && npx stylelint "src/**/*.css" && npx vitest run

# 前端构建（产物写入 internal/ui/dist）
web-build:
	cd web && npm run build

# 产物漂移校验：重新构建后 dist 必须与仓库中已提交的产物一致。
# 干净树返回 0；改了 web/src 却未重建 → 非 0。
dist-check: web-build
	git diff --exit-code internal/ui/dist

# Go 构建 + 静态检查 + 单测
go-check:
	go build ./... && go vet ./... && go test ./...

# 提交前全量校验
check: web-check go-check
