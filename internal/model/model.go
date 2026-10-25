// Package model 存放跨包共享的简单数据类型。
package model

// Video 是 B 站视频的基本元数据。
type Video struct {
	BVid     string // BV 号，如 BV1xx411c7mD
	AID      string // avid 数字部分
	CID      string // 当前分 P 的 cid
	Title    string // 视频标题
	Owner    string // UP 主昵称
	Duration int    // 时长（秒）
	Page     int    // 分 P 序号，从 1 开始
	URL      string // 规范化后的视频链接
}

// Cue 是一条字幕，时间单位为秒。
type Cue struct {
	From float64
	To   float64
	Text string
}

// SubtitleTrack 是一条可用字幕轨道。
type SubtitleTrack struct {
	Lang    string // 如 ai-zh、zh-CN
	LangDoc string // 显示名，如 中文（AI生成）
	URL     string // 字幕 JSON 或 SRT 的下载地址
}
