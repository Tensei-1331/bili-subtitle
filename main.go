// bili-sub：把 B 站视频的 AI 字幕抓下来，交给大模型总结成 Markdown 稿件。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bili-subtitle/internal/app"
	"bili-subtitle/internal/bili"
	"bili-subtitle/internal/config"
	"bili-subtitle/internal/web"

	"rsc.io/qr"
)

const version = "0.3.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "\n出错："+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return errors.New("缺少 B 站视频链接或 BV 号")
	}
	switch args[0] {
	case "login":
		return runLogin(args[1:])
	case "config":
		return runConfig(args[1:])
	case "gui":
		return runGUI(args[1:])
	case "version", "--version", "-v":
		fmt.Println("bili-sub " + version)
		return nil
	case "help", "--help", "-h":
		printUsage()
		return nil
	}
	return runFetch(args)
}

// ---------- 参数解析 ----------

type runOptions struct {
	config            string
	out               string
	open              string
	noOpen            bool
	summarizer        string
	model             string
	opencodeModel     string
	promptFile        string
	prompt            string
	preset            string
	engine            string
	langs             string
	cookies           string
	keepSub           bool
	includeTranscript bool
	onExists          string
	dryRun            bool
	verbose           bool
	autoOpen          bool
	autoOpenSet       bool
}

func (o runOptions) openWith() string {
	if o.noOpen {
		return "none"
	}
	return o.open
}

func (o runOptions) langsList() []string {
	if strings.TrimSpace(o.langs) == "" {
		return nil
	}
	parts := strings.FieldsFunc(o.langs, func(r rune) bool { return r == ',' || r == ' ' })
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func parseRunArgs(args []string) (runOptions, string, error) {
	o := runOptions{}
	value := map[string]*string{
		"config": &o.config, "out": &o.out, "open": &o.open,
		"summarizer": &o.summarizer, "model": &o.model, "opencode-model": &o.opencodeModel,
		"prompt-file": &o.promptFile, "prompt": &o.prompt, "preset": &o.preset, "engine": &o.engine,
		"langs": &o.langs, "cookies": &o.cookies, "on-exists": &o.onExists,
	}
	boolean := map[string]*bool{
		"no-open": &o.noOpen, "keep-sub": &o.keepSub,
		"include-transcript": &o.includeTranscript, "dry-run": &o.dryRun, "verbose": &o.verbose, "auto-open": &o.autoOpen,
	}
	var input string
	i := 0
	for i < len(args) {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			name := strings.TrimPrefix(a, "--")
			val, hasVal := "", false
			if k := strings.IndexByte(name, '='); k >= 0 {
				name, val, hasVal = name[:k], name[k+1:], true
			}
			if p, ok := boolean[name]; ok {
				if hasVal {
					v, err := strconv.ParseBool(val)
					if err != nil {
						return o, "", fmt.Errorf("参数 --%s 的值 %q 不是布尔值", name, val)
					}
					*p = v
				} else {
					*p = true
				}
				if name == "auto-open" {
					o.autoOpenSet = true
				}
				i++
				continue
			}
			if p, ok := value[name]; ok {
				if !hasVal {
					if i+1 >= len(args) {
						return o, "", fmt.Errorf("参数 --%s 缺少值", name)
					}
					i++
					val = args[i]
				}
				*p = val
				i++
				continue
			}
			return o, "", fmt.Errorf("未知参数 --%s（用 bili-sub help 看用法）", name)
		}
		if input != "" {
			return o, "", fmt.Errorf("只能传一个视频链接，多出来的是：%s", a)
		}
		input = a
		i++
	}
	return o, input, nil
}

// ---------- 主流程 ----------

