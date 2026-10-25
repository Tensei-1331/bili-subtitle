package summarize

import (
	"context"
	"os"
	"testing"
	"time"
	"unicode/utf8"

	"bili-subtitle/internal/config"
)

// 真实联调，跑法：
//   BILI_SUB_LIVE=1 go test ./internal/summarize -run TestLive -v
//
// 需要网络；api 后端要能拿到 DeepSeek key（config.apiKey / DEEPSEEK_API_KEY /
// %USERPROFILE%\.local\share\opencode\auth.json 三者之一），opencode 后端要本机装了 opencode。

// liveTranscript 是一段约 300 字的假字幕，够触发一次完整总结。
const liveTranscript = "今天我们来聊聊 Go 语言的并发模型。Go 用 goroutine 做并发，goroutine 是运行时管理的用户态协程，" +
	"初始栈只有 2KB，还可以按需增长，所以同时开几千个也不费劲。goroutine 之间通信用 channel，channel 分有缓冲和无缓冲两种；" +
	"无缓冲 channel 的发送和接收必须同时就绪，相当于一次会合，自带同步效果。Go 的调度器是 GMP 模型，G 是 goroutine，" +
	"M 是系统线程，P 是处理器上下文，M 必须绑定一个 P 才能执行 G。从 Go 1.14 开始，调度器支持基于信号的抢占式调度，" +
	"长时间运行的 goroutine 也能被及时打断。很多人觉得 Go 的并发好写，关键就在这套模型：用通信代替共享内存，尽量少用锁，" +
	"把并发控制交给 channel 和 select。当然 goroutine 也不是越多越好，要控制并发度、避免泄漏，" +
	"必要时可以用带缓冲的 channel 当信号量来限流。"

func TestLiveAPI(t *testing.T) {
	if os.Getenv("BILI_SUB_LIVE") != "1" {
		t.Skip("设置 BILI_SUB_LIVE=1 才跑真实联调")
	}
	cfg := config.Default()
	cfg.Summarizer = "api"
	runLiveSummarize(t, cfg)
}

func TestLiveOpencode(t *testing.T) {
	if os.Getenv("BILI_SUB_LIVE") != "1" {
		t.Skip("设置 BILI_SUB_LIVE=1 才跑真实联调")
	}
	cfg := config.Default()
	cfg.Summarizer = "opencode"
	runLiveSummarize(t, cfg)
}

// TestLiveChunkedAPI 用真实 API 验证长字幕分段 + 合并流程（把 MaxChars 调小逼出分段）。
func TestLiveChunkedAPI(t *testing.T) {
	if os.Getenv("BILI_SUB_LIVE") != "1" {
		t.Skip("设置 BILI_SUB_LIVE=1 才跑真实联调")
	}
	cfg := config.Default()
	cfg.Summarizer = "api"
	cfg.MaxChars = 120
	if n := utf8.RuneCountInString(liveTranscript); n <= cfg.MaxChars {
		t.Fatalf("字幕 %d 字，没超过 %d，触发不了分段", n, cfg.MaxChars)
	}
	req := Request{
		Title:           "Go 并发模型入门",
		Owner:           "测试UP",
		URL:             "https://www.bilibili.com/video/BV1xx411c7mD",
		DurationSeconds: 600,
		Transcript:      liveTranscript,
		Prompt:          livePrompt(liveTranscript),
	}
	start := time.Now()
	out, err := Summarize(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("分段总结失败: %v", err)
	}
	runes := []rune(out)
	head := out
	if len(runes) > 200 {
		head = string(runes[:200])
	}
	t.Logf("分段总结耗时 %s，输出 %d 字\n前 200 字：\n%s", time.Since(start).Round(time.Millisecond), len(runes), head)
	if len(runes) < 100 {
		t.Errorf("合并后的终稿太短: %d 字", len(runes))
	}
}

// runLiveSummarize 跑一次真实总结，打印耗时和结果前 200 字。
func runLiveSummarize(t *testing.T, cfg *config.Config) {
	t.Helper()
	if n := utf8.RuneCountInString(liveTranscript); n < 280 {
		t.Fatalf("联调字幕只有 %d 字，太短了", n)
	}
	req := Request{
		Title:           "Go 并发模型入门",
		Owner:           "测试UP",
		URL:             "https://www.bilibili.com/video/BV1xx411c7mD",
		DurationSeconds: 600,
		Transcript:      liveTranscript,
		Prompt:          livePrompt(liveTranscript),
	}
	start := time.Now()
	out, err := Summarize(context.Background(), cfg, req)
	if err != nil {
		t.Fatalf("Summarize(%s) 失败: %v", cfg.Summarizer, err)
	}
	elapsed := time.Since(start)
	runes := []rune(out)
	head := out
	if len(runes) > 200 {
		head = string(runes[:200])
	}
	t.Logf("后端 %s，耗时 %s，输出 %d 字\n前 200 字：\n%s", cfg.Summarizer, elapsed.Round(time.Millisecond), len(runes), head)
	if len(runes) < 100 {
		t.Errorf("输出太短: %d 字", len(runes))
	}
}

// livePrompt 手动渲染一份和 prompts/default.md 类似的 prompt。
func livePrompt(transcript string) string {
	return "你是一名中文内容编辑，负责把 B 站视频的口语化字幕整理成一篇可读的稿件。\n\n" +
		"视频信息：\n- 标题：Go 并发模型入门\n- UP 主：测试UP\n- 链接：https://www.bilibili.com/video/BV1xx411c7mD\n- 时长：600 秒\n\n" +
		"要求：\n1. 用中文写作。\n2. 开头写 2-4 句概述。\n3. 正文用小标题分成 2-3 节。\n4. 全文 200-400 字。" +
		"5. 只输出 Markdown 正文，不要 frontmatter，不要用代码块包裹。\n\n字幕文本：\n" + transcript
}
