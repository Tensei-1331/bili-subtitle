// Package summarize 把字幕整理成稿件：支持 OpenAI 兼容 API 和 OpenCode Go 两种方式，
// 字幕太长时先分段总结再合并。
package summarize

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"bili-subtitle/internal/config"
)

// defaultMaxChars 是没配置 maxChars 时的分段阈值。
const defaultMaxChars = 300000

// Request 是一次总结任务需要的全部信息。
type Request struct {
	Title, Owner, URL string
	DurationSeconds   int
	Transcript        string
	Prompt            string // 已渲染好占位符的完整 prompt（含字幕）
}

// backendFunc 是一次具体的模型调用。
type backendFunc func(ctx context.Context, prompt string) (string, error)

// Summarize 按 cfg.Summarizer 选后端，把字幕整理成稿件。
// 字幕 rune 数超过 cfg.MaxChars 时按行切块，逐块总结后再合并成终稿。
func Summarize(ctx context.Context, cfg *config.Config, req Request) (string, error) {
	if cfg == nil {
		return "", errors.New("配置为空")
	}
	if strings.TrimSpace(req.Transcript) == "" {
		return "", errors.New("字幕是空的，没法总结")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return "", errors.New("prompt 是空的：调用方要先渲染好带字幕的完整 prompt")
	}
	call, err := backendFor(cfg)
	if err != nil {
		return "", err
	}
	maxChars := cfg.MaxChars
	if maxChars <= 0 {
		maxChars = defaultMaxChars
	}
	if utf8.RuneCountInString(req.Transcript) <= maxChars {
		out, err := call(ctx, req.Prompt)
		if err != nil {
			return "", err
		}
		return requireText(out)
	}
	return summarizeInChunks(ctx, req, maxChars, call)
}

// backendFor 根据配置选后端。
func backendFor(cfg *config.Config) (backendFunc, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Summarizer)) {
	case "", "api":
		return func(ctx context.Context, prompt string) (string, error) {
			return callAPI(ctx, cfg, prompt)
		}, nil
	case "opencode":
		session := newOpencodeSession()
		return func(ctx context.Context, prompt string) (string, error) {
			return callOpencode(ctx, cfg, prompt, session)
		}, nil
	default:
		return nil, fmt.Errorf("不支持的 summarizer %q：只能填 api 或 opencode", cfg.Summarizer)
	}
}

// summarizeInChunks 先逐块总结字幕，再让模型把各块结果合并成终稿。
func summarizeInChunks(ctx context.Context, req Request, maxChars int, call backendFunc) (string, error) {
	chunks := splitTranscript(req.Transcript, maxChars)
	if len(chunks) <= 1 {
		out, err := call(ctx, req.Prompt)
		if err != nil {
			return "", err
		}
		return requireText(out)
	}

	sections := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		notice := fmt.Sprintf("（注意：这是完整字幕的第 %d/%d 部分，只整理这一部分，不要写总结性结尾）\n%s", i+1, len(chunks), chunk)
		chunkPrompt := replaceTranscript(req.Prompt, req.Transcript, notice)
		out, err := call(ctx, chunkPrompt)
		if err != nil {
			return "", fmt.Errorf("第 %d/%d 部分总结失败: %w", i+1, len(chunks), err)
		}
		sections = append(sections, out)
	}

	var b strings.Builder
	for i, s := range sections {
		fmt.Fprintf(&b, "第 %d/%d 部分要点：\n%s\n\n", i+1, len(sections), s)
	}
	mergeText := "上面这份字幕太长，已经按顺序分段总结。以下是各部分的要点，请把它们合并成一篇完整的稿件：" +
		"按主题重新组织结构，去掉重复内容，保留关键人名、作品名、概念和例子，不要编造字幕里没有的内容。" +
		"只输出最终的 Markdown 正文。\n\n" + b.String()
	mergePrompt := replaceTranscript(req.Prompt, req.Transcript, mergeText)
	out, err := call(ctx, mergePrompt)
	if err != nil {
		return "", fmt.Errorf("合并分段总结失败: %w", err)
	}
	return requireText(out)
}

// requireText 检查模型有没有返回有效文本。
func requireText(out string) (string, error) {
	out = strings.TrimSpace(out)
	if out == "" {
		return "", errors.New("模型返回了空内容")
	}
	return out, nil
}

// replaceTranscript 把完整 prompt 里的字幕替换成 replacement。
// prompt 里找不到原字幕时（比如自定义 prompt 没带字幕），把 replacement 追加到末尾。
func replaceTranscript(prompt, transcript, replacement string) string {
	if transcript == "" {
		return prompt + "\n\n" + replacement
	}
	if idx := strings.LastIndex(prompt, transcript); idx >= 0 {
		return prompt[:idx] + replacement + prompt[idx+len(transcript):]
	}
	return prompt + "\n\n" + replacement
}

// splitTranscript 按行把字幕切成不超过 maxChars 个 rune 的块。
// 单行超长时再按 rune 硬切。切出来的块会去掉首尾空白，空块丢弃。
func splitTranscript(text string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = defaultMaxChars
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")

	var chunks []string
	var cur []string
	curRunes := 0

	flush := func() {
		if len(cur) == 0 {
			return
		}
		chunk := strings.TrimSpace(strings.Join(cur, "\n"))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		cur = nil
		curRunes = 0
	}

	for _, line := range lines {
		n := utf8.RuneCountInString(line)
		if n > maxChars {
			flush()
			for _, piece := range splitLongLine(line, maxChars) {
				if piece = strings.TrimSpace(piece); piece != "" {
					chunks = append(chunks, piece)
				}
			}
			continue
		}
		add := n
		if len(cur) > 0 {
			add++ // 拼接时的换行符
		}
		if curRunes+add > maxChars {
			flush()
			add = n
		}
		if len(cur) == 0 && n == 0 {
			continue // 跳过块开头的空行
		}
		cur = append(cur, line)
		curRunes += add
	}
	flush()

	if len(chunks) == 0 {
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			chunks = append(chunks, trimmed)
		}
	}
	return chunks
}

// splitLongLine 把一行超长文本按 rune 切成若干段。
func splitLongLine(line string, maxChars int) []string {
	if maxChars <= 0 {
		return []string{line}
	}
	runes := []rune(line)
	var out []string
	for len(runes) > maxChars {
		out = append(out, string(runes[:maxChars]))
		runes = runes[maxChars:]
	}
	if len(runes) > 0 {
		out = append(out, string(runes))
	}
	return out
}