func runFetch(args []string) error {
	start := time.Now()
	o, input, err := parseRunArgs(args)
	if err != nil {
		return err
	}
	if input == "" {
		return errors.New("缺少 B 站视频链接或 BV 号")
	}
	cfg, _, err := loadConfig(o.config)
	if err != nil {
		return err
	}
	if o.preset != "" {
		p, err := presetPath(cfg, o.preset)
		if err != nil {
			return err
		}
		o.promptFile = p
	}
	ctx := context.Background()
	logf := func(format string, a ...any) { fmt.Printf(format+"\n", a...) }
	fetchOpts := app.FetchOptions{Input: input, Engine: o.engine, Cookies: o.cookies, Langs: o.langsList()}

	if o.dryRun {
		f, err := app.Fetch(ctx, cfg, fetchOpts, logf)
		if err != nil {
			return err
		}
		fmt.Printf("\n预览（前 500 字）：\n%s\n", preview(f.Text, 500))
		return nil
	}

	var autoOpen *bool
	if o.autoOpenSet {
		autoOpen = &o.autoOpen
	}
	_, err = app.Run(ctx, cfg, app.RunOptions{
		FetchOptions:      fetchOpts,
		Prompt:            o.prompt,
		PromptFile:        o.promptFile,
		Summarizer:        o.summarizer,
		Model:             o.model,
		OpencodeModel:     o.opencodeModel,
		OutDir:            o.out,
		OpenWith:          o.openWith(),
		AutoOpen:          autoOpen,
		KeepSubtitle:      o.keepSub,
		IncludeTranscript: o.includeTranscript,
		OnExists:          o.onExists,
	}, logf)
	if err != nil {
		return err
	}
	fmt.Printf("完成，用时 %s\n", time.Since(start).Round(10*time.Millisecond))
	return nil
}

// ---------- gui ----------

func runGUI(args []string) error {
	cfgPath := ""
	port := 0
	openBrowser := true
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			if i+1 >= len(args) {
				return errors.New("--config 缺少值")
			}
			i++
			cfgPath = args[i]
		case "--port":
			if i+1 >= len(args) {
				return errors.New("--port 缺少值")
			}
			i++
			p, err := strconv.Atoi(args[i])
			if err != nil || p < 0 || p > 65535 {
				return fmt.Errorf("--port 的值 %q 不是合法端口", args[i])
			}
			port = p
		case "--no-open":
			openBrowser = false
		default:
			return fmt.Errorf("gui 不认识的参数：%s", args[i])
		}
	}
	cfg, resolvedPath, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	srv := web.New(cfg, resolvedPath)
	return srv.Run(port, openBrowser)
}

// ---------- login / config ----------

func runLogin(args []string) error {
	cfgPath, cookiesPath, verbose, openQR := "", "", false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			if i+1 < len(args) {
				i++
				cfgPath = args[i]
			}
		case "--cookies":
			if i+1 < len(args) {
				i++
				cookiesPath = args[i]
			}
		case "--verbose":
			verbose = true
		case "--open-qr":
			openQR = true
		default:
			return fmt.Errorf("login 不认识的参数：%s", args[i])
		}
	}
	cfg, _, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	path := cfg.Resolve(cfg.CookiesFile)
	if cookiesPath != "" {
		path = cookiesPath
	}

	ctx := context.Background()
	url, key, err := bili.QRGenerate(ctx)
	if err != nil {
		return fmt.Errorf("申请登录二维码失败：%w", err)
	}
	fmt.Println("用 B 站手机 App 扫码登录（二维码 3 分钟内有效）")
	bili.RenderQRTerminal(url)

	// 终端二维码在部分终端里会糊，顺手存一张 PNG，扫码更方便。
	pngPath := cfg.Resolve("data/login-qr.png")
	if err := saveQRPNG(pngPath, url); err != nil {
		fmt.Printf("生成二维码图片失败：%v\n", err)
	} else {
		fmt.Printf("二维码图片：%s\n", pngPath)
		if openQR {
			_ = exec.Command("cmd", "/c", "start", "", pngPath).Start()
		}
	}

	deadline := time.Now().Add(3 * time.Minute)
	last := -1
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		status, msg, cookies, err := bili.QRPoll(ctx, key)
		if err != nil {
			if verbose {
				fmt.Printf("轮询失败：%v\n", err)
			}
			continue
		}
		if status != last {
			if msg != "" {
				fmt.Println(msg)
			}
			last = status
		}
		if status == 0 {
			saved, err := bili.SaveCookiesFile(path, cookies)
			if err != nil {
				return fmt.Errorf("保存 cookies 失败：%w", err)
			}
			fmt.Printf("登录成功，已保存 %d 个 cookies 到：%s\n", len(saved), path)
			return nil
		}
		if status == 86038 {
			return errors.New("二维码已过期，请重新运行 bili-sub login")
		}
	}
	return errors.New("等待扫码超时，请重新运行 bili-sub login")
}

