package summarize

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"

	"bili-subtitle/internal/config"
)

const (
	// OpenCode Go 的 OpenAI 兼容网关（Go 套餐）。
	defaultOpencodeBaseURL = "https://opencode.ai/inference/go/openai/v1"
	defaultOpencodeModel   = "deepseek-v4.1-flash"
)

// newOpencodeSession 生成 Go 网关要求的 x-opencode-session。
// 同一次总结的多段请求复用同一个 session。
func newOpencodeSession() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "ses_bilisub"
	}
	return "ses_bilisub_" + hex.EncodeToString(b[:])
}

// callOpencode 调用 OpenCode Go 的 API，走 OpenAI 兼容的 /chat/completions。
func callOpencode(ctx context.Context, cfg *config.Config, prompt, session string) (string, error) {
	key := strings.TrimSpace(cfg.OpencodeAPIKey)
	if key == "" {
		key = strings.TrimSpace(os.Getenv("OPENCODE_GO_API_KEY"))
	}
	if key == "" {
		key = strings.TrimSpace(os.Getenv("OPENCODE_API_KEY"))
	}
	if key == "" {
		return "", errors.New("找不到 OpenCode Go API key：在 config.json 填 opencodeApiKey，或设置环境变量 OPENCODE_GO_API_KEY")
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.OpencodeBaseURL), "/")
	if base == "" {
		base = defaultOpencodeBaseURL
	}
	headers := map[string]string{
		"x-opencode-session": session,
		"User-Agent":         "bili-sub",
	}
	return callChatEndpoint(ctx, cfg, base+"/chat/completions", key, normalizeOpencodeModel(cfg.OpencodeModel), prompt, headers)
}

// normalizeOpencodeModel 去掉 opencode/、opencode-go/ 这类 provider 前缀。
func normalizeOpencodeModel(model string) string {
	m := strings.TrimSpace(model)
	if m == "" {
		return defaultOpencodeModel
	}
	for _, prefix := range []string{"opencode-go/", "opencode/"} {
		if strings.HasPrefix(m, prefix) && len(m) > len(prefix) {
			return strings.TrimPrefix(m, prefix)
		}
	}
	return m
}
