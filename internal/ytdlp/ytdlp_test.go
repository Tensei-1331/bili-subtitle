package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchSubtitlesMissingExe(t *testing.T) {
	_, _, _, err := FetchSubtitles(context.Background(), filepath.Join(t.TempDir(), "nope.exe"), "https://www.bilibili.com/video/BV1c5HK6CEPu", "", nil)
	if err == nil || !strings.Contains(err.Error(), "yt-dlp") {
		t.Fatalf("应该报找不到 yt-dlp，实际：%v", err)
	}
}

func TestFetchSubtitlesEmptyURL(t *testing.T) {
	if _, _, _, err := FetchSubtitles(context.Background(), "yt-dlp.exe", "  ", "", nil); err == nil {
		t.Fatal("空地址应该报错")
	}
}

func TestLoginRequired(t *testing.T) {
	yes := []string{
		"WARNING: [BiliBili] Subtitles are only available when logged in. Use --cookies-from-browser or --cookies for the authentication.",
		"ERROR: Only available when logged in",
	}
	for _, text := range yes {
		if !loginRequired(text) {
			t.Errorf("应该认出需要登录：%q", text)
		}
	}
	no := []string{"ERROR: Video unavailable", "WARNING: Falling back on generic information extractor", ""}
	for _, text := range no {
		if loginRequired(text) {
			t.Errorf("不该误判成需要登录：%q", text)
		}
	}
}

func TestFindSubtitleFilePrefersLangOrder(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"abc.en.srt", "abc.zh-Hans.srt", "abc.ai-zh.srt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := filepath.Base(findSubtitleFile(dir, []string{"ai-zh", "zh-Hans", "en"})); got != "abc.ai-zh.srt" {
		t.Fatalf("应该优先取 ai-zh，实际 %q", got)
	}
	if got := filepath.Base(findSubtitleFile(dir, []string{"zh-Hans"})); got != "abc.zh-Hans.srt" {
		t.Fatalf("应该优先取 zh-Hans，实际 %q", got)
	}
	// 语言都没命中时退回排好序的第一个 srt
	if got := filepath.Base(findSubtitleFile(dir, []string{"ja"})); got != "abc.ai-zh.srt" {
		t.Fatalf("没有命中语言时应退回任意 srt，实际 %q", got)
	}
	if got := findSubtitleFile(t.TempDir(), defaultLangs); got != "" {
		t.Fatalf("空目录应该返回空字符串，实际 %q", got)
	}
}

func TestParsePrintedMetadata(t *testing.T) {
	title, owner := parsePrintedMetadata("\ufeff双人抬棺\nZ贤者\n")
	if title != "双人抬棺" || owner != "Z贤者" {
		t.Fatalf("两行元数据解析不对：title=%q owner=%q", title, owner)
	}
	title, owner = parsePrintedMetadata("只有标题\n")
	if title != "只有标题" || owner != "" {
		t.Fatalf("只有一行时解析不对：title=%q owner=%q", title, owner)
	}
}

func TestSetEnvOverrides(t *testing.T) {
	env := setEnv([]string{"A=1", "pythonioencoding=gbk", "B=2"}, "PYTHONIOENCODING", "utf-8")
	joined := strings.Join(env, "|")
	if strings.Contains(strings.ToLower(joined), "pythonioencoding=gbk") {
		t.Fatalf("旧变量没被覆盖：%s", joined)
	}
	if !strings.Contains(joined, "PYTHONIOENCODING=utf-8") || !strings.Contains(joined, "A=1") {
		t.Fatalf("覆盖后的环境变量不对：%s", joined)
	}
}
