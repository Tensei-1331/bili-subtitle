package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultValues(t *testing.T) {
	c := Default()
	cases := []struct {
		name string
		got  any
		want any
	}{
		{"OutputDir", c.OutputDir, "稿件"},
		{"Summarizer", c.Summarizer, "api"},
		{"Model", c.Model, "deepseek-flash"},
		{"OpencodeModel", c.OpencodeModel, "deepseek-v4.1-flash"},
		{"OpencodeAPIKey", c.OpencodeAPIKey, ""},
		{"OpencodeBaseURL", c.OpencodeBaseURL, "https://opencode.ai/inference/go/openai/v1"},
		{"APIKey", c.APIKey, ""},
		{"BaseURL", c.BaseURL, "https://api.deepseek.com/v1"},
		{"PromptFile", c.PromptFile, "prompts/default.md"},
		{"TemplateFile", c.TemplateFile, "templates/note.md"},
		{"CookiesFile", c.CookiesFile, "data/cookies.txt"},
		{"YtDlpPath", c.YtDlpPath, ""},
		{"OpenWith", c.OpenWith, "none"},
		{"AutoOpen", c.AutoOpen, false},
		{"TyporaPath", c.TyporaPath, ""},
		{"SubLangs", strings.Join(c.SubLangs, ","), "ai-zh,zh-Hans,zh-CN,zh,ai-en,en"},
		{"KeepSubtitle", c.KeepSubtitle, false},
		{"IncludeTranscript", c.IncludeTranscript, false},
		{"OnExists", c.OnExists, "suffix"},
		{"MaxChars", c.MaxChars, 300000},
		{"APITimeoutSeconds", c.APITimeoutSeconds, 300},
		{"RequestTimeoutSec", c.RequestTimeoutSec, 30},
	}
	for _, tc := range cases {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("%s = %#v, 想要 %#v", tc.name, tc.got, tc.want)
		}
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.json")
	c := Default()
	c.APIKey = ""
	c.Model = "deepseek-v9"
	c.MaxChars = 12345
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("配置文件没写出来: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Model != "deepseek-v9" || got.MaxChars != 12345 {
		t.Errorf("往返后字段不对: model=%q maxChars=%d", got.Model, got.MaxChars)
	}
	if got.OutputDir != Default().OutputDir {
		t.Errorf("默认字段丢了: outputDir=%q", got.OutputDir)
	}
}

func TestLoadWritesDefaultWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Summarizer != "api" {
		t.Errorf("Summarizer = %q", c.Summarizer)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("首次运行没有生成 config.json: %v", err)
	}
	if !strings.Contains(string(data), "\"summarizer\": \"api\"") {
		t.Errorf("生成的 config.json 内容不对: %s", data)
	}
	if strings.Contains(string(data), "\"path\"") {
		t.Errorf("不该把内部 path 字段写进 JSON: %s", data)
	}
}

func TestLoadFillsMissingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := "{\"model\":\"custom-model\",\"summarizer\":\"opencode\"}"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Model != "custom-model" || c.Summarizer != "opencode" {
		t.Errorf("JSON 里的字段没生效: %+v", c)
	}
	if c.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("缺失字段没补默认值: baseUrl=%q", c.BaseURL)
	}
}

func TestLoadEmptyFileRewritesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.MaxChars != 300000 {
		t.Errorf("空文件应该按默认配置处理: %+v", c)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{\"model\":"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("坏 JSON 应该报错")
	} else if !strings.Contains(err.Error(), "解析配置") {
		t.Errorf("错误信息不够明确: %v", err)
	}
}

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := c.Resolve("prompts/default.md")
	want := filepath.Join(dir, "prompts", "default.md")
	if got != want {
		t.Errorf("Resolve 相对路径 = %q, 想要 %q", got, want)
	}
	abs := filepath.Join(dir, "yt-dlp.exe")
	if got := c.Resolve(abs); got != abs {
		t.Errorf("Resolve 绝对路径 = %q, 想要 %q", got, abs)
	}
	if got := c.Resolve("  "); got != "" {
		t.Errorf("空路径应该返回空串, 得到 %q", got)
	}
}

func TestResolveWithoutLoadedPath(t *testing.T) {
	c := Default()
	got := c.Resolve("data/cookies.txt")
	if !filepath.IsAbs(got) {
		t.Errorf("没 Load 过时也应该按 DefaultPath 的目录解析成绝对路径, 得到 %q", got)
	}
	if filepath.Base(got) != "cookies.txt" {
		t.Errorf("文件名不对: %q", got)
	}
}

func TestDefaultPath(t *testing.T) {
	p := DefaultPath()
	if !filepath.IsAbs(p) {
		t.Fatalf("DefaultPath 应该是绝对路径: %q", p)
	}
	if filepath.Base(p) != "config.json" {
		t.Errorf("DefaultPath = %q", p)
	}
	t.Logf("DefaultPath = %s", p)
	// go test 的 exe 在临时构建目录里，开发场景应该退回当前工作目录
	if exe, err := os.Executable(); err == nil && isTempBuildDir(filepath.Dir(exe)) {
		wd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(wd, "config.json"); p != want {
			t.Errorf("临时 exe 场景应该退回 cwd: 得到 %q, 想要 %q", p, want)
		}
	}
}

// TestLoadDefaultPathCreatesConfigFile 验证首次运行会在默认路径生成 config.json。
func TestLoadDefaultPathCreatesConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	p := DefaultPath()
	if _, err := Load(""); err != nil {
		t.Fatalf("Load(空路径): %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("首次运行没在默认路径 %q 生成 config.json: %v", p, err)
	}
}
func TestLoadSupportsComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `// 顶部注释
{
  // 行注释
  "model": "m1",
  "baseUrl": "https://example.com/v1", // 字符串里的 // 不能被当成注释
  /* 块注释
     可以多行 */
  "maxChars": 123
}
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Model != "m1" || c.MaxChars != 123 {
		t.Errorf("注释文件解析不对: model=%q maxChars=%d", c.Model, c.MaxChars)
	}
	if c.BaseURL != "https://example.com/v1" {
		t.Errorf("字符串里的 // 被误删了: %q", c.BaseURL)
	}
}

func TestSaveWritesCommentedConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c := Default()
	c.BaseURL = "https://example.com/v1"
	c.APIKey = "test-key"
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "//") {
		t.Errorf("保存的配置里没有注释: %s", data)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.BaseURL != "https://example.com/v1" || got.APIKey != "test-key" {
		t.Errorf("带注释的配置往返不对: baseUrl=%q apiKey=%q", got.BaseURL, got.APIKey)
	}
}
