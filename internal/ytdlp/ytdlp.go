// Package ytdlp 是字幕兜底方案：调 yt-dlp 下载 B 站字幕并解析成 model.Cue。
package ytdlp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"bili-subtitle/internal/model"
)

// defaultLangs 跟 config.SubLangs 的默认值一致，调用方没给语言时用。
var defaultLangs = []string{"ai-zh", "zh-Hans", "zh-CN", "zh", "ai-en", "en"}

// FetchSubtitles 用 yt-dlp 兜底下载字幕。exePath 是 yt-dlp.exe 路径；
// 按 langs 顺序优先选语言，下载 SRT 后解析。返回标题、UP 主和字幕。
func FetchSubtitles(ctx context.Context, exePath, rawURL, cookiesFile string, langs []string) (title, owner string, cues []model.Cue, err error) {
	if strings.TrimSpace(rawURL) == "" {
		return "", "", nil, errors.New("yt-dlp：视频地址为空")
	}
	if strings.TrimSpace(exePath) == "" {
		return "", "", nil, errors.New("yt-dlp：没有配置 yt-dlp.exe 路径")
	}
	if info, statErr := os.Stat(exePath); statErr != nil || info.IsDir() {
		return "", "", nil, fmt.Errorf("找不到 yt-dlp：%s（检查配置里的 ytdlpPath）", exePath)
	}
	if len(langs) == 0 {
		langs = defaultLangs
	}

	tmpDir, err := os.MkdirTemp("", "bili-subtitle-ytdlp-")
	if err != nil {
		return "", "", nil, fmt.Errorf("yt-dlp：建临时目录失败：%w", err)
	}
	defer os.RemoveAll(tmpDir)

	args := []string{
		"--ignore-config", // 不读用户 yt-dlp 配置，行为才可预期
		"--no-playlist",
		"--skip-download", // 只要字幕，不碰视频本体
		"--write-subs",
		"--sub-langs", strings.Join(langs, ","),
		"--sub-format", "srt/vtt/best",
		"--convert-subs", "srt",
		"--no-simulate",       // --print 默认带 --simulate，模拟模式下不会真的写字幕文件
		"--encoding", "utf-8", // 中文 Windows 下 yt-dlp 默认按 GBK 输出，标题会乱码
		"--print", "%(title)s",
		"--print", "%(uploader)s",
	}
	if strings.TrimSpace(cookiesFile) != "" {
		if _, statErr := os.Stat(cookiesFile); statErr != nil {
			return "", "", nil, fmt.Errorf("yt-dlp 需要 cookies 文件，但 %s 不存在：请先登录生成 cookies，或把配置里的 cookiesFile 清空", cookiesFile)
		}
		args = append(args, "--cookies", cookiesFile)
	}
	args = append(args, "-o", filepath.Join(tmpDir, "%(id)s"), rawURL)

	command := exec.CommandContext(ctx, exePath, args...)
	command.Env = setEnv(setEnv(os.Environ(), "PYTHONIOENCODING", "utf-8"), "PYTHONUTF8", "1")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()

	title, owner = parsePrintedMetadata(stdout.String())

	subtitleFile := findSubtitleFile(tmpDir, langs)
	if subtitleFile == "" {
		stderrText := stderr.String()
		switch {
		case loginRequired(stderrText):
			return title, owner, nil, fmt.Errorf("B 站字幕需要登录才能拿到（yt-dlp 提示要 cookies）。请先跑登录生成 %s 再试。yt-dlp 输出：%s",
				cookiesDisplayName(cookiesFile), oneLine(tail(stderrText, 600)))
		case ctx.Err() != nil:
			return title, owner, nil, fmt.Errorf("yt-dlp 执行被取消或超时：%w", ctx.Err())
		case runErr != nil:
			return title, owner, nil, fmt.Errorf("yt-dlp 执行失败：%w；输出（末尾）：%s", runErr, oneLine(tail(stderrText, 800)))
		default:
			return title, owner, nil, fmt.Errorf("yt-dlp 没下到字幕（视频可能没有字幕，或语言不在 %s 里）。yt-dlp 输出：%s",
				strings.Join(langs, ","), oneLine(tail(stderrText, 600)))
		}
	}

	data, err := os.ReadFile(subtitleFile)
	if err != nil {
		return title, owner, nil, fmt.Errorf("读取 yt-dlp 下载的字幕 %s 失败：%w", filepath.Base(subtitleFile), err)
	}
	cues = ParseSRT(data)
	if len(cues) == 0 {
		return title, owner, nil, fmt.Errorf("yt-dlp 下了字幕文件 %s，但解析后没有内容", filepath.Base(subtitleFile))
	}
	return title, owner, cues, nil
}

// parsePrintedMetadata 解析 --print 打出来的两行：标题、UP 主。
func parsePrintedMetadata(stdout string) (title, owner string) {
	stdout = strings.TrimPrefix(stdout, "\ufeff")
	lines := strings.Split(stdout, "\n")
	if len(lines) > 0 {
		title = strings.TrimSpace(lines[0])
	}
	if len(lines) > 1 {
		owner = strings.TrimSpace(lines[1])
	}
	return title, owner
}

// findSubtitleFile 在临时目录里按 langs 顺序找字幕文件，都没命中就退回任意 srt/vtt。
func findSubtitleFile(dir string, langs []string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var srtFiles, vttFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch {
		case strings.HasSuffix(strings.ToLower(name), ".srt"):
			srtFiles = append(srtFiles, name)
		case strings.HasSuffix(strings.ToLower(name), ".vtt"):
			vttFiles = append(vttFiles, name)
		}
	}
	pickByLang := func(files []string, lang, ext string) string {
		suffix := "." + strings.ToLower(lang) + ext
		for _, name := range files {
			if strings.HasSuffix(strings.ToLower(name), suffix) {
				return filepath.Join(dir, name)
			}
		}
		return ""
	}
	for _, lang := range langs {
		if found := pickByLang(srtFiles, lang, ".srt"); found != "" {
			return found
		}
	}
	sort.Strings(srtFiles)
	if len(srtFiles) > 0 {
		return filepath.Join(dir, srtFiles[0])
	}
	for _, lang := range langs {
		if found := pickByLang(vttFiles, lang, ".vtt"); found != "" {
			return found
		}
	}
	sort.Strings(vttFiles)
	if len(vttFiles) > 0 {
		return filepath.Join(dir, vttFiles[0])
	}
	return ""
}

// loginRequired 认 yt-dlp 在未登录时的提示，B 站会明确说要 cookies 才能拿字幕。
func loginRequired(stderrText string) bool {
	lower := strings.ToLower(stderrText)
	for _, needle := range []string{
		"subtitles are only available when logged in",
		"only available when logged in",
		"login required",
		"需要登录",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func cookiesDisplayName(cookiesFile string) string {
	if strings.TrimSpace(cookiesFile) == "" {
		return "cookies.txt"
	}
	return cookiesFile
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// tail 取末尾最多 limit 个字符，报错时不糊一屏。
func tail(s string, limit int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= limit {
		return string(runes)
	}
	return "…" + string(runes[len(runes)-limit:])
}

// setEnv 覆盖一个环境变量，Windows 上变量名不分大小写。
func setEnv(env []string, name, value string) []string {
	prefix := name + "="
	kept := make([]string, 0, len(env)+1)
	for _, item := range env {
		if len(item) >= len(prefix) && strings.EqualFold(item[:len(prefix)], prefix) {
			continue
		}
		kept = append(kept, item)
	}
	return append(kept, prefix+value)
}
