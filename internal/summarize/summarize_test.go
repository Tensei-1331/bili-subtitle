package summarize

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"bili-subtitle/internal/config"
)

// fakeAPIServer 起一个假的 /chat/completions 服务，reply 决定返回内容。
func fakeAPIServer(t *testing.T, reply func(prompt string) (status int, content string)) (*httptest.Server, *[]string) {
	t.Helper()
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("请求路径不对: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var req apiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("解析请求体失败: %v", err)
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if len(req.Messages) != 1 {
			t.Errorf("messages 数量 = %d", len(req.Messages))
		}
		prompt := ""
		if len(req.Messages) > 0 {
			prompt = req.Messages[0].Content
		}
		prompts = append(prompts, prompt)
		status, content := reply(prompt)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("{\"error\":{\"message\":\"fake error\"}}"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message":       map[string]any{"content": content},
				"finish_reason": "stop",
			}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &prompts
}

// testConfig 返回一份指向假服务器的配置。
func testConfig(t *testing.T, serverURL string) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.BaseURL = serverURL
	cfg.APIKey = "test-key"
	cfg.APITimeoutSeconds = 10
	return cfg
}

func TestSplitTranscript(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxChars int
		want     []string
	}{
		{"短文本一块", "第一行\n第二行", 100, []string{"第一行\n第二行"}},
		{"按行切成两块", "aaaa\nbbbb\ncccc", 9, []string{"aaaa\nbbbb", "cccc"}},
		{"单行超长按 rune 硬切", strings.Repeat("字", 30), 10, []string{strings.Repeat("字", 10), strings.Repeat("字", 10), strings.Repeat("字", 10)}},
		{"超长行前后还有短行", "短行\n" + strings.Repeat("长", 25), 10, []string{"短行", strings.Repeat("长", 10), strings.Repeat("长", 10), strings.Repeat("长", 5)}},
		{"刚好等于阈值不切", strings.Repeat("中", 10), 10, []string{strings.Repeat("中", 10)}},
		{"CRLF 归一", "a\r\nb", 100, []string{"a\nb"}},
		{"全是空行没有块", "\n\n\n", 100, nil},
		{"空文本没有块", "", 100, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitTranscript(tc.text, tc.maxChars)
			if len(got) != len(tc.want) {
				t.Fatalf("分块数 = %d, 想要 %d: %#v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("第 %d 块 = %q, 想要 %q", i+1, got[i], tc.want[i])
				}
			}
			for i, chunk := range got {
				if n := utf8.RuneCountInString(chunk); n > tc.maxChars {
					t.Errorf("第 %d 块 %d 个 rune，超过阈值 %d", i+1, n, tc.maxChars)
				}
			}
		})
	}
}

func TestReplaceTranscript(t *testing.T) {
	transcript := "这是一段字幕"
	prompt := "前缀\n字幕文本：\n" + transcript + "\n请总结"
	got := replaceTranscript(prompt, transcript, "新的字幕内容")
	want := "前缀\n字幕文本：\n新的字幕内容\n请总结"
	if got != want {
		t.Errorf("替换结果 = %q, 想要 %q", got, want)
	}

	// prompt 里找不到原字幕时追加到末尾
	noTranscript := "没有字幕占位符的 prompt"
	got = replaceTranscript(noTranscript, transcript, "追加内容")
	if got != noTranscript+"\n\n追加内容" {
		t.Errorf("找不到字幕时的结果 = %q", got)
	}
}

