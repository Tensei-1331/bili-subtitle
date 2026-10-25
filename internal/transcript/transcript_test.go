package transcript

import (
	"strings"
	"testing"

	"bili-subtitle/internal/model"
)

func TestCleanMergesSentences(t *testing.T) {
	cues := []model.Cue{
		{From: 0, To: 1, Text: "大家好。"},
		{From: 1, To: 2, Text: "今天聊一聊 AI"},
		{From: 2, To: 2.5, Text: "会不会取代程序员。"},
		{From: 2.5, To: 2.5, Text: "会不会取代程序员。"},
		{From: 8, To: 9, Text: "先说结论。"},
	}
	got := Clean(cues)
	want := "大家好。\n今天聊一聊 AI会不会取代程序员。\n\n先说结论。"
	if got != want {
		t.Fatalf("清理结果不对：\n got=%q\nwant=%q", got, want)
	}
}

func TestCleanEnglishSpace(t *testing.T) {
	cues := []model.Cue{
		{From: 0, To: 1, Text: "hello"},
		{From: 1, To: 2, Text: "world"},
	}
	if got := Clean(cues); got != "hello world" {
		t.Fatalf("英文应补空格，got=%q", got)
	}
}

func TestToSRT(t *testing.T) {
	cues := []model.Cue{{From: 1.5, To: 2.25, Text: " 测试  "}}
	got := ToSRT(cues)
	if !strings.Contains(got, "00:00:01,500 --> 00:00:02,250") || !strings.Contains(got, "测试") {
		t.Fatalf("SRT 输出不对：%q", got)
	}
}
