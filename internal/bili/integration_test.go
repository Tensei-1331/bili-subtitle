//go:build integration

package bili

import (
	"context"
	"strings"
	"testing"
	"time"
)

const testBV = "BV1c5HK6CEPu"

func newTestContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 60*time.Second)
}

func TestIntegrationResolveInput(t *testing.T) {
	ctx, cancel := newTestContext(t)
	defer cancel()

	cases := []struct {
		raw      string
		wantBV   string
		wantPage int
	}{
		{"BV1c5HK6CEPu", testBV, 1},
		{"https://www.bilibili.com/video/BV1c5HK6CEPu?p=1&t=3", testBV, 1},
		{"https://b23.tv/BV1c5HK6CEPu", testBV, 1},
	}
	for _, tc := range cases {
		input, err := ResolveInput(ctx, tc.raw)
		if err != nil {
			t.Errorf("ResolveInput(%q) 失败：%v", tc.raw, err)
			continue
		}
		t.Logf("ResolveInput(%q) => BVid=%s AID=%s Page=%d", tc.raw, input.BVid, input.AID, input.Page)
		if input.BVid != tc.wantBV || input.Page != tc.wantPage {
			t.Errorf("ResolveInput(%q) 结果不对：BVid=%q Page=%d", tc.raw, input.BVid, input.Page)
		}
	}
}

func TestIntegrationVideoInfo(t *testing.T) {
	ctx, cancel := newTestContext(t)
	defer cancel()

	video, err := New(nil).VideoInfo(ctx, &Input{Raw: testBV, BVid: testBV, Page: 1})
	if err != nil {
		t.Fatalf("VideoInfo 失败：%v", err)
	}
	t.Logf("真实视频信息：标题=%q UP=%q 时长=%ds cid=%s URL=%s", video.Title, video.Owner, video.Duration, video.CID, video.URL)
	if video.Title == "" {
		t.Error("标题为空")
	}
	if video.Owner == "" {
		t.Error("UP 名为空")
	}
	if video.CID == "" {
		t.Error("cid 为空")
	}
	if video.Duration <= 0 {
		t.Error("时长应该大于 0")
	}
	if !strings.Contains(video.URL, testBV) {
		t.Errorf("URL 里应该带 BV 号：%s", video.URL)
	}
}

func TestIntegrationSubtitleTracksUnlogged(t *testing.T) {
	ctx, cancel := newTestContext(t)
	defer cancel()

	client := New(nil)
	video, err := client.VideoInfo(ctx, &Input{BVid: testBV, Page: 1})
	if err != nil {
		t.Fatalf("VideoInfo 失败：%v", err)
	}
	tracks, err := client.SubtitleTracks(ctx, video)
	if err != nil {
		t.Fatalf("未登录查字幕轨道不应该报错：%v", err)
	}
	t.Logf("未登录时字幕轨道数=%d（WBI 签名已走通，B 站限制未登录不给字幕）", len(tracks))
	if len(tracks) != 0 {
		t.Logf("注意：未登录也返回了 %d 条轨道，B 站策略可能变了", len(tracks))
	}
}

func TestIntegrationQRLogin(t *testing.T) {
	ctx, cancel := newTestContext(t)
	defer cancel()

	qrURL, key, err := QRGenerate(ctx)
	if err != nil {
		t.Fatalf("QRGenerate 失败：%v", err)
	}
	t.Logf("二维码 url=%s key=%s", qrURL, key)
	if !strings.Contains(qrURL, "bilibili.com") {
		t.Errorf("二维码地址看着不对：%s", qrURL)
	}
	if len(key) < 16 {
		t.Errorf("二维码 key 太短：%q", key)
	}

	status, message, cookies, err := QRPoll(ctx, key)
	if err != nil {
		t.Fatalf("QRPoll 失败：%v", err)
	}
	t.Logf("首次轮询：status=%d message=%q cookies=%d", status, message, len(cookies))
	switch status {
	case QRStatusNotScanned, QRStatusScanned:
		// 正常：没人扫码
	case QRStatusExpired:
		t.Logf("二维码已经过期（生成后等太久？）")
	case QRStatusSuccess:
		t.Logf("二维码被扫了，cookies 数=%d", len(cookies))
	default:
		t.Errorf("未知状态码 %d（message=%q）", status, message)
	}
}

func TestIntegrationRenderQRTerminal(t *testing.T) {
	// 只确认不 panic；输出可以重定向掉，不刷屏。
	qrURL, _, err := QRGenerate(context.Background())
	if err != nil {
		t.Fatalf("QRGenerate 失败：%v", err)
	}
	RenderQRTerminal(qrURL)
}
