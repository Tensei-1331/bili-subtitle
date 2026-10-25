//go:build windows

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// readClipboardText 用 PowerShell 读剪贴板文本。
// 走子进程是为了避免在 Go 里直接碰 Win32 剪贴板 API（unsafe + 编码问题）。
func readClipboardText() (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"[Console]::OutputEncoding = [Text.Encoding]::UTF8; Get-Clipboard -Raw")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("读取剪贴板失败：%w", err)
	}
	text := strings.TrimSpace(strings.TrimSuffix(string(out), "\x00"))
	if text == "" {
		return "", errors.New("剪贴板里没有文本")
	}
	return text, nil
}