func runConfig(args []string) error {
	cfgPath := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			if i+1 < len(args) {
				i++
				cfgPath = args[i]
			}
		case "init":
		default:
			return fmt.Errorf("config 不认识的参数：%s", args[i])
		}
	}
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("加载配置失败：%w", err)
	}
	view := *cfg
	if view.APIKey != "" {
		view.APIKey = "***"
	}
	b, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("配置文件：%s\n%s\n", cfgPath, string(b))
	return nil
}

// ---------- 辅助 ----------

func loadConfig(path string) (*config.Config, string, error) {
	if path == "" {
		path = config.DefaultPath()
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, path, fmt.Errorf("读取配置失败（%s）：%w", path, err)
	}
	return cfg, path, nil
}

// presetPath 把 --preset 的名字解析成 prompts 目录里的文件。
func presetPath(cfg *config.Config, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", errors.New("--preset 名字不合法")
	}
	if filepath.Base(name) != name {
		return "", fmt.Errorf("预设名不能带路径：%s", name)
	}
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		name += ".md"
	}
	dir := filepath.Dir(cfg.Resolve(cfg.PromptFile))
	full := filepath.Join(dir, name)
	if _, err := os.Stat(full); err != nil {
		return "", fmt.Errorf("找不到预设 %s（应放在 %s）", name, dir)
	}
	return full, nil
}

// saveQRPNG 把二维码内容渲染成 PNG，扫码时比终端字符更清楚。
func saveQRPNG(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	code, err := qr.Encode(content, qr.M)
	if err != nil {
		return err
	}
	return os.WriteFile(path, code.PNG(), 0o644)
}

func preview(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func printUsage() {
	fmt.Println(strings.Join([]string{
		"bili-sub — B站视频字幕 → AI 总结 → Markdown 稿件",
		"",
		"用法：",
		"  bili-sub <BV号|av号|视频链接|b23.tv短链> [选项]",
		"  bili-sub gui [--port 18080] [--no-open]   启动本地图形界面",
		"  bili-sub login [--cookies <路径>] [--open-qr]  扫码登录 B 站，保存 cookies",
		"  bili-sub config [--config <路径>]      查看当前配置",
		"  bili-sub version",
		"",
		"选项：",
		"  --out <目录>             输出目录（覆盖配置）",
		"  --open <typora|obsidian|none>  写完后用指定程序打开",
		"  --auto-open              写完自动打开（目标取配置 openWith）",
		"  --no-open                不自动打开",
		"  --summarizer <api|opencode>   用哪个后端总结",
		"  --model <名字>           API 模型（默认 deepseek-flash）",
		"  --opencode-model <名字>  opencode 模型（默认 opencode/deepseek-v4.1-flash-free）",
		"  --prompt-file <路径>     自定义提示词文件",
		"  --preset <名字>          用 prompts/<名字>.md 预设",
		"  --prompt <文本>          直接给提示词（覆盖模板文件）",
		"  --engine <auto|api|ytdlp>     字幕来源：自动、仅 API、仅 yt-dlp",
		"  --langs <列表>           字幕语言优先级，逗号分隔",
		"  --cookies <路径>         cookies.txt 路径",
		"  --keep-sub               保留原始 .srt 到 data/subs/",
		"  --include-transcript     把字幕正文附进笔记",
		"  --on-exists <suffix|overwrite|skip>  同名文件怎么处理",
		"  --dry-run                只下字幕并预览，不总结",
		"  --verbose                打印详细过程",
		"  --config <路径>          指定配置文件",
	}, "\n"))
}
