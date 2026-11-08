# 本地/CI 通用护栏（不绑定具体 CI 平台）。
# 说明：
#   - 只做校验与构建，**不执行任何 git 写操作**（不 add / 不 commit）。
#   - 若构建缓存在只读路径，可先 `export GOCACHE=/tmp/gocache` 再调用 go-check。
.PHONY: go-check web-check web-build dist-check check

WEB_DIR := web
DIST_DIR := internal/ui/dist

# Go 构建 + 静态检查 + 单测
go-check:
	go build ./... && go vet ./... && go test ./...

# 前端类型检查（Node 22+；首次会装依赖，不新增包）
web-check:
	cd $(WEB_DIR) && npm install --no-audit --no-fund && npx tsc --noEmit

# 前端生产构建：vite 直接把产物写进 internal/ui/dist，供 go:embed 打进单二进制
web-build:
	cd $(WEB_DIR) && npm run build

# 单二进制前确认 embed 目录确实有产物（占位 index.html 也算，真实构建会覆盖）
dist-check:
	@test -f $(DIST_DIR)/index.html || { echo "缺少 $(DIST_DIR)/index.html：先 make web-build"; exit 1; }

# 提交前全量校验
check: web-check go-check