func TestSummarizeSingleCall(t *testing.T) {
	srv, prompts := fakeAPIServer(t, func(prompt string) (int, string) {
		if !strings.Contains(prompt, "我的字幕") {
			t.Errorf("prompt 里没有字幕: %q", prompt)
		}
		return http.StatusOK, "总结结果"
	})
	cfg := testConfig(t, srv.URL)
	transcript := "我的字幕"
	req := Request{
		Title:      "标题",
		Owner:      "UP",
		URL:        "https://example.com",
		Transcript: transcript,
		Prompt:     "字幕文本：\n" + transcript,
	}
	out, err := Summarize(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if out != "总结结果" {
		t.Errorf("结果 = %q", out)
	}
	if len(*prompts) != 1 {
		t.Errorf("只该调一次 API, 实际 %d 次", len(*prompts))
	}
}

func TestSummarizeChunksAndMerges(t *testing.T) {
	srv, prompts := fakeAPIServer(t, func(prompt string) (int, string) {
		switch {
		case strings.Contains(prompt, "分段总结"):
			return http.StatusOK, "终稿"
		case strings.Contains(prompt, "只整理这一部分"):
			return http.StatusOK, "这一段的结果"
		default:
			t.Errorf("没见过的 prompt: %q", truncateForTest(prompt, 100))
			return http.StatusOK, "意外"
		}
	})
	cfg := testConfig(t, srv.URL)
	cfg.MaxChars = 45

	line := "这是一段测试字幕内容，用来验证分段逻辑。"
	transcript := strings.Join([]string{line, line, line, line, line}, "\n")
	if n := utf8.RuneCountInString(transcript); n <= cfg.MaxChars {
		t.Fatalf("测试字幕只有 %d 个 rune，没触发分段", n)
	}
	req := Request{
		Title:      "标题",
		Owner:      "UP",
		URL:        "https://example.com",
		Transcript: transcript,
		Prompt:     "请总结下面的字幕：\n\n" + transcript + "\n\n只输出正文。",
	}
	out, err := Summarize(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if out != "终稿" {
		t.Errorf("最终结果 = %q, 想要合并后的终稿", out)
	}
	if len(*prompts) != 4 {
		t.Fatalf("API 调用次数 = %d, 想要 3 段 + 1 次合并 = 4", len(*prompts))
	}
	// 分段 prompt 里要有「第 i/n 部分」提示，并且不能带完整字幕
	for i := 0; i < 3; i++ {
		partMark := "第 " + string(rune('1'+i)) + "/3 部分"
		if !strings.Contains((*prompts)[i], partMark) {
			t.Errorf("第 %d 次调用缺少分段提示 %q: %q", i+1, partMark, truncateForTest((*prompts)[i], 120))
		}
		if strings.Contains((*prompts)[i], transcript) {
			t.Errorf("分段 prompt 里不该再带完整字幕")
		}
	}
	if !strings.Contains((*prompts)[3], "各部分的要点") {
		t.Errorf("合并 prompt 里没有各段要点: %q", truncateForTest((*prompts)[3], 120))
	}
	if !strings.Contains((*prompts)[3], "这一段的结果") {
		t.Errorf("合并 prompt 里没有分段结果")
	}
}

func TestSummarizeErrors(t *testing.T) {
	cfg := config.Default()
	cfg.APIKey = "test-key"

	if _, err := Summarize(context.Background(), nil, Request{}); err == nil {
		t.Error("cfg 为 nil 应该报错")
	}
	if _, err := Summarize(context.Background(), cfg, Request{Transcript: "  ", Prompt: "x"}); err == nil {
		t.Error("空字幕应该报错")
	}
	if _, err := Summarize(context.Background(), cfg, Request{Transcript: "字幕", Prompt: "  "}); err == nil {
		t.Error("空 prompt 应该报错")
	}
	badCfg := config.Default()
	badCfg.Summarizer = "chatgpt"
	if _, err := Summarize(context.Background(), badCfg, Request{Transcript: "字幕", Prompt: "p"}); err == nil {
		t.Error("未知后端应该报错")
	} else if !strings.Contains(err.Error(), "不支持的 summarizer") {
		t.Errorf("错误信息不明确: %v", err)
	}
}

func TestAPIModelAndAuthHeader(t *testing.T) {
	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req apiRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}"))
	}))
	defer srv.Close()
	cfg := testConfig(t, srv.URL)
	if _, err := callAPI(context.Background(), cfg, "hi"); err != nil {
		t.Fatalf("callAPI: %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotModel != "deepseek-flash" {
		t.Errorf("model = %q", gotModel)
	}
}

func TestAPIRetryOn429And5xx(t *testing.T) {
	old := apiRetryBaseDelay
	apiRetryBaseDelay = 5 * time.Millisecond
	defer func() { apiRetryBaseDelay = old }()

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
		case 2:
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("bad gateway"))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"重试成功\"},\"finish_reason\":\"stop\"}]}"))
		}
	}))
	defer srv.Close()
	cfg := testConfig(t, srv.URL)
	out, err := callAPI(context.Background(), cfg, "hi")
	if err != nil {
		t.Fatalf("callAPI: %v", err)
	}
	if out != "重试成功" {
		t.Errorf("结果 = %q", out)
	}
	if calls != 3 {
		t.Errorf("调用次数 = %d, 想要 1 次失败 + 2 次重试 = 3", calls)
	}
}

