// Package transcript 把字幕 cue 序列整理成适合喂给模型的纯文本。
package transcript

import (
	"fmt"
	"strings"

	"bili-subtitle/internal/model"
)

// Clean 合并字幕：去掉空行和连续重复、按句子断行、停顿较长处空行分段。
// B站 AI 字幕每条的粒度通常是一小句，直接拼接会没有段落，这里按标点和停顿重建结构。
func Clean(cues []model.Cue) string {
	var b strings.Builder
	var prev string
	prevEnd := false
	var prevTo float64
	for _, c := range cues {
		t := strings.TrimSpace(c.Text)
		if t == "" {
			continue
		}
		t = strings.Join(strings.Fields(t), " ")
		if t == prev {
			continue
		}
		// 两条之间隔了 2 秒以上，多半换了一段：前面已有句尾换行就补一个空行，否则直接空行。
		if b.Len() > 0 && c.From-prevTo > 2.0 {
			if prevEnd {
				b.WriteString("\n")
			} else {
				b.WriteString("\n\n")
			}
		}
		// 英文/数字相接时补空格，避免两个词粘在一起。
		if b.Len() > 0 && needsSpace(lastRune(b.String()), firstRune(t)) {
			b.WriteString(" ")
		}
		b.WriteString(t)
		prev = t
		prevTo = c.To
		if endsSentence(t) {
			b.WriteString("\n")
			prevEnd = true
		} else {
			prevEnd = false
		}
	}
	return strings.TrimSpace(b.String())
}

// ToSRT 把 cue 还原成 SRT，保留时间轴。
func ToSRT(cues []model.Cue) string {
	var b strings.Builder
	n := 0
	for _, c := range cues {
		text := strings.TrimSpace(strings.Join(strings.Fields(c.Text), " "))
		if text == "" {
			continue
		}
		n++
		fmt.Fprintf(&b, "%d\r\n%s --> %s\r\n%s\r\n\r\n", n, srtTime(c.From), srtTime(c.To), text)
	}
	return b.String()
}

func srtTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	ms := int(sec*1000 + 0.5)
	h := ms / 3600000
	ms -= h * 3600000
	m := ms / 60000
	ms -= m * 60000
	s := ms / 1000
	ms -= s * 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

func endsSentence(s string) bool {
	r := []rune(s)
	if len(r) == 0 {
		return false
	}
	switch r[len(r)-1] {
	case '。', '！', '？', '!', '?', '；', ';', '…', '」', '”', '）', ')', '】', ']', '"':
		return true
	}
	return false
}

func lastRune(s string) rune {
	r := []rune(s)
	if len(r) == 0 {
		return 0
	}
	return r[len(r)-1]
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func needsSpace(a, b rune) bool {
	if a == 0 || b == 0 {
		return false
	}
	aWord := (a >= 'a' && a <= 'z') || (a >= 'A' && a <= 'Z') || (a >= '0' && a <= '9')
	bWord := (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
	return aWord && bWord
}
