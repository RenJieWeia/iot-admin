package admin

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/migrations"
)

// exception_count 的 CHECK 在 0012 之后是「当前 EventTypes + 历史值」的并集：
//
//   - 历史上的 upload_timeout / snapshot_failed / late_data 保留在 CHECK 里，是为了让
//     存量行仍然合法（直接排除会让迁移在存量校验时失败，删行又属于数据删除）；
//   - 写侧以 Go 的 exception.EventTypes 为准，Recorder 不会产生这三个值。
//
// 所以断言是 **包含**（EventTypes ⊆ CHECK），不是集合相等——相等会因历史值而红。
func TestEventTypeCheckCoversEventTypes(t *testing.T) {
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatalf("读取迁移目录: %v", err)
	}
	sort.Strings(files)

	var latest string
	for _, name := range files {
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatalf("读取 %s: %v", name, err)
		}
		if strings.Contains(string(body), "exception_count_error_type_check") {
			latest = string(body) // 取最后一次定义约束的迁移
		}
	}
	if latest == "" {
		t.Fatal("迁移里找不到 exception_count_error_type_check 的定义")
	}

	idx := strings.LastIndex(latest, "exception_count_error_type_check")
	block := latest[idx:]
	open := strings.Index(block, " in (")
	if open < 0 {
		t.Fatalf("约束定义里找不到 in ( 列表: %s", block[:min(len(block), 120)])
	}
	rest := block[open+len(" in ("):]
	end := strings.Index(rest, "));")
	if end < 0 {
		t.Fatalf("约束定义没有闭合: %s", rest[:min(len(rest), 120)])
	}
	allowed := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(rest[:end], -1) {
		allowed[m[1]] = true
	}
	if len(allowed) == 0 {
		t.Fatal("CHECK 列表解析为空，无法对账")
	}

	for _, kind := range exception.EventTypes {
		if !allowed[string(kind)] {
			t.Fatalf("EventTypes 的 %q 不在 exception_count 的 CHECK 里——新增事件类型必须同步补迁移", kind)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
