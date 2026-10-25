package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bili-subtitle/internal/config"
)

const (
	defaultBaseURL = "https://api.deepseek.com/v1"
	defaultModel   = "deepseek-flash"
	// apiRetryTimes 是 429/5xx 的重试次数：首次之外再试 2 次。
	apiRetryTimes = 2
	// maxAPIResponseBytes 限制一次响应最多读多少，防止异常响应把内存吃满。
	maxAPIResponseBytes = 32 << 20
)

var (
	apiClient = &http.Client{}
	// apiRetryBaseDelay 是重试等待的基数，测试里会调小。
	apiRetryBaseDelay = time.Second
	// opencodeAuthPath 是 opencode 保存登录凭据的路径，测试里会替换。
	opencodeAuthPath = defaultOpencodeAuthPath()
)

type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type apiRequest struct {
	Model    string       `json:"model"`
	Messages []apiMessage `json:"messages"`
	Stream   bool         `json:"stream"`
}

type apiResponse struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// callAPI 走 OpenAI 兼容的 /chat/completions 接口，429/5xx 自动重试 2 次。
func callAPI(ctx context.Context, cfg *config.Config, prompt string) (string, error) {
	key, err := resolveAPIKey(cfg)
	if err != nil {
		return "", err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultModel
	}
	payload, err := json.Marshal(apiRequest{
		Model:    model,
		Messages: []apiMessage{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", fmt.Errorf("构造 API 请求体失败: %w", err)
	}
	endpoint := baseURL + "/chat/completions"

	var lastErr error
	var lastRetryAfter time.Duration
	attempts := 0
	for attempt := 0; attempt <= apiRetryTimes; attempt++ {
		if attempt > 0 {
			delay := apiRetryBaseDelay * time.Duration(attempt)
			if lastRetryAfter > delay {
				delay = lastRetryAfter
			}
			if err := sleepCtx(ctx, delay); err != nil {
				return "", fmt.Errorf("等待重试时被取消: %w", err)
			}
		}
		attempts++
		content, retryable, retryAfter, err := doAPIOnce(ctx, cfg, endpoint, key, payload)
		if err == nil {
			return content, nil
		}
		lastErr, lastRetryAfter = err, retryAfter
		if !retryable {
			break
		}
	}
	return "", fmt.Errorf("API 请求失败（共尝试 %d 次）: %w", attempts, lastErr)
}

// doAPIOnce 发一次请求。retryable 表示这次失败要不要重试（429 或 5xx）。
func doAPIOnce(ctx context.Context, cfg *config.Config, endpoint, key string, payload []byte) (content string, retryable bool, retryAfter time.Duration, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout(cfg))
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", false, 0, fmt.Errorf("构造 API 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := apiClient.Do(req)
	if err != nil {
		return "", false, 0, fmt.Errorf("请求 %s 失败: %w", endpoint, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponseBytes))
	if err != nil {
		return "", false, 0, fmt.Errorf("读取 API 响应失败: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return "", true, parseRetryAfter(resp.Header.Get("Retry-After")), fmt.Errorf("API 返回 %d: %s", resp.StatusCode, snippet(body))
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, 0, fmt.Errorf("API 返回 %d: %s", resp.StatusCode, snippet(body))
	}

	var parsed apiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", false, 0, fmt.Errorf("解析 API 响应失败: %w（响应片段：%s）", err, snippet(body))
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", false, 0, fmt.Errorf("API 报错: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", false, 0, fmt.Errorf("API 响应里没有 choices：%s", snippet(body))
	}
	content = strings.TrimSpace(parsed.Choices[0].Message.Content)
	if content == "" {
		return "", false, 0, fmt.Errorf("API 返回了空内容（finish_reason=%s）", parsed.Choices[0].FinishReason)
	}
	return content, false, 0, nil
}

// resolveAPIKey 按 cfg.APIKey -> env DEEPSEEK_API_KEY -> opencode auth.json 的顺序找 key。
func resolveAPIKey(cfg *config.Config) (string, error) {
	if cfg != nil {
		if key := strings.TrimSpace(cfg.APIKey); key != "" {
			return key, nil
		}
	}
	if key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")); key != "" {
		return key, nil
	}
	if opencodeAuthPath != "" {
		if key := readOpencodeAuthKey(opencodeAuthPath); key != "" {
			return key, nil
		}
	}
	return "", fmt.Errorf("找不到 DeepSeek API key：config.json 的 apiKey 为空，环境变量 DEEPSEEK_API_KEY 没设置，也没从 %s 读到 deepseek.key", opencodeAuthPath)
}

// readOpencodeAuthKey 从 opencode 的 auth.json 里读 deepseek.key，读不到返回空串。
func readOpencodeAuthKey(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var auth map[string]struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(data, &auth); err != nil {
		return ""
	}
	return strings.TrimSpace(auth["deepseek"].Key)
}

// defaultOpencodeAuthPath 返回 %USERPROFILE%.localshareopencodeauth.json。
func defaultOpencodeAuthPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json")
}

// requestTimeout 返回单次请求的超时时间。
func requestTimeout(cfg *config.Config) time.Duration {
	sec := 300
	if cfg != nil && cfg.APITimeoutSeconds > 0 {
		sec = cfg.APITimeoutSeconds
	}
	return time.Duration(sec) * time.Second
}

// parseRetryAfter 解析 Retry-After 头（秒数），最多等 30 秒。
func parseRetryAfter(value string) time.Duration {
	sec, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || sec <= 0 {
		return 0
	}
	if sec > 30 {
		sec = 30
	}
	return time.Duration(sec) * time.Second
}

// sleepCtx 等待 d，ctx 取消时提前返回。
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// snippet 把响应片段压成一行、截短，方便塞进错误信息。
func snippet(data []byte) string {
	s := strings.Join(strings.Fields(string(data)), " ")
	if s == "" {
		return "(空响应)"
	}
	runes := []rune(s)
	if len(runes) > 300 {
		return string(runes[:300]) + "…"
	}
	return s
}
