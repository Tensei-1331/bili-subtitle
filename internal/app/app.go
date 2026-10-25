// Package app 把「取字幕 → 总结 → 写笔记」的流程从 CLI 抽出来，CLI 和本地 Web UI 共用。
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"bili-subtitle/internal/bili"
	"bili-subtitle/internal/config"
	"bili-subtitle/internal/model"
	"bili-subtitle/internal/note"
	"bili-subtitle/internal/summarize"
	"bili-subtitle/internal/transcript"
	"bili-subtitle/internal/ytdlp"
)

// LogFunc 接收过程日志：CLI 直接打印，Web 端收进数组展示。
type LogFunc func(format string, args ...any)

func logfOrNop(logf LogFunc) LogFunc {
	if logf != nil {
		return logf
	}
	return func(string, ...any) {}
}

func clone(c *config.Config) *config.Config {
	cp := *c
	cp.SubLangs = append([]string(nil), c.SubLangs...)
	return &cp
}

// FetchOptions 是取字幕这一步的参数；空值表示用配置里的默认。
type FetchOptions struct {
	Input   string   // BV 号 / 链接 / 短链
	Engine  string   // auto | api | ytdlp，默认 auto
	Cookies string   // 覆盖配置里的 cookies 路径
	Langs   []string // 覆盖字幕语言优先级
}

// Fetched 是取字幕的结果。
type Fetched struct {
	Video       *model.Video
	Lang        string
	Cues        []model.Cue
	Text        string
	CookiesPath string
	LoggedIn    bool
}

// Fetch 解析输入、取元数据和字幕；API 拿不到时按 engine 决定要不要回退 yt-dlp。
func Fetch(ctx context.Context, cfg *config.Config, o FetchOptions, logf LogFunc) (*Fetched, error) {
	log := logfOrNop(logf)
	if cfg == nil {
		return nil, errors.New("配置为空")
	}
	c := clone(cfg)
	engine := strings.TrimSpace(o.Engine)
	if engine == "" {
		engine = "auto"
	}
	if len(o.Langs) > 0 {
		c.SubLangs = append([]string(nil), o.Langs...)
	}
	cookiePath := c.Resolve(c.CookiesFile)
	if o.Cookies != "" {
		cookiePath = o.Cookies
	}

	log("解析输入：%s", o.Input)
	in, err := bili.ResolveInput(ctx, o.Input)
	if err != nil {
		return nil, fmt.Errorf("解析链接失败：%w", err)
	}

	cookies, cerr := bili.LoadCookiesFile(cookiePath)
	if cerr != nil {
		log("读取 cookies 失败（%v），按未登录继续", cerr)
	}
	loggedIn := cookies["SESSDATA"] != ""
	client := bili.New(cookies)

	video, err := client.VideoInfo(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("获取视频信息失败：%w", err)
	}
	log("视频：%s", video.Title)
	log("UP：%s ｜ 时长：%s ｜ 分P：%d", video.Owner, HumanDuration(video.Duration), video.Page)

	var cues []model.Cue
	var apiErr error
	usedLang := ""
	if engine != "ytdlp" {
		tracks, terr := client.SubtitleTracks(ctx, video)
		if terr != nil {
			apiErr = terr
			log("字幕接口出错：%v", terr)
		} else {
			if len(tracks) == 0 {
				log("API 返回的字幕轨道：无")
			} else {
				names := make([]string, 0, len(tracks))
				for _, t := range tracks {
					names = append(names, t.Lang+"("+t.LangDoc+")")
				}
				log("API 返回的字幕轨道：%s", strings.Join(names, "，"))
			}
			if tr, ok := pickTrack(tracks, c.SubLangs); ok {
				usedLang = tr.Lang
				log("字幕轨道：%s（%s）", tr.Lang, tr.LangDoc)
				cues, apiErr = bili.FetchCues(ctx, tr.URL)
				if apiErr != nil {
					log("下载字幕失败：%v", apiErr)
				}
			}
		}
	}

	if len(cues) == 0 && engine != "api" {
		log("API 没拿到字幕，改用 yt-dlp 兜底…")
		title, owner, cs, ferr := ytdlp.FetchSubtitles(ctx, resolveYtDlp(c), in.Raw, cookiePath, c.SubLangs)
		if ferr != nil {
			apiErr = ferr
			log("yt-dlp 兜底失败：%v", ferr)
		} else {
			cues = cs
			if title != "" {
				video.Title = title
			}
			if owner != "" {
				video.Owner = owner
			}
			usedLang = "yt-dlp"
			log("已用 yt-dlp 兜底拿到字幕")
		}
	}

	if len(cues) == 0 {
		hint := "这个视频没有可用字幕（当前已登录，登录态正常；可能是该视频没生成 AI 字幕）。"
		if !loggedIn {
			hint = "该视频没有可用字幕，或登录态已失效。\nB 站 AI 字幕必须登录才能拿。先运行 bili-sub login 扫码，或用 Chrome 扩展 Get cookies.txt LOCALLY 导出 bilibili.com 的 cookies，存到 " + cookiePath + "。"
		}
		if apiErr != nil {
			hint += "\n最后一条错误：" + apiErr.Error()
		}
		return nil, errors.New(hint)
	}

	text := transcript.Clean(cues)
	if text == "" {
		return nil, errors.New("字幕内容为空（可能只有弹幕）")
	}
	log("字幕：%d 条 cue ｜ %d 字 ｜ 语言：%s", len(cues), len([]rune(text)), usedLang)

	return &Fetched{
		Video:       video,
		Lang:        usedLang,
		Cues:        cues,
		Text:        text,
		CookiesPath: cookiePath,
		LoggedIn:    loggedIn,
	}, nil
}

