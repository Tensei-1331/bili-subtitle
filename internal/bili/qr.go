package bili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/mdp/qrterminal/v3"
)

// 二维码登录轮询状态码，QRPoll 返回的 status 就是这几个值。
const (
	QRStatusSuccess    = 0     // 登录成功
	QRStatusNotScanned = 86101 // 还没扫码
	QRStatusScanned    = 86090 // 已扫码，等手机确认
	QRStatusExpired    = 86038 // 二维码过期
)

// QRGenerate 申请登录二维码，返回二维码内容 URL 和轮询 key。
func QRGenerate(ctx context.Context) (string, string, error) {
	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			URL       string `json:"url"`
			QRCodeKey string `json:"qrcode_key"`
		} `json:"data"`
	}
	if err := defaultClient.apiGet(ctx, passportBase+"/x/passport-login/web/qrcode/generate", "二维码申请（qrcode/generate）", &response); err != nil {
		return "", "", fmt.Errorf("申请登录二维码失败：%w", err)
	}
	if response.Data.URL == "" || response.Data.QRCodeKey == "" {
		return "", "", fmt.Errorf("申请登录二维码失败：接口没返回 url 或 qrcode_key（code=%d %s）", response.Code, response.Message)
	}
	return response.Data.URL, response.Data.QRCodeKey, nil
}

// QRPoll 轮询扫码状态：status 0=成功，86101=未扫码，86090=已扫码待确认，86038=已过期。
// 成功时从响应的 Set-Cookie 里收集登录 cookies。
func QRPoll(ctx context.Context, key string) (int, string, []*http.Cookie, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return 0, "", nil, errors.New("QRPoll：二维码 key 为空")
	}
	fullURL := passportBase + "/x/passport-login/web/qrcode/poll?qrcode_key=" + url.QueryEscape(key)
	body, response, err := defaultClient.get(ctx, fullURL, "二维码轮询（qrcode/poll）")
	if err != nil {
		return 0, "", nil, fmt.Errorf("轮询扫码状态失败：%w", err)
	}

	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			URL     string `json:"url"`
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, "", nil, fmt.Errorf("轮询扫码状态：响应不是合法 JSON：%w（响应开头：%s）", err, snippet(body))
	}
	if payload.Code != 0 {
		return 0, "", nil, &APIError{Code: payload.Code, Message: payload.Message, API: "二维码轮询（qrcode/poll）"}
	}

	status := payload.Data.Code
	message := strings.TrimSpace(payload.Data.Message)
	if message == "" {
		message = qrStatusText(status)
	}
	var cookies []*http.Cookie
	if status == QRStatusSuccess {
		cookies = response.Cookies()
		if len(cookies) == 0 {
			// 正常靠 Set-Cookie；个别代理会改响应头，再从登录回调地址的 query 里捞一次。
			cookies = cookiesFromLoginURL(payload.Data.URL)
		}
		if len(cookies) == 0 {
			return status, message, nil, errors.New("扫码成功但没拿到登录 cookies（响应被代理改写？），请重新生成二维码再试")
		}
	}
	return status, message, cookies, nil
}

// cookiesFromLoginURL 从登录成功回调地址的 query 里取 cookie。回调形如
// https://passport.biligame.com/crossDomain?DedeUserID=...&SESSDATA=...&bili_jct=...
var loginCookieNames = []string{"SESSDATA", "bili_jct", "DedeUserID", "DedeUserID__ckMd5", "sid"}

func cookiesFromLoginURL(rawURL string) []*http.Cookie {
	if strings.TrimSpace(rawURL) == "" {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	query := parsed.Query()
	var cookies []*http.Cookie
	for _, name := range loginCookieNames {
		value := query.Get(name)
		if value == "" {
			continue
		}
		cookies = append(cookies, &http.Cookie{Name: name, Value: value, Domain: defaultCookieDomain, Path: "/"})
	}
	return cookies
}

func qrStatusText(status int) string {
	switch status {
	case QRStatusSuccess:
		return "扫码登录成功"
	case QRStatusNotScanned:
		return "等待扫码"
	case QRStatusScanned:
		return "已扫码，请在手机上确认登录"
	case QRStatusExpired:
		return "二维码已过期，请重新生成"
	default:
		return fmt.Sprintf("未知扫码状态 %d", status)
	}
}

// RenderQRTerminal 在终端画二维码。用 L 级纠错，码面小一点，窄终端也放得下。
func RenderQRTerminal(contentURL string) {
	qrterminal.Generate(contentURL, qrterminal.L, os.Stdout)
}
