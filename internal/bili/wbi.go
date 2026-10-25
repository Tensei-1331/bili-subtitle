package bili

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// wbiKeyTTL 控制签名 key 的缓存时间。B 站每天会换 key，缓存短一点更稳。
const wbiKeyTTL = 30 * time.Minute

// mixinKeyEncTab 是 WBI 签名用的字符重排表，顺序不能改。
var mixinKeyEncTab = []int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35,
	27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13,
	37, 48, 7, 16, 24, 55, 40, 61, 26, 17, 0, 1, 60, 51, 30, 4,
	22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11, 36, 20, 34, 44, 52,
}

// wbiKeys 是 nav 接口给的两把 key 和算出来的 mixinKey。
type wbiKeys struct {
	imgKey   string
	subKey   string
	mixinKey string
	fetched  time.Time
}

// mixinKey 把 img_key 和 sub_key 拼起来，按固定表重排后取前 32 位当签名盐。
// 表有 64 项，B 站的 JS 实现映射完会 .slice(0, 32)，不截断就和浏览器算出来的不一样。
func mixinKey(imgKey, subKey string) string {
	raw := imgKey + subKey
	mixed := make([]byte, 0, len(mixinKeyEncTab))
	for _, index := range mixinKeyEncTab {
		if index >= 0 && index < len(raw) {
			mixed = append(mixed, raw[index])
		}
	}
	result := string(mixed)
	if len(result) > 32 {
		result = result[:32]
	}
	return result
}

// signQuery 按 WBI 规则签名：参数名升序、过滤值里的 !'()*、百分号编码，最后拼 wts 和 w_rid。
// 返回可以直接接在 URL 后面的查询串。
func signQuery(keys wbiKeys, params map[string]string, now time.Time) string {
	signed := make(map[string]string, len(params)+1)
	for key, value := range params {
		signed[key] = value
	}
	signed["wts"] = strconv.FormatInt(now.Unix(), 10)

	names := make([]string, 0, len(signed))
	for name := range signed {
		names = append(names, name)
	}
	sort.Strings(names)

	var builder strings.Builder
	for i, name := range names {
		if i > 0 {
			builder.WriteByte('&')
		}
		builder.WriteString(jsEscape(name))
		builder.WriteByte('=')
		builder.WriteString(jsEscape(filterWBIValue(signed[name])))
	}
	query := builder.String()
	return query + "&w_rid=" + md5Hex(query+keys.mixinKey)
}

func filterWBIValue(value string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '!', '\'', '(', ')', '*':
			return -1
		}
		return r
	}, value)
}

// jsEscape 跟 JS 的 encodeURIComponent 对齐。Go 的 url.QueryEscape 会把空格编成 +，
// 两边算出来的待签名串就不一样了。
func jsEscape(s string) string {
	const hexUpper = "0123456789ABCDEF"
	var builder strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == '~' ||
			c == '!' || c == '\'' || c == '(' || c == ')' || c == '*' {
			builder.WriteByte(c)
			continue
		}
		builder.WriteByte('%')
		builder.WriteByte(hexUpper[c>>4])
		builder.WriteByte(hexUpper[c&0x0f])
	}
	return builder.String()
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// extractWBIKey 从 https://i0.hdslb.com/bfs/wbi/xxxx.png 里取文件名当 key。
func extractWBIKey(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if parsed, err := url.Parse(rawURL); err == nil && parsed.Path != "" {
		rawURL = parsed.Path
	}
	base := rawURL
	if index := strings.LastIndexAny(base, "/\\"); index >= 0 {
		base = base[index+1:]
	}
	if index := strings.Index(base, "."); index > 0 {
		base = base[:index]
	}
	return base
}

// wbiKeys 取签名 key，缓存 wbiKeyTTL 时间。
func (c *Client) wbiKeys(ctx context.Context) (wbiKeys, error) {
	c.mu.Lock()
	cached := c.keys
	c.mu.Unlock()
	if cached != nil && time.Since(cached.fetched) < wbiKeyTTL {
		return *cached, nil
	}
	return c.fetchWBIKeys(ctx)
}

func (c *Client) fetchWBIKeys(ctx context.Context) (wbiKeys, error) {
	body, _, err := c.get(ctx, apiBase+"/x/web-interface/nav", "WBI key（nav）")
	if err != nil {
		return wbiKeys{}, err
	}
	var nav struct {
		Data struct {
			WbiImg struct {
				ImgURL string `json:"img_url"`
				SubURL string `json:"sub_url"`
			} `json:"wbi_img"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &nav); err != nil {
		return wbiKeys{}, fmt.Errorf("WBI key：解析 nav 响应失败：%w（响应开头：%s）", err, snippet(body))
	}
	keys := wbiKeys{
		imgKey:  extractWBIKey(nav.Data.WbiImg.ImgURL),
		subKey:  extractWBIKey(nav.Data.WbiImg.SubURL),
		fetched: time.Now(),
	}
	if keys.imgKey == "" || keys.subKey == "" {
		return wbiKeys{}, fmt.Errorf("WBI key：nav 没返回 img_url/sub_url（响应开头：%s）", snippet(body))
	}
	keys.mixinKey = mixinKey(keys.imgKey, keys.subKey)
	c.mu.Lock()
	c.keys = &keys
	c.mu.Unlock()
	return keys, nil
}

// refreshWBIKeys 在签名被风控拒绝时强制换一份 key 再试。
func (c *Client) refreshWBIKeys(ctx context.Context) (wbiKeys, error) {
	c.mu.Lock()
	c.keys = nil
	c.mu.Unlock()
	return c.wbiKeys(ctx)
}
