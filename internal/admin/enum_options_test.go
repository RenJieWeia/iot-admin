package admin

import (
	"sort"
	"testing"

	"github.com/RenJieWeia/iot-admin/internal/exception"
)

// TestEventTypeOptionsMatchEnum 守住「后台下拉」与「后端白名单」不漂移。
//
// 异常类型是闭集：`exception.EventTypes` 决定后端接受哪些值，`eventTypeOptions`
// 决定后台下拉给用户看哪些值。两边不一致就会出现两种病：
//   - 下拉里有、后端不认 → 用户选了就 400（下线能力时最容易留下这种死选项）
//   - 后端有、下拉里没有 → 用户只能看到原始 code（terminal_unknown 曾长期如此）
func TestEventTypeOptionsMatchEnum(t *testing.T) {
	got := make([]string, 0, len(eventTypeOptions))
	for _, o := range eventTypeOptions {
		got = append(got, o.Value)
	}
	want := make([]string, 0, len(exception.EventTypes))
	for _, kind := range exception.EventTypes {
		want = append(want, string(kind))
	}
	sort.Strings(got)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("后台异常类型下拉 %d 项，后端白名单 %d 项：%v vs %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 项不一致：下拉 %q，白名单 %q（全量 %v vs %v）", i, got[i], want[i], got, want)
		}
	}
}
