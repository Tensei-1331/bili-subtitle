package bili

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Input 是解析后的用户输入。
type Input struct {
	Raw  string // 原始输入
	BVid string // BV 号（可能为空，若用户给的是 av 号）
	AID  string // av 号数字（可能为空）
	Page int    // 分 P，从 1 开始
}

var (
	// BV 号固定是 BV + 10 位 base58 字符，大小写敏感，不能整体转大小写。
	bvPattern     = regexp.MustCompile(`[Bb][Vv]([0-9A-Za-z]{10})`)
	bvOnlyPattern = regexp.MustCompile(`^[Bb][Vv][0-9A-Za-z]{10}$`)
	avOnlyPattern = regexp.MustCompile(`^[Aa][Vv](\d+)$`)
	// 前面不能紧挨字母数字，避免把 "nav123" 里的 av123 当成 av 号。
	avPattern = regexp.MustCompile(`(^|[^0-9A-Za-z])[Aa][Vv](\d+)`)
)

// shortLinkHosts 是 B 站分享出来的短链域名，必须跟 302 才知道最终视频。
var shortLinkHosts = map[string]bool{
	"b23.tv":      true,
	"bili2233.cn": true,
}

// parsedInput 是 parseInput 的结果；ShortURL 非空表示还得跟一次跳转。
type parsedInput struct {
	Input    *Input
	ShortURL string
}

// ResolveInput 解析 BV 号 / av 号 / 完整链接 / b23.tv 短链；短链会跟随后拿最终 BV。
func ResolveInput(ctx context.Context, raw string) (*Input, error) {
	parsed, err := parseInput(raw)
	if err != nil {
		return nil, err
	}
	if parsed.ShortURL == "" {
		return parsed.Input, nil
	}

	finalURL, err := followShortLink(ctx, parsed.ShortURL)
	if err != nil {
		return nil, err
	}
	input, err := parseVideoURL(raw, finalURL)
	if err != nil {
		return nil, fmt.Errorf("短链 %s 跳转到了 %s，但这不是视频页：%w", parsed.ShortURL, finalURL.String(), err)
	}
	return input, nil
}

// parseInput 只做本地解析，短链不联网，交给 ResolveInput 跟跳转。
func parseInput(raw string) (*parsedInput, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("输入为空：请粘贴 BV 号、av 号或视频链接")
	}

	if bvOnlyPattern.MatchString(trimmed) {
		return &parsedInput{Input: &Input{Raw: raw, BVid: "BV" + trimmed[2:], Page: 1}}, nil
	}
	if match := avOnlyPattern.FindStringSubmatch(trimmed); match != nil {
		return &parsedInput{Input: &Input{Raw: raw, AID: match[1], Page: 1}}, nil
	}

	// 用户可能漏写协议，补上再当 URL 解析。
	candidate := trimmed
	if !strings.Contains(candidate, "://") && looksLikeHost(candidate) {
		candidate = "https://" + candidate
	}
	if parsedURL, err := url.Parse(candidate); err == nil && parsedURL.Host != "" {
		host := strings.ToLower(parsedURL.Hostname())
		if isShortLinkHost(host) {
			return &parsedInput{Input: &Input{Raw: raw, Page: 1}, ShortURL: parsedURL.String()}, nil
		}
		if isBilibiliHost(host) {
			input, err := parseVideoURL(raw, parsedURL)
			if err != nil {
				return nil, err
			}
			return &parsedInput{Input: input}, nil
		}
	}

	// 兜底：从一段文字里抠 BV 号或 av 号（比如带标题的分享文案）。
	if match := bvPattern.FindStringSubmatch(trimmed); match != nil {
		return &parsedInput{Input: &Input{Raw: raw, BVid: "BV" + match[1], Page: 1}}, nil
	}
	if match := avPattern.FindStringSubmatch(trimmed); match != nil {
		return &parsedInput{Input: &Input{Raw: raw, AID: match[2], Page: 1}}, nil
	}

	return nil, fmt.Errorf("无法识别的输入「%s」：支持 BV 号（如 BV1c5HK6CEPu）、av 号（如 av170001）、视频链接或 b23.tv 短链", raw)
}

func looksLikeHost(s string) bool {
	lower := strings.ToLower(s)
	for _, prefix := range []string{"www.bilibili.com", "m.bilibili.com", "bilibili.com", "b23.tv", "bili2233.cn"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func isShortLinkHost(host string) bool {
	if shortLinkHosts[host] {
		return true
	}
	return strings.HasSuffix(host, ".b23.tv")
}

func isBilibiliHost(host string) bool {
	return host == "bilibili.com" || strings.HasSuffix(host, ".bilibili.com")
}

// parseVideoURL 从视频链接里取 BV/av 号和 ?p= 分 P。
func parseVideoURL(raw string, parsedURL *url.URL) (*Input, error) {
	input := &Input{Raw: raw, Page: 1}
	switch {
	case bvPattern.MatchString(parsedURL.Path):
		match := bvPattern.FindStringSubmatch(parsedURL.Path)
		input.BVid = "BV" + match[1]
	case avPattern.MatchString(parsedURL.Path):
		match := avPattern.FindStringSubmatch(parsedURL.Path)
		input.AID = match[2]
	default:
		return nil, fmt.Errorf("链接里没有找到 BV 号或 av 号：%s", raw)
	}

	if pageText := parsedURL.Query().Get("p"); pageText != "" {
		page, err := strconv.Atoi(pageText)
		if err != nil || page < 1 {
			return nil, fmt.Errorf("链接里的分 P 参数 p=%q 无效，应为大于等于 1 的整数：%s", pageText, raw)
		}
		input.Page = page
	}
	return input, nil
}

// followShortLink 让 http.Client 跟完 302，最终地址在 resp.Request.URL 里。
func followShortLink(ctx context.Context, shortURL string) (*url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shortURL, nil)
	if err != nil {
		return nil, fmt.Errorf("解析短链 %s 失败：%w", shortURL, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", referer)

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("解析短链 %s 失败（网络不通或被拦截）：%w", shortURL, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.Request == nil || resp.Request.URL == nil {
		return nil, fmt.Errorf("解析短链 %s 失败：拿不到跳转后的地址", shortURL)
	}
	return resp.Request.URL, nil
}
