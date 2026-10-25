// Package config 负责配置结构、默认值、读写和相对路径解析。
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config 对应 config.json 的字段。JSON 里用驼峰命名，方便手改。
type Config struct {
	OutputDir         string   `json:"outputDir"`             // 笔记输出目录
	Summarizer        string   `json:"summarizer"`            // api | opencode
	Model             string   `json:"model"`                 // api 后端用的模型名
	OpencodeModel     string   `json:"opencodeModel"`         // opencode 后端用的模型名
	APIKey            string   `json:"apiKey"`                // 可空，回退环境变量 / opencode auth.json
	BaseURL           string   `json:"baseUrl"`               // OpenAI 兼容接口的 base URL
	PromptFile        string   `json:"promptFile"`            // 总结用的 prompt 模板
	TemplateFile      string   `json:"templateFile"`          // 笔记模板
	CookiesFile       string   `json:"cookiesFile"`           // B 站 cookies
	YtDlpPath         string   `json:"ytdlpPath"`             // yt-dlp.exe 路径
	OpenWith          string   `json:"openWith"`              // typora | obsidian | none
	TyporaPath        string   `json:"typoraPath"`            // Typora.exe 路径
	SubLangs          []string `json:"subLangs"`              // 字幕语言优先级
	KeepSubtitle      bool     `json:"keepSubtitle"`          // 是否保留原始 .srt
	IncludeTranscript bool     `json:"includeTranscript"`     // 是否把字幕塞进笔记
	OnExists          string   `json:"onExists"`              // suffix | overwrite | skip
	MaxChars          int      `json:"maxChars"`              // 长字幕分段阈值（rune 数）
	APITimeoutSeconds int      `json:"apiTimeoutSeconds"`     // 单次总结请求的超时秒数
	RequestTimeoutSec int      `json:"requestTimeoutSeconds"` // 单次 B 站请求的超时秒数

	// path 是当前配置文件的绝对路径，用来解析相对路径。
	// 它不写进 JSON；通过 Load/Save 设置，直接用结构体字面量构造时为空。
	path string
}

// Default 返回一份按契约填好默认值的配置。
func Default() *Config {
	return &Config{
		OutputDir:         `稿件`,
		Summarizer:        "api",
		Model:             "deepseek-flash",
		OpencodeModel:     "opencode/deepseek-v4.1-flash-free",
		APIKey:            "",
		BaseURL:           "https://api.deepseek.com/v1",
		PromptFile:        "prompts/default.md",
		TemplateFile:      "templates/note.md",
		CookiesFile:       "data/cookies.txt",
		YtDlpPath:         "",
		OpenWith:          "none",
		TyporaPath:        "",
		SubLangs:          []string{"ai-zh", "zh-Hans", "zh-CN", "zh", "ai-en", "en"},
		KeepSubtitle:      false,
		IncludeTranscript: false,
		OnExists:          "suffix",
		MaxChars:          300000,
		APITimeoutSeconds: 300,
		RequestTimeoutSec: 30,
	}
}

// DefaultPath 返回默认配置文件路径：exe 同目录的 config.json。
// go run / go test 的 exe 在临时构建目录里，这时退回当前工作目录。
func DefaultPath() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if !isTempBuildDir(dir) {
			return filepath.Join(dir, "config.json")
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "config.json")
	}
	return "config.json"
}

// isTempBuildDir 判断目录是不是临时目录（go run / go test 的构建产物放这里）。
func isTempBuildDir(dir string) bool {
	clean := strings.ToLower(filepath.Clean(dir))
	if strings.Contains(clean, "go-build") {
		return true
	}
	tmp := strings.ToLower(filepath.Clean(os.TempDir()))
	return clean == tmp || strings.HasPrefix(clean, tmp+string(filepath.Separator))
}

// Load 读取配置文件；文件不存在（或内容为空）时先写一份默认配置再返回。
// path 为空时用 DefaultPath。
func Load(path string) (*Config, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("解析配置路径 %s 失败: %w", path, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("读取配置 %s 失败: %w", abs, err)
		}
		c := Default()
		c.path = abs
		if err := c.Save(abs); err != nil {
			return nil, err
		}
		return c, nil
	}
	c := Default() // 先铺默认值，JSON 里缺的字段就保持默认
	if len(bytes.TrimSpace(data)) == 0 {
		c.path = abs
		if err := c.Save(abs); err != nil {
			return nil, err
		}
		return c, nil
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("解析配置 %s 失败: %w", abs, err)
	}
	c.path = abs
	return c, nil
}

// Save 把配置写到 path（自动建目录）。path 为空时先看当前配置的路径，再看 DefaultPath。
func (c *Config) Save(path string) error {
	if strings.TrimSpace(path) == "" {
		path = c.path
	}
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("解析配置路径 %s 失败: %w", path, err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	data = append(data, 10) // 末尾补一个换行符
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("创建配置目录 %s 失败: %w", filepath.Dir(abs), err)
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return fmt.Errorf("写入配置 %s 失败: %w", abs, err)
	}
	c.path = abs
	return nil
}

// Resolve 把相对路径按 config.json 所在目录解析；绝对路径原样返回（只做 Clean）。
func (c *Config) Resolve(p string) string {
	if strings.TrimSpace(p) == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(c.dir(), filepath.Clean(p))
}

// dir 返回配置所在目录；没通过 Load/Save 设置时退回 DefaultPath 所在目录。
func (c *Config) dir() string {
	if c.path != "" {
		return filepath.Dir(c.path)
	}
	return filepath.Dir(DefaultPath())
}
