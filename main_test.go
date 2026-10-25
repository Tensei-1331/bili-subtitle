package main

import (
	"strings"
	"testing"
)

func TestParseRunArgsJoinsSplitShareText(t *testing.T) {
	args := []string{
		"--open", "obsidian", "--auto-open", "--preset", "default", "--summarizer", "opencode",
		"【【20多岁心理学】468.", `"脑雾"形成的真正原因;`, "如何消除脑",
		"https://www.bilibili.com/video/BV1C1JY6JERw?vd_source=abc",
	}
	o, input, err := parseRunArgs(args)
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if o.open != "obsidian" || !o.autoOpen || o.preset != "default" || o.summarizer != "opencode" {
		t.Errorf("参数没解析对: %+v", o)
	}
	if !strings.Contains(input, "https://www.bilibili.com/video/BV1C1JY6JERw") {
		t.Errorf("输入里丢了链接: %q", input)
	}
	if !strings.Contains(input, "脑雾") || !strings.Contains(input, "如何消除脑") {
		t.Errorf("输入没有拼回整段文案: %q", input)
	}
}

func TestParseRunArgsFlagsAfterInput(t *testing.T) {
	o, input, err := parseRunArgs([]string{"BV1c5HK6CEPu", "--dry-run", "--verbose"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if input != "BV1c5HK6CEPu" || !o.dryRun || !o.verbose {
		t.Errorf("参数没解析对: input=%q opts=%+v", input, o)
	}
}
