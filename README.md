# bili-sub

给一个 B 站视频链接（BV 号、完整 URL、b23.tv 短链都行），自动下载该视频的 AI 字幕，用 DeepSeek API 或本机 opencode 总结成 Markdown 稿件，并按配置用 Typora / Obsidian 打开。CLI 和本地 Web UI 两种用法。

## 技术栈

- **Go**：单文件静态 exe，无运行时依赖（Windows）
- **B 站 Web API + WBI 签名**：直连获取 AI 字幕，不下载视频
- **yt-dlp**（可选）：B 站接口失效时兜底取字幕
- **DeepSeek 官方 API**（默认模型 `deepseek-flash`）或本机 **opencode CLI**
- **net/http + go:embed**：`bili-sub gui` 的内嵌页面，不依赖 Node/Electron

## 准备

1. 装 Go 1.22+，在项目目录运行 `build.cmd`（或 `go build -trimpath -ldflags "-s -w" -o bili-sub.exe .`）。
2. 登录 B 站（AI 字幕必须登录才能拿）：

   ```bat
   bili-sub login --open-qr
   ```

   手机扫码后 cookies 存到 `data/cookies.txt`；也可以用浏览器扩展导出 Netscape 格式 cookies 覆盖该文件。
3. 选总结后端：
   - **DeepSeek API**：key 依次取 `config.json` 的 `apiKey`、环境变量 `DEEPSEEK_API_KEY`、`~/.local/share/opencode/auth.json`；
   - **opencode**：本机装好并登录 opencode，默认模型 `opencode/deepseek-v4.1-flash-free`。
4. （可选）yt-dlp：放 PATH 里即可，或在 `config.json` 的 `ytdlpPath` 填 exe 路径。

## 使用

```bat
bili-sub BV1xx411c7mD                          rem 下载字幕→总结→写稿件→Typora 打开
bili-sub https://b23.tv/xxxx --open obsidian   rem 短链也行，用 Obsidian 打开
bili-sub BV1xx411c7mD --dry-run --verbose      rem 只看字幕，不总结
bili-sub BV1xx411c7mD --summarizer opencode    rem 换 opencode 后端
bili-sub BV1xx411c7mD --prompt my-prompt.md    rem 换提示词
bili-sub gui                                   rem 本地 Web UI，自动开浏览器
bili-sub login --open-qr                       rem 扫码登录/续期
bili-sub config                                rem 查看当前配置
```

其他参数：`--out` 输出目录、`--model` API 模型、`--opencode-model` opencode 模型、`--engine auto|api|ytdlp` 字幕来源、`--langs` 语言优先级、`--keep-sub` 保留 .srt、`--include-transcript` 笔记附字幕、`--on-exists suffix|overwrite|skip`。完整列表看 `bili-sub help`。

## 配置

首次运行在 exe 同目录生成 `config.json`，常用字段：

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| outputDir | `稿件` | Markdown 输出目录（相对路径按 config.json 所在目录解析） |
| summarizer | `api` | `api` 或 `opencode` |
| model | `deepseek-flash` | DeepSeek API 模型 |
| opencodeModel | `opencode/deepseek-v4.1-flash-free` | opencode 模型 |
| apiKey | 空 | 空则依次读环境变量、opencode auth.json |
| baseUrl | `https://api.deepseek.com/v1` | OpenAI 兼容接口地址 |
| openWith | `none` | `typora` / `obsidian` / `none` |
| typoraPath | 空 | Typora.exe 路径，`openWith=typora` 时必填 |
| ytdlpPath | 空 | 空则从 PATH 找 yt-dlp |
| cookiesFile | `data/cookies.txt` | 登录态文件 |
| subLangs | `ai-zh,zh-Hans,zh-CN,zh,ai-en,en` | 字幕语言优先级 |
| keepSubtitle / includeTranscript | `false` | 保留原始 .srt / 笔记里附字幕 |
| onExists | `suffix` | 重名文件处理 |
| maxChars | `300000` | 超长字幕分段阈值 |

- 提示词：`prompts/default.md`，占位符 `{{title}} {{owner}} {{source}} {{bvid}} {{duration}} {{transcript}}`。
- 笔记模板：`templates/note.md`，输出 frontmatter 为 `type: 稿件 / source / up / date / bvid`。

## 示例

输入 25 分钟视频 `BV12qnt6VEM1`，拿到 `ai-zh` 字幕 644 条 / 7835 字，DeepSeek API 总结约 25–45 秒，输出文件形如 `稿件/UP主-视频标题.md`：

```markdown
---
type: 稿件
source: https://www.bilibili.com/video/BV12qnt6VEM1
up: UP主
date: 2026-10-26
bvid: BV12qnt6VEM1
---
# AI 提炼的标题

概述段落……

## 第一个主题

正文……
```

## 目录

```
main.go                  CLI 入口
internal/app             取字幕→总结→写笔记的共用流程
internal/bili            B 站接口、WBI 签名、二维码登录
internal/ytdlp           yt-dlp 兜底与 SRT 解析
internal/summarize       DeepSeek API / opencode 两个后端、长文分段
internal/note            模板渲染、文件名清洗、保存、外部打开
internal/config          配置结构
internal/transcript      字幕清洗
internal/web             GUI 的本地服务和内嵌页面
prompts/  templates/     提示词与笔记模板
```

## 更新

- yt-dlp：`yt-dlp -U`
- 本工具：`git pull` 后重新运行 `build.cmd`

## 注意

- B 站 AI 字幕需要登录；视频本身没有 AI 字幕时会明确报错。
- opencode 的免费线路（`opencode/deepseek-v4.1-flash-free`）被上游限流时可能长时间不返回，可换 `--opencode-model deepseek/deepseek-flash`，或直接用默认 API 后端。
- `data/cookies.txt` 等同于账号登录凭证，不要提交到版本库（已在 `.gitignore` 里）。