// RunOptions 是完整流程的参数；空值表示用配置里的默认。
type RunOptions struct {
	FetchOptions
	Prompt            string // 直接给完整提示词模板，优先于 PromptFile
	PromptFile        string // 覆盖配置里的提示词文件
	Summarizer        string // api | opencode
	Model             string // API 模型
	OpencodeModel     string // opencode 模型
	OutDir            string // 输出目录
	OpenWith          string // typora | obsidian | none
	KeepSubtitle      bool
	IncludeTranscript bool
	OnExists          string // suffix | overwrite | skip
}

// Result 是完整流程的结果。
type Result struct {
	Fetched *Fetched
	Prompt  string
	Summary string
	Path    string
	SRTPath string
	Elapsed time.Duration
}

// Run 取字幕、总结、写 Markdown，并按配置决定要不要自动打开。
func Run(ctx context.Context, cfg *config.Config, o RunOptions, logf LogFunc) (*Result, error) {
	log := logfOrNop(logf)
	if cfg == nil {
		return nil, errors.New("配置为空")
	}
	start := time.Now()
	c := clone(cfg)
	if o.Summarizer != "" {
		c.Summarizer = o.Summarizer
	}
	if o.Model != "" {
		c.Model = o.Model
	}
	if o.OpencodeModel != "" {
		c.OpencodeModel = o.OpencodeModel
	}
	if o.OpenWith != "" {
		c.OpenWith = o.OpenWith
	}
	if o.OnExists != "" {
		c.OnExists = o.OnExists
	}
	if o.KeepSubtitle {
		c.KeepSubtitle = true
	}
	if o.IncludeTranscript {
		c.IncludeTranscript = true
	}

	fetched, err := Fetch(ctx, c, o.FetchOptions, log)
	if err != nil {
		return nil, err
	}
	res := &Result{Fetched: fetched}
	if c.KeepSubtitle {
		p, err := writeSRT(c, fetched.Video, fetched.Cues)
		if err != nil {
			log("保存原始字幕失败：%v", err)
		} else {
			res.SRTPath = p
			log("原始字幕：%s", p)
		}
	}

	prompt, err := BuildPrompt(c, o.Prompt, o.PromptFile, fetched.Video, fetched.Text)
	if err != nil {
		return nil, err
	}
	res.Prompt = prompt
	log("总结中（%s）…", summarizeLabel(c))
	summary, err := summarize.Summarize(ctx, c, summarize.Request{
		Title:           fetched.Video.Title,
		Owner:           fetched.Video.Owner,
		URL:             fetched.Video.URL,
		DurationSeconds: fetched.Video.Duration,
		Transcript:      fetched.Text,
		Prompt:          prompt,
	})
	if err != nil {
		return nil, fmt.Errorf("总结失败：%w", err)
	}
	summary = EnsureHeading(strings.TrimSpace(summary), fetched.Video.Title)
	if summary == "" {
		return nil, errors.New("模型返回了空内容")
	}
	res.Summary = summary
	log("总结完成：%d 字", len([]rune(summary)))

	bvid := fetched.Video.BVid
	if bvid == "" {
		bvid = fetched.Video.BVid
	}
	data := note.Data{
		Title:   fetched.Video.Title,
		Owner:   fetched.Video.Owner,
		Source:  fetched.Video.URL,
		BVid:    bvid,
		Date:    time.Now().Format("2006-01-02"),
		Summary: summary,
	}
	if c.IncludeTranscript {
		data.Transcript = fetched.Text
	}
	content, err := note.Render(c.Resolve(c.TemplateFile), data)
	if err != nil {
		return nil, fmt.Errorf("渲染笔记失败：%w", err)
	}

	outDir := c.Resolve(c.OutputDir)
	if o.OutDir != "" {
		outDir = o.OutDir
	}
	path, err := note.Save(outDir, note.FileName(fetched.Video.Owner, fetched.Video.Title), content, c.OnExists)
	if err != nil {
		return nil, fmt.Errorf("保存笔记失败：%w", err)
	}
	res.Path = path
	log("已保存：%s", path)

	if c.OpenWith != "" && c.OpenWith != "none" {
		if err := note.Open(path, c); err != nil {
			log("自动打开失败：%v", err)
		}
	}
	res.Elapsed = time.Since(start)
	return res, nil
}

