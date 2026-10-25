package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MarshalCommented 生成带注释的 config.json 内容。
// 注释用 // 写在字段上方，读取时会先去掉注释再解析。
func (c *Config) MarshalCommented() ([]byte, error) {
	js := func(s string) string {
		b, _ := json.Marshal(s)
		return string(b)
	}
	langs := make([]string, 0, len(c.SubLangs))
	for _, l := range c.SubLangs {
		langs = append(langs, js(l))
	}

	var b strings.Builder
	b.WriteString("// bili-sub 配置：删掉的字段会用默认值；支持 // 和 /* */ 注释。\n")
	b.WriteString("// 保存配置（GUI 或 bili-sub config init）时会按这个格式重写。\n")
	b.WriteString("{\n")
	field := func(comment, key, value string, comma bool) {
		fmt.Fprintf(&b, "  // %s\n  %q: %s", comment, key, value)
		if comma {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	field("笔记输出目录；相对路径按本文件所在目录解析", "outputDir", js(c.OutputDir), true)
	field("总结方式：api 用 OpenAI 兼容接口，opencode 用 OpenCode Go 的 API", "summarizer", js(c.Summarizer), true)
	field("API 模型名（OpenAI 兼容接口，默认 DeepSeek）", "model", js(c.Model), true)
	field("OpenCode Go 模型（默认 deepseek-v4.1-flash）", "opencodeModel", js(c.OpencodeModel), true)
	field("OpenCode Go 的 API key；留空则读环境变量 OPENCODE_GO_API_KEY", "opencodeApiKey", js(c.OpencodeAPIKey), true)
	field("OpenCode Go 的 OpenAI 兼容基址（换 Zen 线路时改这里）", "opencodeBaseUrl", js(c.OpencodeBaseURL), true)
	field("API key；留空则依次读环境变量 DEEPSEEK_API_KEY、OPENAI_API_KEY、opencode auth.json", "apiKey", js(c.APIKey), true)
	field("OpenAI 兼容接口的 base URL，可换成其他服务", "baseUrl", js(c.BaseURL), true)
	field("默认提示词预设文件", "promptFile", js(c.PromptFile), true)
	field("笔记模板文件", "templateFile", js(c.TemplateFile), true)
	field("B 站 cookies 文件", "cookiesFile", js(c.CookiesFile), true)
	field("yt-dlp.exe 路径；留空则从 PATH 里找", "ytdlpPath", js(c.YtDlpPath), true)
	field("写完后打开方式：typora / obsidian / none", "openWith", js(c.OpenWith), true)
	field("写完是否自动打开（配合 openWith）", "autoOpen", fmt.Sprintf("%t", c.AutoOpen), true)
	field("Typora.exe 路径；openWith=typora 时必填", "typoraPath", js(c.TyporaPath), true)
	field("字幕语言优先级", "subLangs", "["+strings.Join(langs, ", ")+"]", true)
	field("是否保留原始 .srt 到 data/subs/", "keepSubtitle", fmt.Sprintf("%t", c.KeepSubtitle), true)
	field("是否把字幕正文附进笔记", "includeTranscript", fmt.Sprintf("%t", c.IncludeTranscript), true)
	field("同名文件处理：suffix / overwrite / skip", "onExists", js(c.OnExists), true)
	field("长字幕分段阈值（字符数）", "maxChars", fmt.Sprintf("%d", c.MaxChars), true)
	field("单次总结请求超时（秒）", "apiTimeoutSeconds", fmt.Sprintf("%d", c.APITimeoutSeconds), true)
	field("单次 B 站请求超时（秒）", "requestTimeoutSeconds", fmt.Sprintf("%d", c.RequestTimeoutSec), false)
	b.WriteString("}\n")
	return []byte(b.String()), nil
}
