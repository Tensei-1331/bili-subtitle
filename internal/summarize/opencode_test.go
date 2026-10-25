package summarize

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bili-subtitle/internal/config"
)

// mustMarshalEvent 把事件编码成一行 JSON。
func mustMarshalEvent(t *testing.T, ev ocEvent) string {
	t.Helper()
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("编码事件失败: %v", err)
	}
	return string(data)
}

// textEvent 造一条 text 事件。
func textEvent(t *testing.T, messageID, partID, text string) string {
	t.Helper()
	ev := ocEvent{Type: "text"}
	ev.Part.ID = partID
	ev.Part.MessageID = messageID
	ev.Part.Type = "text"
	ev.Part.Text = text
	return mustMarshalEvent(t, ev)
}

func TestParseOpencodeEventsRealSample(t *testing.T) {
	// 结构照真实 opencode run --pure --format json 的输出来
	lines := []string{
		mustMarshalEvent(t, ocEvent{Type: "step_start"}),
		textEvent(t, "msg_1", "prt_1", "## 概述\n\n这是整理后的正文。"),
		mustMarshalEvent(t, ocEvent{Type: "step_finish"}),
	}
	got, err := parseOpencodeEvents([]byte(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	want := "## 概述\n\n这是整理后的正文。"
	if got != want {
		t.Errorf("结果 = %q, 想要 %q", got, want)
	}
}

func TestParseOpencodeEventsMultipleParts(t *testing.T) {
	lines := []string{
		textEvent(t, "msg_1", "prt_1", "第一段。"),
		textEvent(t, "msg_1", "prt_2", "第二段。"),
	}
	got, err := parseOpencodeEvents([]byte(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got != "第一段。第二段。" {
		t.Errorf("结果 = %q", got)
	}
}

func TestParseOpencodeEventsOnlyLastMessage(t *testing.T) {
	// 两个 message：前面的当中间过程，只取最后一条
	lines := []string{
		textEvent(t, "msg_1", "prt_1", "中间过程，不算。"),
		textEvent(t, "msg_2", "prt_2", "最终答案"),
	}
	got, err := parseOpencodeEvents([]byte(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got != "最终答案" {
		t.Errorf("结果 = %q", got)
	}
}

func TestParseOpencodeEventsStreamingSnapshot(t *testing.T) {
	// 同一个 part 多次输出时，后到的快照覆盖先到的，不能拼接重复
	lines := []string{
		textEvent(t, "msg_1", "prt_1", "半截"),
		textEvent(t, "msg_1", "prt_1", "完整内容"),
	}
	got, err := parseOpencodeEvents([]byte(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got != "完整内容" {
		t.Errorf("结果 = %q", got)
	}
}

func TestParseOpencodeEventsSkipsNoise(t *testing.T) {
	lines := []string{
		"[pty.loader] bun-pty loaded successfully",
		"not json at all",
		textEvent(t, "msg_1", "prt_1", "正文"),
	}
	got, err := parseOpencodeEvents([]byte(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got != "正文" {
		t.Errorf("结果 = %q", got)
	}
}

func TestParseOpencodeEventsErrorEvent(t *testing.T) {
	ev := ocEvent{Type: "error"}
	ev.Error.Name = "UnknownError"
	ev.Error.Data.Message = "Unexpected server error"
	line := mustMarshalEvent(t, ev)
	_, err := parseOpencodeEvents([]byte(line + "\n"))
	if err == nil {
		t.Fatal("错误事件应该返回错误")
	}
	if !strings.Contains(err.Error(), "Unexpected server error") {
		t.Errorf("错误信息没带出事件内容: %v", err)
	}
}

func TestParseOpencodeEventsNoText(t *testing.T) {
	_, err := parseOpencodeEvents([]byte("{\"type\":\"step_start\"}\n{\"type\":\"step_finish\"}\n"))
	if err == nil || !strings.Contains(err.Error(), "没有输出任何文本") {
		t.Errorf("没有文本应该报错, 得到 %v", err)
	}
	if _, err := parseOpencodeEvents(nil); err == nil {
		t.Error("空输出应该报错")
	}
}

// installFakeOpencode 在临时目录放一个假的 opencode.cmd，并把它加到 PATH 最前面。
// script 里可以用 %~dp0 指到临时目录，用相对路径读 prompt.md。
func installFakeOpencode(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.cmd")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatalf("写假 opencode 失败: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func TestCallOpencodeWithFakeBinary(t *testing.T) {
	binDir := installFakeOpencode(t, "@echo off\r\n"+
		"setlocal\r\n"+
		"echo %* > \"%~dp0args.txt\"\r\n"+
		"if exist prompt.md copy /y prompt.md \"%~dp0seen_prompt.md\" >nul\r\n"+
		"if defined PWD echo %PWD% > \"%~dp0pwd_env.txt\"\r\n"+
		"echo {\"type\":\"text\",\"timestamp\":1,\"sessionID\":\"ses_fake\",\"part\":{\"id\":\"prt_fake\",\"messageID\":\"msg_fake\",\"sessionID\":\"ses_fake\",\"type\":\"text\",\"text\":\"FAKE_OK\"}}\r\n"+
		"exit /b 0\r\n")

	cfg := config.Default()
	cfg.APITimeoutSeconds = 30
	prompt := "这是完整的测试 prompt\n字幕：今天聊 Go 并发模型。"
	out, err := callOpencode(context.Background(), cfg, prompt)
	if err != nil {
		t.Fatalf("callOpencode: %v", err)
	}
	if out != "FAKE_OK" {
		t.Errorf("结果 = %q, 想要 FAKE_OK", out)
	}

	argsRaw, err := os.ReadFile(filepath.Join(binDir, "args.txt"))
	if err != nil {
		t.Fatalf("假 opencode 没被调用: %v", err)
	}
	args := string(argsRaw)
	for _, want := range []string{"run", "--pure", "--format", "json", "-m", "opencode/deepseek-v4.1-flash-free", "-f "} {
		if !strings.Contains(args, want) {
			t.Errorf("命令行缺少 %q: %s", want, args)
		}
	}
	// 附件必须是绝对路径（opencode 用 PWD 解析相对路径，Git Bash 下会出错）
	if !strings.Contains(args, ":\\") || !strings.Contains(args, "prompt.md") {
		t.Errorf("-f 应该是绝对路径: %s", args)
	}
	// -f 是数组参数，消息参数必须写在它前面，否则会被 yargs 当成第二个附件
	instIdx := strings.Index(args, "Read the attached prompt file")
	fIdx := strings.LastIndex(args, "-f ")
	if instIdx < 0 || fIdx < 0 || instIdx > fIdx {
		t.Errorf("消息参数应该在 -f 前面: %s", args)
	}
	// 子进程环境里不该带 PWD / OLDPWD，否则 opencode 可能把别的目录当项目根
	if _, err := os.Stat(filepath.Join(binDir, "pwd_env.txt")); err == nil {
		data, _ := os.ReadFile(filepath.Join(binDir, "pwd_env.txt"))
		t.Errorf("子进程里还有 PWD 环境变量: %q", data)
	}

	seen, err := os.ReadFile(filepath.Join(binDir, "seen_prompt.md"))
	if err != nil {
		t.Fatalf("假 opencode 没读到 prompt 文件: %v", err)
	}
	if string(seen) != prompt {
		t.Errorf("附件内容不一致:\n得到 %q\n想要 %q", seen, prompt)
	}
}

func TestCallOpencodeErrorEvent(t *testing.T) {
	installFakeOpencode(t, "@echo off\r\n"+
		"echo {\"type\":\"error\",\"timestamp\":1,\"sessionID\":\"ses_fake\",\"error\":{\"name\":\"UnknownError\",\"data\":{\"message\":\"fake upstream error\",\"ref\":\"err_x\"}}}\r\n"+
		"exit /b 1\r\n")

	cfg := config.Default()
	cfg.APITimeoutSeconds = 30
	if _, err := callOpencode(context.Background(), cfg, "prompt"); err == nil {
		t.Fatal("假 opencode 报错时应该返回错误")
	} else if !strings.Contains(err.Error(), "fake upstream error") {
		t.Errorf("错误信息没带出事件内容: %v", err)
	}
}

func TestFindOpencodeNotFound(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("APPDATA", t.TempDir())
	if _, err := findOpencode(); err == nil {
		t.Fatal("PATH 和 npm 目录都没有时应该报错")
	} else if !strings.Contains(err.Error(), "找不到 opencode") {
		t.Errorf("错误信息不明确: %v", err)
	}
}
