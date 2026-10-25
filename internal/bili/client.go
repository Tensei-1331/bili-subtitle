// Package bili 封装 B 站数据获取：输入解析、视频信息、字幕轨道、字幕下载和二维码登录。
package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// 用 Chrome 141 的 UA，B 站部分接口会按 UA 返回不同的结构。
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
	referer   = "https://www.bilibili.com/"

	apiBase      = "https://api.bilibili.com"
	passportBase = "https://passport.bilibili.com"

	maxResponseBytes = 8 << 20
	requestTimeout   = 30 * time.Second
)

// defaultHTTPClient 给包级函数（ResolveInput、QRGenerate、FetchCues）用。
var defaultHTTPClient = &http.Client{Timeout: requestTimeout}

// defaultClient 是包级函数共用的无 cookies 客户端。
var defaultClient = &Client{}

// Client 是带登录态的 B 站 API 客户端，请用 New 构造。
type Client struct {
	cookies map[string]string
	http    *http.Client

	mu   sync.Mutex
	keys *wbiKeys // 缓存的 WBI 签名 key，见 wbi.go
}

// New 用 cookies（name->value）构造客户端；nil 表示未登录。
func New(cookies map[string]string) *Client {
	copied := make(map[string]string, len(cookies))
	for name, value := range cookies {
		if name == "" {
			continue
		}
		copied[name] = value
	}
	return &Client{
		cookies: copied,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

func (c *Client) httpClient() *http.Client {
	if c == nil || c.http == nil {
		return defaultHTTPClient
	}
	return c.http
}

// APIError 表示 B 站接口返回了非 0 的业务 code。
type APIError struct {
	Code    int
	Message string
	API     string // 接口用途，报错时方便定位是哪一个请求
}

func (e *APIError) Error() string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = "无错误信息"
	}
	return fmt.Sprintf("B 站接口「%s」返回 code=%d：%s", e.API, e.Code, msg)
}

func (c *Client) newRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", referer)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	if c != nil {
		if header := cookieHeader(c.cookies); header != "" {
			req.Header.Set("Cookie", header)
		}
	}
	return req, nil
}

// cookieHeader 固定按名字排序，同样的 cookies 拼出来的请求头每次一样，出问题好复现。
func cookieHeader(cookies map[string]string) string {
	if len(cookies) == 0 {
		return ""
	}
	names := make([]string, 0, len(cookies))
	for name, value := range cookies {
		if name == "" || value == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+cookies[name])
	}
	return strings.Join(parts, "; ")
}

// get 发 GET 取原始 body，apiName 只用于报错。
func (c *Client) get(ctx context.Context, rawURL, apiName string) ([]byte, *http.Response, error) {
	req, err := c.newRequest(ctx, rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("%s：构造请求失败：%w", apiName, err)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%s：请求失败：%w", apiName, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("%s：读取响应失败：%w", apiName, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp, fmt.Errorf("%s：HTTP %d（响应开头：%s）", apiName, resp.StatusCode, snippet(body))
	}
	return body, resp, nil
}

// apiGet 按 {code,message,data} 解包，code 非 0 时返回 *APIError。
func (c *Client) apiGet(ctx context.Context, rawURL, apiName string, out any) error {
	body, _, err := c.get(ctx, rawURL, apiName)
	if err != nil {
		return err
	}
	return decodeAPIResponse(body, apiName, out)
}

func decodeAPIResponse(body []byte, apiName string, out any) error {
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("%s：响应不是合法 JSON：%w（响应开头：%s）", apiName, err, snippet(body))
	}
	if envelope.Code != 0 {
		return &APIError{Code: envelope.Code, Message: envelope.Message, API: apiName}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s：解析 data 失败：%w（响应开头：%s）", apiName, err, snippet(body))
	}
	return nil
}

func snippet(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