// BuildPrompt 读提示词模板、填占位符；override 非空时直接用它当模板。
func BuildPrompt(cfg *config.Config, override, promptFile string, v *model.Video, text string) (string, error) {
	tmpl := ""
	if override != "" {
		tmpl = override
	} else {
		p := cfg.Resolve(cfg.PromptFile)
		if promptFile != "" {
			p = promptFile
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("读提示词文件失败（%s）：%w", p, err)
		}
		tmpl = string(b)
	}
	if !strings.Contains(tmpl, "{{transcript}}") {
		tmpl += "\n\n字幕文本：\n{{transcript}}"
	}
	rep := strings.NewReplacer(
		"{{title}}", v.Title,
		"{{owner}}", v.Owner,
		"{{source}}", v.URL,
		"{{bvid}}", v.BVid,
		"{{duration}}", HumanDuration(v.Duration),
		"{{transcript}}", text,
	)
	return rep.Replace(tmpl), nil
}

// EnsureHeading 保证总结第一行是一级标题；模型没给就补上视频标题。
func EnsureHeading(summary, title string) string {
	if summary == "" {
		return ""
	}
	first := summary
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = first[:i]
	}
	first = strings.TrimSpace(first)
	if strings.HasPrefix(first, "# ") || first == "#" {
		return summary
	}
	return "# " + title + "\n\n" + summary
}

// HumanDuration 把秒数写成中文时长。
func HumanDuration(sec int) string {
	if sec <= 0 {
		return "未知"
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%d小时%d分", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%d分%d秒", m, s)
	}
	return fmt.Sprintf("%d秒", s)
}

func pickTrack(tracks []model.SubtitleTrack, langs []string) (model.SubtitleTrack, bool) {
	for _, want := range langs {
		for _, t := range tracks {
			if strings.EqualFold(t.Lang, want) {
				return t, true
			}
		}
	}
	for _, t := range tracks {
		l := strings.ToLower(t.Lang)
		if strings.Contains(l, "zh") || strings.Contains(l, "ai-") {
			return t, true
		}
	}
	if len(tracks) > 0 {
		return tracks[0], true
	}
	return model.SubtitleTrack{}, false
}

// resolveYtDlp 找 yt-dlp：配置了路径就用配置的，找不到再退到 PATH；都没有就交给下游报错。
func resolveYtDlp(cfg *config.Config) string {
	raw := strings.TrimSpace(cfg.YtDlpPath)
	if raw != "" {
		if p := cfg.Resolve(raw); fileExists(p) {
			return p
		}
	}
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		return p
	}
	if raw != "" {
		return cfg.Resolve(raw)
	}
	return "yt-dlp"
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func writeSRT(cfg *config.Config, v *model.Video, cues []model.Cue) (string, error) {
	dir := cfg.Resolve("data/subs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := v.BVid
	if name == "" {
		name = "subtitle"
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-p%d.srt", name, v.Page))
	if err := os.WriteFile(path, []byte(transcript.ToSRT(cues)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func summarizeLabel(cfg *config.Config) string {
	switch cfg.Summarizer {
	case "opencode":
		return cfg.OpencodeModel
	default:
		return "DeepSeek API/" + cfg.Model
	}
}
