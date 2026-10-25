package summarize

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bili-subtitle/internal/config"
)

func TestCallOpencodeAPI(t *testing.T) {
	var gotPath, gotAuth, gotSession, gotModel, gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotSession = r.Header.Get("x-opencode-session")
		var req struct {
			Model    string
			Messages []struct{ Content string }
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model
		if len(req.Messages) > 0 {
			gotPrompt = req.Messages[0].Content
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"FAKE_OK"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.OpencodeBaseURL = srv.URL
	cfg.OpencodeAPIKey = "test-key"
	cfg.APITimeoutSeconds = 10

	out, err := callOpencode(context.Background(), cfg, "测试 prompt", "ses_test")
	if err != nil {
		t.Fatalf("callOpencode: %v", err)
	}
	if out != "FAKE_OK" {
		t.Errorf("结果 = %q", out)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("请求路径 = %q", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotSession != "ses_test" {
		t.Errorf("x-opencode-session = %q", gotSession)
	}
	if gotModel != "deepseek-v4.1-flash" {
		t.Errorf("模型 = %q", gotModel)
	}
	if !strings.Contains(gotPrompt, "测试 prompt") {
		t.Errorf("prompt 没传过去: %q", gotPrompt)
	}
}

func TestCallOpencodeUsesEnvKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer env-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"OK"}}]}`)
	}))
	defer srv.Close()

	t.Setenv("OPENCODE_GO_API_KEY", "env-key")
	cfg := config.Default()
	cfg.OpencodeBaseURL = srv.URL
	cfg.OpencodeAPIKey = ""

	if _, err := callOpencode(context.Background(), cfg, "p", "ses_x"); err != nil {
		t.Fatalf("callOpencode: %v", err)
	}
}

func TestCallOpencodeMissingKey(t *testing.T) {
	t.Setenv("OPENCODE_GO_API_KEY", "")
	cfg := config.Default()
	if _, err := callOpencode(context.Background(), cfg, "p", "ses_x"); err == nil || !strings.Contains(err.Error(), "OPENCODE_GO_API_KEY") {
		t.Fatalf("缺 key 时应该报错，得到 %v", err)
	}
}

func TestNormalizeOpencodeModel(t *testing.T) {
	cases := map[string]string{
		"":                                  "deepseek-v4.1-flash",
		"deepseek-v4.1-flash":               "deepseek-v4.1-flash",
		"opencode-go/deepseek-v4-pro":       "deepseek-v4-pro",
		"opencode/deepseek-v4.1-flash-free": "deepseek-v4.1-flash-free",
	}
	for in, want := range cases {
		if got := normalizeOpencodeModel(in); got != want {
			t.Errorf("normalizeOpencodeModel(%q) = %q, 想要 %q", in, got, want)
		}
	}
}