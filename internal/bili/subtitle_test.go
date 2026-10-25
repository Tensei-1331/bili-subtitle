package bili

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseSubtitleJSON(t *testing.T) {
	data := []byte(`{"body":[{"from":0.5,"to":2.5,"content":"  第一句  "},{"from":3,"to":3,"content":"零长度"},{"from":4,"to":5,"content":" "},{"from":6,"to":7.5,"content":"第二句"}]}`)
	cues, err := parseSubtitleJSON(data)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(cues) != 2 {
		t.Fatalf("应该只剩 2 条有效字幕，实际 %d：%+v", len(cues), cues)
	}
	if cues[0].From != 0.5 || cues[0].To != 2.5 || cues[0].Text != "第一句" {
		t.Fatalf("第一条字幕不对：%+v", cues[0])
	}
	if cues[1].From != 6 || cues[1].Text != "第二句" {
		t.Fatalf("第二条字幕不对：%+v", cues[1])
	}

	if empty, err := parseSubtitleJSON([]byte(`{"body":[]}`)); err != nil || len(empty) != 0 {
		t.Fatalf("空 body 应该返回空切片和 nil：cues=%v err=%v", empty, err)
	}
	if _, err := parseSubtitleJSON([]byte("这不是 JSON")); err == nil {
		t.Fatal("非 JSON 应该报错")
	}
}

func TestNormalizeSubtitleURL(t *testing.T) {
	cases := map[string]string{
		"//aisubtitle.hdslb.com/bfs/ai_subtitle/prod/a.json":      "https://aisubtitle.hdslb.com/bfs/ai_subtitle/prod/a.json",
		"http://aisubtitle.hdslb.com/bfs/ai_subtitle/prod/b.json": "https://aisubtitle.hdslb.com/bfs/ai_subtitle/prod/b.json",
		"https://aisubtitle.hdslb.com/c.json":                     "https://aisubtitle.hdslb.com/c.json",
		"":                                                        "",
	}
	for raw, want := range cases {
		if got := normalizeSubtitleURL(raw); got != want {
			t.Errorf("normalizeSubtitleURL(%q)=%q，期望 %q", raw, got, want)
		}
	}
}

func TestSubtitleTracksFromPlayerResponse(t *testing.T) {
	raw := []byte(`{"code":0,"data":{"need_login_subtitle":true,"subtitle":{"subtitles":[{"id":1,"lan":"ai-zh","lan_doc":"中文（AI生成）","subtitle_url":"//aisubtitle.hdslb.com/a.json"},{"id":2,"lan":"zh-CN","lan_doc":"中文（中国）","subtitle_url":"","subtitle_url_v2":"https://aisubtitle.hdslb.com/b.json"},{"id":3,"lan":"en","lan_doc":"English","subtitle_url":""}]}}}`)
	var response playerResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("测试数据解析失败：%v", err)
	}
	tracks := subtitleTracks(&response)
	if len(tracks) != 2 {
		t.Fatalf("应该解析出 2 条能用的轨道，实际 %d：%+v", len(tracks), tracks)
	}
	if tracks[0].Lang != "ai-zh" || tracks[0].LangDoc != "中文（AI生成）" || tracks[0].URL != "https://aisubtitle.hdslb.com/a.json" {
		t.Fatalf("第一条轨道不对：%+v", tracks[0])
	}
	if tracks[1].Lang != "zh-CN" || tracks[1].URL != "https://aisubtitle.hdslb.com/b.json" {
		t.Fatalf("第二条轨道（用 subtitle_url_v2 兜底）不对：%+v", tracks[1])
	}
}

func TestFetchCues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Errorf("请求没带 User-Agent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"body":[{"from":0,"to":1.5,"content":"你好"},{"from":1.5,"to":3,"content":"世界"}]}`))
	}))
	defer server.Close()

	cues, err := FetchCues(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("FetchCues 失败：%v", err)
	}
	if len(cues) != 2 || cues[0].Text != "你好" || cues[1].From != 1.5 {
		t.Fatalf("解析结果不对：%+v", cues)
	}
}

func TestFetchCuesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusForbidden)
	}))
	defer server.Close()

	if _, err := FetchCues(context.Background(), server.URL); err == nil {
		t.Fatal("HTTP 403 应该报错")
	}
}
