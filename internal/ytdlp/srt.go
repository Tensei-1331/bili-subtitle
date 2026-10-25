package ytdlp

import (
	"html"
	"regexp"
	"strconv"
	"strings"

	"bili-subtitle/internal/model"
)

var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

// ParseSRT 解析 SRT/VTT 文本。两种格式结构一样：时间轴行加若干文本行，
// 差别只在时间戳分隔符（逗号/点）和各自的头部块。
func ParseSRT(data []byte) []model.Cue {
	text := strings.TrimPrefix(string(data), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")

	cues := make([]model.Cue, 0, len(lines)/3+1)
	for i := 0; i < len(lines); i++ {
		from, to, ok := parseTimingLine(lines[i])
		if !ok {
			continue // 序号、WEBVTT 头、NOTE/STYLE 块都不是时间轴，跳过
		}
		var textLines []string
		j := i + 1
		for ; j < len(lines); j++ {
			line := strings.TrimSpace(lines[j])
			if line == "" {
				break
			}
			textLines = append(textLines, line)
		}
		i = j
		content := cleanCueText(strings.Join(textLines, " "))
		if content == "" || to <= from {
			continue
		}
		cues = append(cues, model.Cue{From: from, To: to, Text: content})
	}
	return cues
}

func parseTimingLine(line string) (from, to float64, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(line), "-->", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	from, ok = parseTimestamp(parts[0])
	if !ok {
		return 0, 0, false
	}
	// VTT 的时间轴后面可能跟 align:start position:0% 之类的设置，只取第一个字段。
	fields := strings.Fields(strings.TrimSpace(parts[1]))
	if len(fields) == 0 {
		return 0, 0, false
	}
	to, ok = parseTimestamp(fields[0])
	if !ok {
		return 0, 0, false
	}
	return from, to, true
}

// parseTimestamp 支持 HH:MM:SS,mmm / HH:MM:SS.mmm / MM:SS.mmm 三种写法。
func parseTimestamp(raw string) (float64, bool) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), ",", ".")
	parts := strings.Split(cleaned, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil || seconds < 0 {
		return 0, false
	}
	total := seconds
	if len(parts) == 3 {
		hours, err := strconv.Atoi(parts[0])
		if err != nil || hours < 0 {
			return 0, false
		}
		minutes, err := strconv.Atoi(parts[1])
		if err != nil || minutes < 0 || minutes > 59 {
			return 0, false
		}
		total += float64(hours*3600 + minutes*60)
	} else {
		minutes, err := strconv.Atoi(parts[0])
		if err != nil || minutes < 0 {
			return 0, false
		}
		total += float64(minutes * 60)
	}
	return total, true
}

func cleanCueText(s string) string {
	s = htmlTagPattern.ReplaceAllString(s, "") // VTT 的 <c>、<v 说话人> 等标记
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}
