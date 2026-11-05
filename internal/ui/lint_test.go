package ui

import (
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// 设计令牌 lint：颜色字面量只允许出现在 :root 令牌块，间距只用 4 的倍数。
//
// 验收 §9：app.css 中 :root 之外无颜色字面量。
func TestCSSTokensOnlyInRoot(t *testing.T) {
	raw, err := fs.ReadFile(staticFS, "static/app.css")
	if err != nil {
		t.Fatalf("读取 app.css: %v", err)
	}
	css := string(raw)
	rootStart := strings.Index(css, ":root")
	if rootStart < 0 {
		t.Fatal("app.css 缺少 :root 令牌块")
	}
	rootEnd := strings.Index(css[rootStart:], "}")
	if rootEnd < 0 {
		t.Fatal(":root 令牌块未闭合")
	}
	rootBlock := css[rootStart : rootStart+rootEnd+1]
	rest := css[:rootStart] + css[rootStart+rootEnd+1:]

	// 1) 令牌块本身要声明颜色与间距变量
	for _, token := range []string{"--color-text", "--color-primary", "--space-1", "--space-8"} {
		if !strings.Contains(rootBlock, token) {
			t.Fatalf(":root 缺少令牌 %s", token)
		}
	}

	// 2) 令牌块之外不得出现颜色字面量
	colorLiteral := regexp.MustCompile(`(?i)#[0-9a-f]{3,8}\b|rgba?\(|hsla?\(`)
	for _, line := range strings.Split(rest, "\n") {
		code := strings.TrimSpace(line)
		if code == "" || strings.HasPrefix(code, "/*") || strings.HasPrefix(code, "*") {
			continue
		}
		if loc := colorLiteral.FindString(code); loc != "" {
			t.Fatalf(":root 之外出现颜色字面量 %q（行：%s）", loc, code)
		}
	}

	// 3) 间距只用 4 的倍数（令牌与 0 除外）
	spacing := regexp.MustCompile(`(?m)^\s*(padding|margin|gap)(?:-(?:top|right|bottom|left))?\s*:\s*([^;]+);`)
	px := regexp.MustCompile(`(\d+)px`)
	for _, m := range spacing.FindAllStringSubmatch(css, -1) {
		for _, n := range px.FindAllStringSubmatch(m[2], -1) {
			v, err := strconv.Atoi(n[1])
			if err != nil {
				continue
			}
			if v%4 != 0 {
				t.Fatalf("%s 用了非 4 倍数间距 %dpx（%s）", m[1], v, strings.TrimSpace(m[0]))
			}
		}
	}

	// 4) 令牌值本身也必须是 4 的倍数
	spaceToken := regexp.MustCompile(`--space-\d+:\s*(\d+)px`)
	for _, m := range spaceToken.FindAllStringSubmatch(rootBlock, -1) {
		v, _ := strconv.Atoi(m[1])
		if v%4 != 0 {
			t.Fatalf("间距令牌用了非 4 倍数: %spx", m[1])
		}
	}
}

// 模板不得把用户数据包进 template.HTML（保留 html/template 默认转义）。
func TestTemplatesAvoidUnsafeHTML(t *testing.T) {
	err := fs.WalkDir(templateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(templateFS, path)
		if err != nil {
			return err
		}
		text := string(body)
		for _, bad := range []string{"template.HTML", "template.URL", "template.JS", "template.CSS", "safeHTML"} {
			if strings.Contains(text, bad) {
				t.Errorf("%s 使用了 %s，会绕过默认转义", path, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历模板: %v", err)
	}
}

// 模板必须用语义化表格与可访问属性（scope / label / role=dialog + aria-modal）。
func TestTemplateAccessibilityMarkers(t *testing.T) {
	all, err := fs.ReadFile(templateFS, "templates/list.gohtml")
	if err != nil {
		t.Fatalf("读取 list.gohtml: %v", err)
	}
	text := string(all)
	for _, want := range []string{`<table class="grid">`, `scope="col"`, `<label for=`, `role="dialog"`, `aria-modal="true"`, `role="alertdialog"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("模板缺少可访问性标记 %q", want)
		}
	}
}
