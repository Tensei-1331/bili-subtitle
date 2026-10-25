package note

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"bili-subtitle/internal/config"
)

func TestRender(t *testing.T) {
	dir := t.TempDir()
	templatePath := filepath.Join(dir, "note.md")
	tmpl := "---\nsource: {{source}}\nup: {{owner}}\ndate: {{date}}\nbvid: {{bvid}}\n---\n# {{title}}\n\n{{summary}}\n\n## 字幕\n{{transcript}}\n"
	if err := os.WriteFile(templatePath, []byte(tmpl), 0o644); err != nil {
		t.Fatal(err)
	}
	d := Data{
		Title:      "标题A",
		Owner:      "UP主B",
		Source:     "https://www.bilibili.com/video/BV1xx",
		BVid:       "BV1xx",
		Date:       "2026-10-26",
		Summary:    "## 概述\n\n正文内容。",
		Transcript: "原始字幕",
	}
	got, err := Render(templatePath, d)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		"source: https://www.bilibili.com/video/BV1xx",
		"up: UP主B",
		"date: 2026-10-26",
		"bvid: BV1xx",
		"# 标题A",
		"## 概述\n\n正文内容。",
		"## 字幕\n原始字幕",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("渲染结果缺少 %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "{{") {
		t.Errorf("还有没替换的占位符:\n%s", got)
	}
}

func TestRenderProjectTemplate(t *testing.T) {
	// 用仓库里真实的 templates/note.md 验一遍
	path := filepath.Join("..", "..", "templates", "note.md")
	got, err := Render(path, Data{
		Title:   "标题A",
		Owner:   "UP主B",
		Source:  "https://www.bilibili.com/video/BV1xx",
		BVid:    "BV1xx",
		Date:    "2026-10-26",
		Summary: "# 总结标题\n\n这是总结正文。",
	})
	if err != nil {
		t.Fatalf("Render 真实模板: %v", err)
	}
	// 模板只负责 frontmatter + 正文；H1 由总结正文自己带（main.ensureHeading 兜底）。
	if !strings.Contains(got, "# 总结标题") || !strings.Contains(got, "这是总结正文。") {
		t.Errorf("真实模板渲染结果不对:\n%s", got)
	}
	if !strings.HasPrefix(got, "---\ntype: 稿件\n") {
		t.Errorf("frontmatter 不对:\n%s", got)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("还有没替换的占位符:\n%s", got)
	}
}

func TestRenderErrors(t *testing.T) {
	if _, err := Render(filepath.Join(t.TempDir(), "nope.md"), Data{}); err == nil {
		t.Error("模板不存在应该报错")
	}
	empty := filepath.Join(t.TempDir(), "empty.md")
	if err := os.WriteFile(empty, []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(empty, Data{}); err == nil || !strings.Contains(err.Error(), "是空的") {
		t.Errorf("空模板应该报错, 得到 %v", err)
	}
}

func TestFileName(t *testing.T) {
	tests := []struct {
		name  string
		owner string
		title string
		want  string
	}{
		{"正常", "小明", "Go 并发模型入门", "小明-Go 并发模型入门.md"},
		{"空值回退", "", "", "未知UP主-无标题.md"},
		{"只有空白", "  ", "   ", "未知UP主-无标题.md"},
		{"非法字符替换", "a/b:c*d?e\"f<g>h|i\\j", "标题", "a_b_c_d_e_f_g_h_i_j-标题.md"},
		{"连续非法字符合并", "a//b", "c", "a_b-c.md"},
		{"结尾点和空格去掉", "UP", "标题...", "UP-标题.md"},
		{"换行压成空格", "UP", "第一行\n第二行", "UP-第一行 第二行.md"},
		{"emoji 保留", "UP😀", "标题", "UP😀-标题.md"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FileName(tc.owner, tc.title)
			if got != tc.want {
				t.Errorf("FileName(%q, %q) = %q, 想要 %q", tc.owner, tc.title, got, tc.want)
			}
			if strings.ContainsAny(got, "\\/:*?\"<>|") {
				t.Errorf("结果里还有非法字符: %q", got)
			}
		})
	}
}

func TestFileNameTooLong(t *testing.T) {
	got := FileName(strings.Repeat("长", 100), strings.Repeat("标", 300))
	if len(got) > maxFileNameBytes {
		t.Errorf("文件名 %d 字节，超过上限 %d", len(got), maxFileNameBytes)
	}
	if !strings.HasSuffix(got, ".md") {
		t.Errorf("缺 .md 后缀: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("文件名不是合法 UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n < 10 {
		t.Errorf("截得太狠了，只有 %d 个 rune: %q", n, got)
	}
}

func TestSaveSuffix(t *testing.T) {
	dir := t.TempDir()
	p1, err := Save(dir, "UP-标题.md", "第一版", "suffix")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if filepath.Dir(p1) != dir {
		t.Errorf("返回路径应该在输出目录里: %q", p1)
	}
	p2, err := Save(dir, "UP-标题.md", "第二版", "suffix")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if filepath.Base(p2) != "UP-标题 (2).md" {
		t.Errorf("第二次保存文件名 = %q, 想要 UP-标题 (2).md", filepath.Base(p2))
	}
	p3, err := Save(dir, "UP-标题.md", "第三版", "suffix")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if filepath.Base(p3) != "UP-标题 (3).md" {
		t.Errorf("第三次保存文件名 = %q, 想要 UP-标题 (3).md", filepath.Base(p3))
	}
	data, err := os.ReadFile(p2)
	if err != nil || string(data) != "第二版" {
		t.Errorf("(2) 文件内容不对: %q err=%v", data, err)
	}
}

func TestSaveOverwrite(t *testing.T) {
	dir := t.TempDir()
	p1, err := Save(dir, "note.md", "旧内容", "overwrite")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := Save(dir, "note.md", "新内容", "overwrite")
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 {
		t.Errorf("overwrite 应该写同一个文件: %q / %q", p1, p2)
	}
	data, err := os.ReadFile(p2)
	if err != nil || string(data) != "新内容" {
		t.Errorf("覆盖后内容不对: %q err=%v", data, err)
	}
}

func TestSaveSkip(t *testing.T) {
	dir := t.TempDir()
	p1, err := Save(dir, "note.md", "已有内容", "skip")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := Save(dir, "note.md", "不该写入", "skip")
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 {
		t.Errorf("skip 应该返回已有路径: %q / %q", p1, p2)
	}
	data, err := os.ReadFile(p1)
	if err != nil || string(data) != "已有内容" {
		t.Errorf("skip 不该改内容: %q err=%v", data, err)
	}
}

func TestSaveCreatesDirAndDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	// onExists 传空串按 suffix 处理
	p, err := Save(dir, "note.md", "内容", "")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("文件没写出来: %v", err)
	}
	if !filepath.IsAbs(p) {
		t.Errorf("返回路径应该是绝对路径: %q", p)
	}
}

func TestSaveErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Save(dir, "  ", "内容", "suffix"); err == nil {
		t.Error("空文件名应该报错")
	}
	if _, err := Save("  ", "note.md", "内容", "suffix"); err == nil {
		t.Error("空目录应该报错")
	}
	if _, err := Save(dir, "note.md", "内容", "append"); err == nil || !strings.Contains(err.Error(), "不支持的 onExists") {
		t.Errorf("未知 onExists 应该报错, 得到 %v", err)
	}
}

func TestOpenCommandNone(t *testing.T) {
	notePath := filepath.Join(t.TempDir(), "note.md")
	for _, mode := range []string{"none", "", "NONE"} {
		cfg := config.Default()
		cfg.OpenWith = mode
		cmd, err := openCommand(notePath, cfg)
		if err != nil {
			t.Errorf("openWith=%q 不该报错: %v", mode, err)
		}
		if cmd != nil {
			t.Errorf("openWith=%q 应该返回 nil 命令", mode)
		}
	}
	// Open 在 none 下也应该直接返回
	cfg := config.Default()
	cfg.OpenWith = "none"
	if err := Open(notePath, cfg); err != nil {
		t.Errorf("Open(none) 报错: %v", err)
	}
	if _, err := openCommand(notePath, nil); err == nil {
		t.Error("cfg 为 nil 应该报错")
	}
}

func TestOpenCommandTypora(t *testing.T) {
	dir := t.TempDir()
	// 拿一个真实存在的文件冒充 Typora.exe，只验命令构造，不启动
	fakeExe := filepath.Join(dir, "Typora.exe")
	if err := os.WriteFile(fakeExe, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(dir, "笔记.md")
	cfg := config.Default()
	cfg.OpenWith = "typora"
	cfg.TyporaPath = fakeExe

	cmd, err := openCommand(notePath, cfg)
	if err != nil {
		t.Fatalf("openCommand: %v", err)
	}
	if cmd == nil {
		t.Fatal("typora 应该返回命令")
	}
	if cmd.Path != fakeExe {
		t.Errorf("启动程序 = %q, 想要 %q", cmd.Path, fakeExe)
	}
	if len(cmd.Args) != 2 {
		t.Fatalf("参数数量 = %d: %#v", len(cmd.Args), cmd.Args)
	}
	absNote, _ := filepath.Abs(notePath)
	if cmd.Args[1] != absNote {
		t.Errorf("传给 Typora 的路径 = %q, 想要 %q", cmd.Args[1], absNote)
	}

	cfg.TyporaPath = filepath.Join(dir, "nope.exe")
	if _, err := openCommand(notePath, cfg); err == nil || !strings.Contains(err.Error(), "找不到 Typora") {
		t.Errorf("Typora 不存在应该报错, 得到 %v", err)
	}
	cfg.TyporaPath = "  "
	if _, err := openCommand(notePath, cfg); err == nil {
		t.Error("typoraPath 为空应该报错")
	}
}

func TestOpenCommandObsidian(t *testing.T) {
	notePath := filepath.Join(t.TempDir(), "我的 笔记.md")
	cfg := config.Default()
	cfg.OpenWith = "obsidian"
	cmd, err := openCommand(notePath, cfg)
	if err != nil {
		t.Fatalf("openCommand: %v", err)
	}
	if cmd == nil {
		t.Fatal("obsidian 应该返回命令")
	}
	if !strings.HasSuffix(strings.ToLower(cmd.Path), "rundll32.exe") {
		t.Errorf("启动程序 = %q, 应该用 rundll32 打开 URI", cmd.Path)
	}
	if len(cmd.Args) != 3 {
		t.Fatalf("参数数量 = %d: %#v", len(cmd.Args), cmd.Args)
	}
	if cmd.Args[1] != "url.dll,FileProtocolHandler" {
		t.Errorf("第二个参数 = %q", cmd.Args[1])
	}
	uri := cmd.Args[2]
	if !strings.HasPrefix(uri, "obsidian://open?path=") {
		t.Fatalf("URI = %q", uri)
	}
	absNote, _ := filepath.Abs(notePath)
	decoded, err := url.QueryUnescape(strings.TrimPrefix(uri, "obsidian://open?path="))
	if err != nil {
		t.Fatalf("URI 解码失败: %v", err)
	}
	if decoded != absNote {
		t.Errorf("URI 里的路径 = %q, 想要 %q", decoded, absNote)
	}
}

func TestOpenCommandUnknown(t *testing.T) {
	cfg := config.Default()
	cfg.OpenWith = "vscode"
	if _, err := openCommand(filepath.Join(t.TempDir(), "n.md"), cfg); err == nil || !strings.Contains(err.Error(), "不支持的 openWith") {
		t.Errorf("未知 openWith 应该报错, 得到 %v", err)
	}
}
