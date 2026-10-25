//go:build !windows

package main

import "errors"

func readClipboardText() (string, error) {
	return "", errors.New("读剪贴板只支持 Windows")
}