func TestAPIRetryExhausted(t *testing.T) {
	old := apiRetryBaseDelay
	apiRetryBaseDelay = 5 * time.Millisecond
	defer func() { apiRetryBaseDelay = old }()

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("down"))
	}))
	defer srv.Close()
	cfg := testConfig(t, srv.URL)
	if _, err := callAPI(context.Background(), cfg, "hi"); err == nil {
		t.Fatal("一直 5xx 应该报错")
	} else if !strings.Contains(err.Error(), "共尝试 3 次") {
		t.Errorf("错误信息应该说明尝试次数: %v", err)
	}
	if calls != 3 {
		t.Errorf("调用次数 = %d, 想要 3", calls)
	}
}

func TestAPI4xxNoRetry(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("{\"error\":{\"message\":\"bad param\"}}"))
	}))
	defer srv.Close()
	cfg := testConfig(t, srv.URL)
	_, err := callAPI(context.Background(), cfg, "hi")
	if err == nil {
		t.Fatal("400 应该报错")
	}
	if calls != 1 {
		t.Errorf("400 不该重试, 调用次数 = %d", calls)
	}
}

func TestAPIReturnsEmptyContent(t *testing.T) {
	srv, _ := fakeAPIServer(t, func(string) (int, string) { return http.StatusOK, "" })
	cfg := testConfig(t, srv.URL)
	_, err := callAPI(context.Background(), cfg, "hi")
	if err == nil || !strings.Contains(err.Error(), "空内容") {
		t.Errorf("空内容应该报错, 得到 %v", err)
	}
}

func TestResolveAPIKeyOrder(t *testing.T) {
	oldAuth := opencodeAuthPath
	defer func() { opencodeAuthPath = oldAuth }()

	authFile := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authFile, []byte("{\"deepseek\":{\"type\":\"api\",\"key\":\"auth-key\"}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	opencodeAuthPath = authFile
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	cfg := config.Default()
	cfg.APIKey = "cfg-key"
	if got, err := resolveAPIKey(cfg); err != nil || got != "cfg-key" {
		t.Errorf("第一优先级 cfg.APIKey: got=%q err=%v", got, err)
	}

	cfg.APIKey = ""
	t.Setenv("DEEPSEEK_API_KEY", "env-key")
	if got, err := resolveAPIKey(cfg); err != nil || got != "env-key" {
		t.Errorf("第二优先级环境变量: got=%q err=%v", got, err)
	}

	t.Setenv("DEEPSEEK_API_KEY", "")
	if got, err := resolveAPIKey(cfg); err != nil || got != "auth-key" {
		t.Errorf("第三优先级 auth.json: got=%q err=%v", got, err)
	}

	opencodeAuthPath = filepath.Join(t.TempDir(), "missing.json")
	if _, err := resolveAPIKey(cfg); err == nil {
		t.Error("三个来源都没有时应该报错")
	} else if !strings.Contains(err.Error(), "找不到 API key") {
		t.Errorf("错误信息不明确: %v", err)
	}
}

func TestReadOpencodeAuthKeyBadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte("不是 JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readOpencodeAuthKey(path); got != "" {
		t.Errorf("坏 JSON 应该返回空串, 得到 %q", got)
	}
	if got := readOpencodeAuthKey(filepath.Join(t.TempDir(), "nope.json")); got != "" {
		t.Errorf("文件不存在应该返回空串, 得到 %q", got)
	}
}

func TestRequestTimeout(t *testing.T) {
	cfg := config.Default()
	if got := requestTimeout(cfg); got != 300*time.Second {
		t.Errorf("默认超时 = %v", got)
	}
	cfg.APITimeoutSeconds = 7
	if got := requestTimeout(cfg); got != 7*time.Second {
		t.Errorf("超时 = %v", got)
	}
	cfg.APITimeoutSeconds = 0
	if got := requestTimeout(cfg); got != 300*time.Second {
		t.Errorf("0 应该回退默认值, 得到 %v", got)
	}
}

// truncateForTest 截短测试日志里的长字符串。
func truncateForTest(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
