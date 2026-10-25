package bili

import (
	"context"
	"testing"
)

func TestParseInput(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantBV    string
		wantAID   string
		wantPage  int
		wantShort bool
		wantErr   bool
	}{
		{name: "纯 BV", raw: "BV1c5HK6CEPu", wantBV: "BV1c5HK6CEPu", wantPage: 1},
		{name: "BV 小写前缀", raw: "bv1c5HK6CEPu", wantBV: "BV1c5HK6CEPu", wantPage: 1},
		{name: "BV 前后空格", raw: "  BV1c5HK6CEPu  ", wantBV: "BV1c5HK6CEPu", wantPage: 1},
		{name: "纯 av", raw: "av170001", wantAID: "170001", wantPage: 1},
		{name: "完整链接", raw: "https://www.bilibili.com/video/BV1c5HK6CEPu?p=3&t=12", wantBV: "BV1c5HK6CEPu", wantPage: 3},
		{name: "m 站链接", raw: "https://m.bilibili.com/video/BV1c5HK6CEPu?p=2", wantBV: "BV1c5HK6CEPu", wantPage: 2},
		{name: "av 链接", raw: "https://www.bilibili.com/video/av170001", wantAID: "170001", wantPage: 1},
		{name: "没写协议", raw: "www.bilibili.com/video/BV1c5HK6CEPu", wantBV: "BV1c5HK6CEPu", wantPage: 1},
		{name: "b23 短链", raw: "https://b23.tv/AbCdEfG", wantPage: 1, wantShort: true},
		{name: "b23 短链没协议", raw: "b23.tv/AbCdEfG", wantPage: 1, wantShort: true},
		{name: "从文字里抠 BV", raw: "看这个 BV1c5HK6CEPu 很有意思", wantBV: "BV1c5HK6CEPu", wantPage: 1},
		{name: "分享文案带标题和多余参数", raw: "【某视频标题】https://www.bilibili.com/video/BV1XdaD6UE2Y?vd_source=2ab1446137bfb951dca2d05238ae000a", wantBV: "BV1XdaD6UE2Y", wantPage: 1},
		{name: "分享文案保留 p", raw: "【某视频标题】https://www.bilibili.com/video/BV1XdaD6UE2Y?p=2&vd_source=2ab1446137bfb951dca2d05238ae000a", wantBV: "BV1XdaD6UE2Y", wantPage: 2},
		{name: "分享文案带短链", raw: "【某视频标题】https://b23.tv/AbCdEfG", wantPage: 1, wantShort: true},
		{name: "文字里没写协议的链接", raw: "看这个 www.bilibili.com/video/BV1c5HK6CEPu?p=3 不错", wantBV: "BV1c5HK6CEPu", wantPage: 3},
		{name: "文字里裸 BV 带 p 参数", raw: "分享 BV1c5HK6CEPu?p=2 给你", wantBV: "BV1c5HK6CEPu", wantPage: 2},
		{name: "分 P 参数不合法", raw: "https://www.bilibili.com/video/BV1c5HK6CEPu?p=abc", wantErr: true},
		{name: "分 P 参数是 0", raw: "https://www.bilibili.com/video/BV1c5HK6CEPu?p=0", wantErr: true},
		{name: "无法识别", raw: "随便一行字", wantErr: true},
		{name: "空输入", raw: "   ", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parseInput(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际解析成了 %+v", parsed)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败：%v", err)
			}
			if parsed.ShortURL != "" != tc.wantShort {
				t.Fatalf("短链标记不对：ShortURL=%q wantShort=%v", parsed.ShortURL, tc.wantShort)
			}
			if parsed.Input.BVid != tc.wantBV {
				t.Errorf("BVid=%q，期望 %q", parsed.Input.BVid, tc.wantBV)
			}
			if parsed.Input.AID != tc.wantAID {
				t.Errorf("AID=%q，期望 %q", parsed.Input.AID, tc.wantAID)
			}
			if parsed.Input.Page != tc.wantPage {
				t.Errorf("Page=%d，期望 %d", parsed.Input.Page, tc.wantPage)
			}
		})
	}
}

// TestResolveInputNoNetwork 确认 BV 号和完整链接不会联网，直接返回解析结果。
func TestResolveInputNoNetwork(t *testing.T) {
	ctx := context.Background()
	input, err := ResolveInput(ctx, "BV1c5HK6CEPu")
	if err != nil {
		t.Fatalf("ResolveInput 失败：%v", err)
	}
	if input.BVid != "BV1c5HK6CEPu" || input.Page != 1 {
		t.Fatalf("解析结果不对：%+v", input)
	}

	input, err = ResolveInput(ctx, "https://www.bilibili.com/video/BV1c5HK6CEPu?p=2")
	if err != nil {
		t.Fatalf("ResolveInput 失败：%v", err)
	}
	if input.BVid != "BV1c5HK6CEPu" || input.Page != 2 {
		t.Fatalf("解析结果不对：%+v", input)
	}
}
