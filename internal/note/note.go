// Package note 负责把总结套进模板、生成文件名、保存笔记，并按配置用外部程序打开。
package note

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"bili-subtitle/internal/config"
)

// Data 是笔记模板里可用的字段。
type Data struct {
	Title, Owner, Source, BVid, Date string
	Summary                          string
	Transcript                       string
}

// maxFileNameBytes 是生成的文件名长度上限，给完整路径留余量（Windows 单个文件名上限 255 字节）。
const maxFileNameBytes = 200

// Render 读模板文件并替换 {{title}} 等占位符。
func Render(templatePath string, d Data) (string, error) {
	data, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("读取笔记模板 %s 失败: %w", templatePath, err)
	}
	tmpl := string(data)
	if strings.TrimSpace(tmpl) == "" {
		return "", fmt.Errorf("笔记模板 %s 是空的", templatePath)
	}
	replacer := strings.NewReplacer(
		"{{title}}", d.Title,
		"{{owner}}", d.Owner,
		"{{source}}", d.Source,
		"{{bvid}}", d.BVid,
		"{{date}}", d.Date,
		"{{summary}}", d.Summary,
		"{{transcript}}", d.Transcript,
	)
	return replacer.Replace(tmpl), nil
}

// FileName 生成 "UP主-标题.md"，清洗 Windows 非法字符并限制长度。
func FileName(owner, title string) string {
	owner = cleanPart(owner)
	if owner == "" {
		owner = "未知UP主"
	}
	title = cleanPart(title)
	if title == "" {
		title = "无标题"
	}
	owner = truncateRunes(owner, 24)
	title = truncateRunes(title, 60)

	build := func() string { return owner + "-" + title + ".md" }
	name := build()
	// 中文按 UTF-8 算字节，超上限就一点点砍标题
	for len(name) > maxFileNameBytes && utf8.RuneCountInString(title) > 1 {
		title = truncateRunes(title, utf8.RuneCountInString(title)-1)
		name = build()
	}
	title = strings.TrimRight(title, " .")
	if title == "" {
		title = "无标题"
	}
	return build()
}

// cleanPart 清洗文件名的一部分：替换非法字符、去掉控制字符、压缩空白、去掉首尾点和空格。
func cleanPart(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	lastUnderscore := false
	for _, r := range s {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		default:
			if unicode.IsSpace(r) {
				b.WriteByte(' ')
				lastUnderscore = false
				continue
			}
			if r < 0x20 || r == 0x7f {
				continue // 其他控制字符直接丢掉
			}
			b.WriteRune(r)
			lastUnderscore = false
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	out = strings.Trim(out, ". ")
	return out
}

// truncateRunes 按 rune 截断字符串。
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// Save 把内容写进 dir/filename，按 onExists 处理重名：
//
//	suffix    重名时自动加 (2)、(3)
//	overwrite 直接覆盖
//	skip      已存在就不写，返回已有路径
//
// 返回写入（或已存在）文件的绝对路径。
func Save(dir, filename, content, onExists string) (string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "", fmt.Errorf("文件名是空的")
	}
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("输出目录是空的")
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("解析输出目录 %s 失败: %w", dir, err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", fmt.Errorf("创建输出目录 %s 失败: %w", absDir, err)
	}
	path := filepath.Join(absDir, filename)
	switch strings.ToLower(strings.TrimSpace(onExists)) {
	case "overwrite":
		// 什么也不用做，下面直接写
	case "skip":
		if fileExists(path) {
			return path, nil
		}
	case "suffix", "":
		path = uniquePath(path)
	default:
		return "", fmt.Errorf("不支持的 onExists 取值 %q（可选 suffix / overwrite / skip）", onExists)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("写入笔记 %s 失败: %w", path, err)
	}
	return path, nil
}

// uniquePath 遇到重名时依次尝试 "name (2).md"、"name (3).md"。
func uniquePath(path string) string {
	if !fileExists(path) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 2; i < 10000; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if !fileExists(candidate) {
			return candidate
		}
	}
	return path
}

// fileExists 判断文件是否存在。
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Open 按配置用 Typora 或 Obsidian 打开笔记，none 时什么也不做。
// 起外部程序用 Start + Process.Release，不等它退出，不阻塞主流程。
func Open(path string, cfg *config.Config) error {
	cmd, err := openCommand(path, cfg)
	if err != nil {
		return err
	}
	if cmd == nil {
		return nil
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 %s 失败: %w", cmd.Path, err)
	}
	return cmd.Process.Release()
}

// openCommand 只构造打开命令，不启动（方便单测，也不会真弹窗）。
func openCommand(path string, cfg *config.Config) (*exec.Cmd, error) {
	if cfg == nil {
		return nil, fmt.Errorf("配置为空")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("解析笔记路径 %s 失败: %w", path, err)
	}
	switch strings.ToLower(strings.TrimSpace(cfg.OpenWith)) {
	case "", "none":
		return nil, nil
	case "typora":
		exe := strings.TrimSpace(cfg.TyporaPath)
		if exe == "" {
			return nil, fmt.Errorf("openWith=typora 但 typoraPath 是空的")
		}
		if _, err := os.Stat(exe); err != nil {
			return nil, fmt.Errorf("找不到 Typora：%s", exe)
		}
		return exec.Command(exe, abs), nil
	case "obsidian":
		return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", obsidianURI(abs)), nil
	default:
		return nil, fmt.Errorf("不支持的 openWith 取值 %q（可选 typora / obsidian / none）", cfg.OpenWith)
	}
}

// obsidianURI 生成 obsidian://open?path=<URL 编码的绝对路径>。
func obsidianURI(absPath string) string {
	return "obsidian://open?path=" + url.QueryEscape(absPath)
}
