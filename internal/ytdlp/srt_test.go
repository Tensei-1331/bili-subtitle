package ytdlp

import "testing"

func TestParseSRT(t *testing.T) {
	data := "\ufeff1\r\n00:00:01,000 --> 00:00:03,500\r\n第一句\r\n\r\n2\r\n00:00:04,000 --> 00:00:06,000\r\n第二句\r\n换行继续\r\n\r\n"
	cues := ParseSRT([]byte(data))
	if len(cues) != 2 {
		t.Fatalf("应该解析出 2 条字幕，实际 %d：%+v", len(cues), cues)
	}
	if cues[0].From != 1 || cues[0].To != 3.5 || cues[0].Text != "第一句" {
		t.Fatalf("第一条不对：%+v", cues[0])
	}
	if cues[1].From != 4 || cues[1].To != 6 || cues[1].Text != "第二句 换行继续" {
		t.Fatalf("第二条不对：%+v", cues[1])
	}
}

func TestParseVTT(t *testing.T) {
	data := "WEBVTT\n\nNOTE 这是注释块\n不要解析这里\n\nSTYLE\n::cue { color: red }\n\ncue-1\n00:00:00.500 --> 00:00:02.000 align:start position:0%\n<v 说话人>你好 &amp; 再见</v>\n\n00:00:03.000 --> 00:00:04.000\n&lt;哈哈&gt;\n\n"
	cues := ParseSRT([]byte(data))
	if len(cues) != 2 {
		t.Fatalf("应该解析出 2 条字幕，实际 %d：%+v", len(cues), cues)
	}
	if cues[0].From != 0.5 || cues[0].To != 2 || cues[0].Text != "你好 & 再见" {
		t.Fatalf("第一条不对：%+v", cues[0])
	}
	if cues[1].Text != "<哈哈>" {
		t.Fatalf("HTML 实体没有还原：%+v", cues[1])
	}
}

func TestParseSRTSkipsBrokenCues(t *testing.T) {
	data := "1\n00:00:05,000 --> 00:00:02,000\n时间倒流\n\n2\n坏时间轴\n\n3\n00:00:01,000 --> 00:00:02,000\n\n"
	if cues := ParseSRT([]byte(data)); len(cues) != 0 {
		t.Fatalf("坏字幕应该全被丢掉，实际 %+v", cues)
	}
}

func TestParseTimestamp(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"00:00:01,500", 1.5, true},
		{"01:02:03.250", 3723.25, true},
		{"02:03.5", 123.5, true},
		{"坏", 0, false},
		{"00:00:xx", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseTimestamp(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("parseTimestamp(%q)=(%v,%v)，期望 (%v,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
