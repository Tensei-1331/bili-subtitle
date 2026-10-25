package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bili-subtitle/internal/config"
)

func newTestServer(t *testing.T) (*Server, *config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("加载测试配置失败：%v", err)
	}
	// 提示词文件按 config.json 所在目录解析，测试里也放一份。
	promptDir := filepath.Join(dir, "prompts")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "default.md"), []byte("默认提示词"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "faithful.md"), []byte("忠实预设"), 0o644); err != nil {
		t.Fatal(err)
	}
	return New(cfg, cfgPath), cfg, cfgPath
}

func doGet(t *testing.T, h http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		q := req.URL.Query()
		q.Set("token", token)
		req.URL.RawQuery = q.Encode()
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTokenRequired(t *testing.T) {
	srv, _, _ := newTestServer(t)
	h := srv.Handler()
	if rec := doGet(t, h, "/api/state", "bad-token"); rec.Code != http.StatusForbidden {
		t.Fatalf("错误 token 应该 403，实际 %d", rec.Code)
	}
	if rec := doGet(t, h, "/", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("没有 token 打开页面应该 403，实际 %d", rec.Code)
	}
}

func TestStateAndIndex(t *testing.T) {
	srv, _, _ := newTestServer(t)
	h := srv.Handler()
	rec := doGet(t, h, "/api/state", srv.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("state 返回 %d：%s", rec.Code, rec.Body.String())
	}
	var body struct {
		OK     bool `json:"ok"`
		Prompt string
		Config struct {
			OutputDir  string `json:"outputDir"`
			Summarizer string `json:"summarizer"`
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("state 不是合法 JSON：%v", err)
	}
	if !body.OK || body.Prompt == "" || body.Config.OutputDir == "" || body.Config.Summarizer != "api" {
		t.Fatalf("state 内容不对：%+v", body)
	}

	page := doGet(t, h, "/", srv.token)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "bili-sub") {
		t.Fatalf("首页返回 %d，内容开头：%s", page.Code, page.Body.String()[:min(120, page.Body.Len())])
	}

	static := doGet(t, h, "/static/app.js", "")
	if static.Code != http.StatusOK || !strings.Contains(static.Body.String(), "btnFetch") {
		t.Fatalf("静态文件 app.js 返回 %d", static.Code)
	}
}

func TestConfigPatchPersists(t *testing.T) {
	srv, _, cfgPath := newTestServer(t)
	h := srv.Handler()
	body := strings.NewReader(`{"outputDir":"D:/tmp/x","summarizer":"opencode","keepSubtitle":true,"autoOpen":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/config?token="+srv.token, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存配置返回 %d：%s", rec.Code, rec.Body.String())
	}
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("重新加载配置失败：%v", err)
	}
	if saved.OutputDir != "D:/tmp/x" || saved.Summarizer != "opencode" || !saved.KeepSubtitle || !saved.AutoOpen {
		t.Fatalf("配置没有写到磁盘：%+v", saved)
	}
}

func TestPromptSave(t *testing.T) {
	srv, _, cfgPath := newTestServer(t)
	h := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/prompt?token="+srv.token, strings.NewReader(`{"text":"新的提示词"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存提示词返回 %d：%s", rec.Code, rec.Body.String())
	}
	cfg, _ := config.Load(cfgPath)
	data, err := os.ReadFile(cfg.Resolve(cfg.PromptFile))
	if err != nil {
		t.Fatalf("读提示词失败：%v", err)
	}
	if string(data) != "新的提示词" {
		t.Fatalf("提示词内容不对：%q", data)
	}
}
func TestPresetEndpoints(t *testing.T) {
	srv, _, _ := newTestServer(t)
	h := srv.Handler()

	rec := doGet(t, h, "/api/presets", srv.token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "faithful") || !strings.Contains(rec.Body.String(), "default") {
		t.Fatalf("预设列表不对：%d %s", rec.Code, rec.Body.String())
	}

	rec = doGet(t, h, "/api/preset?name=faithful", srv.token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "忠实预设") {
		t.Fatalf("读预设不对：%d %s", rec.Code, rec.Body.String())
	}

	body := strings.NewReader(`{"name":"自定义","text":"新预设内容"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/preset?token="+srv.token, body)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存预设失败：%d %s", rec.Code, rec.Body.String())
	}

	rec = doGet(t, h, "/api/preset?name="+url.QueryEscape("自定义"), srv.token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "新预设内容") {
		t.Fatalf("读回新预设不对：%d %s", rec.Code, rec.Body.String())
	}
}
