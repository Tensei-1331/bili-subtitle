//go:build integration

package ytdlp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testVideoURL = "https://www.bilibili.com/video/BV1c5HK6CEPu"

// lookupYtDlp 从环境变量或 PATH 里找 yt-dlp；找不到就跳过真实联调。
func lookupYtDlp(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("BILI_SUB_YTDLP"); p != "" {
		return p
	}
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		return p
	}
	t.Skip("PATH 里没有 yt-dlp，跳过真实联调（可设 BILI_SUB_YTDLP 指定 exe 路径）")
	return ""
}

// TestIntegrationNoCookiesNeedsLogin 验证无 cookies 时不会真下载视频，并且返回明确的“需要登录”错误。
func TestIntegrationNoCookiesNeedsLogin(t *testing.T) {
	exe := lookupYtDlp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	title, owner, cues, err := FetchSubtitles(ctx, exe, testVideoURL, "", nil)
	t.Logf("title=%q owner=%q cues=%d err=%v", title, owner, len(cues), err)
	if err == nil {
		t.Fatal("没有 cookies 时应该返回需要登录的错误")
	}
	if !strings.Contains(err.Error(), "登录") {
		t.Errorf("错误信息应该提示需要登录，实际：%v", err)
	}
	if len(cues) != 0 {
		t.Errorf("没有 cookies 却拿到了 %d 条字幕", len(cues))
	}
	if title == "" {
		t.Errorf("没拿到标题")
	}
	if owner == "" {
		t.Errorf("没拿到 UP 主")
	}
}

func TestIntegrationMissingCookiesFile(t *testing.T) {
	exe := lookupYtDlp(t)
	_, _, _, err := FetchSubtitles(context.Background(), exe, testVideoURL, filepath.Join(t.TempDir(), "nope.txt"), nil)
	if err == nil || !strings.Contains(err.Error(), "cookies") {
		t.Fatalf("cookies 文件不存在时应该报错并提到 cookies，实际：%v", err)
	}
}
