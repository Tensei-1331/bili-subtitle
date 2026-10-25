package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"bili-subtitle/internal/config"
)

const (
	defaultOpencodeModel = "opencode/deepseek-v4.1-flash-free"
	// opencodeInstruction 是传给 opencode 的简短指令。
	// 故意用纯 ASCII：完整的中文 prompt 放在附件文件里，免得经过 Windows 命令行编码。
	opencodeInstruction = "Read the attached prompt file and follow it strictly. Output only the final Markdown body, without any explanation or code fences."
)

// ocEvent 是 opencode run --format json 输出的一行事件。
type ocEvent struct {
	Type string `json:"type"`
	Part struct {
		ID        string `json:"id"`
		MessageID string `json:"messageID"`
		Type      string `json:"type"`
		Text      string `json:"text"`
	} `json:"part"`
	Error struct {
		Name string `json:"name"`
		Data struct {
			Message string `json:"message"`
			Ref     string `json:"ref"`
		} `json:"data"`
	} `json:"error"`
}

// callOpencode 用本机的 opencode CLI 跑一次总结。
// 做法：把完整 prompt 写进临时目录的 prompt.md，用 -f 作为附件传进去，
// 命令行上只放一句简短英文指令（opencode 的 -f 是数组参数，消息参数必须写在 -f 前面）。
func callOpencode(ctx context.Context, cfg *config.Config, prompt string) (string, error) {
	bin, err := findOpencode()
	if err != nil {
		return "", err
	}
	model := defaultOpencodeModel
	if cfg != nil {
		if m := strings.TrimSpace(cfg.OpencodeModel); m != "" {
			model = m
		}
	}

	workDir, err := os.MkdirTemp("", "bili-sub-opencode-")
	if err != nil {
		return "", fmt.Errorf("创建 opencode 临时目录失败: %w", err)
	}
	defer os.RemoveAll(workDir)
	promptPath := filepath.Join(workDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte(prompt), 0o644); err != nil {
		return "", fmt.Errorf("写 opencode 临时 prompt 失败: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, requestTimeout(cfg))
	defer cancel()
	// 附件用绝对路径：opencode 解析相对路径时认的是 PWD 环境变量，不一定是 cmd.Dir，
	// 从 Git Bash 启动时 PWD 是 /d/... 这种 Unix 路径，相对路径会找不到文件。
	// cmd.Dir 指到临时目录，配合 cleanOpencodeEnv 去掉 PWD，让 opencode 把这里当项目目录，
	// 免得读到用户当前项目的 AGENTS.md。
	cmd := exec.Command(bin, "run", "--pure", "--format", "json", "-m", model, opencodeInstruction, "-f", promptPath)
	cmd.Dir = workDir
	cmd.Env = cleanOpencodeEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动 opencode 失败: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var runErr error
	select {
	case runErr = <-done:
	case <-runCtx.Done():
		// 只杀 cmd.exe 包装会留下 node 孙进程，按进程树结束。
		killProcessTree(cmd.Process.Pid)
		<-done
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("opencode 超过 %d 秒没返回，已中断（免费线路偶尔会卡住，可换 --opencode-model deepseek/deepseek-flash，或直接用默认 API 后端）", int(requestTimeout(cfg).Seconds()))
		}
		return "", fmt.Errorf("opencode 被取消: %v", runCtx.Err())
	}
	text, parseErr := parseOpencodeEvents(stdout.Bytes())
	if runErr != nil {
		if parseErr != nil {
			return "", fmt.Errorf("opencode 运行失败: %v；%w；stderr：%s", runErr, parseErr, snippet(stderr.Bytes()))
		}
		return "", fmt.Errorf("opencode 运行失败: %v；stderr：%s", runErr, snippet(stderr.Bytes()))
	}
	if parseErr != nil {
		if msg := snippet(stderr.Bytes()); msg != "(空响应)" {
			return "", fmt.Errorf("%w；stderr：%s", parseErr, msg)
		}
		return "", parseErr
	}
	return text, nil
}

// killProcessTree 用 taskkill /T 结束整棵进程树，避免 cmd.exe 包装被杀后 node 孙进程残留。
func killProcessTree(pid int) {
	if pid <= 0 {
		return
	}
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
}

// cleanOpencodeEnv 返回子进程环境变量：去掉 PWD / OLDPWD。
// 从 Git Bash 启动时 PWD 是 Unix 路径，opencode 会拿它当项目目录，
// 导致相对路径附件找不到，还可能误读别的项目的 AGENTS.md。
func cleanOpencodeEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		if strings.EqualFold(name, "PWD") || strings.EqualFold(name, "OLDPWD") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// findOpencode 找 opencode 可执行文件：先查 PATH，再退到 npm 全局安装目录。
func findOpencode() (string, error) {
	if p, err := exec.LookPath("opencode"); err == nil {
		return p, nil
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		p := filepath.Join(appData, "npm", "opencode.cmd")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("找不到 opencode 命令：确认已经用 npm 全局安装，并且安装目录在 PATH 里")
}

// parseOpencodeEvents 从 JSON 事件流里取最终 assistant 文本。
// 每个事件占一行；同一 part 可能被多次输出（流式快照），后到的覆盖先到的。
func parseOpencodeEvents(data []byte) (string, error) {
	type textPart struct {
		id        string
		messageID string
		text      string
	}
	var parts []textPart
	index := make(map[string]int)

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev ocEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue // 混进来的非 JSON 提示行直接跳过
		}
		switch ev.Type {
		case "error":
			return "", fmt.Errorf("opencode 报错: %s", ev.errorMessage())
		case "text":
			if ev.Part.Type != "text" || ev.Part.Text == "" {
				continue
			}
			key := ev.Part.ID
			if key == "" {
				key = fmt.Sprintf("part-%d", len(parts))
			}
			if i, ok := index[key]; ok {
				parts[i].text = ev.Part.Text
				continue
			}
			index[key] = len(parts)
			parts = append(parts, textPart{id: key, messageID: ev.Part.MessageID, text: ev.Part.Text})
		}
	}

	if len(parts) == 0 {
		return "", errors.New("opencode 没有输出任何文本事件（可能模型调用失败）")
	}

	// 只取最后一条消息的文本：多步运行时前面的文本可能是中间过程。
	finalMessageID := parts[len(parts)-1].messageID
	var b strings.Builder
	for _, p := range parts {
		if finalMessageID != "" && p.messageID != finalMessageID {
			continue
		}
		b.WriteString(p.text)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", errors.New("opencode 返回的最终文本是空的")
	}
	return out, nil
}

// errorMessage 取事件里的错误描述。
func (e ocEvent) errorMessage() string {
	if e.Error.Data.Message != "" {
		return e.Error.Data.Message
	}
	if e.Error.Name != "" {
		return e.Error.Name
	}
	return "未知错误"
}
