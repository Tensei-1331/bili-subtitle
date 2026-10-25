//go:build windows

package main

import "syscall"

// 控制台按 UTF-8 输出，避免中文在 cmd.exe 里变成乱码。
func init() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleOutputCP := kernel32.NewProc("SetConsoleOutputCP")
	_, _, _ = setConsoleOutputCP.Call(65001)
}
