package bili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"bili-subtitle/internal/model"
)

type playerResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		NeedLoginSubtitle bool `json:"need_login_subtitle"`
		Subtitle          struct {
			Subtitles []struct {
				ID            int64  `json:"id"`
				Lan           string `json:"lan"`
				LanDoc        string `json:"lan_doc"`
				SubtitleURL   string `json:"subtitle_url"`
				SubtitleURLV2 string `json:"subtitle_url_v2"`
			} `json:"subtitles"`
		} `json:"subtitle"`
	} `json:"data"`
}

// SubtitleTracks 取字幕轨道；未登录时 B 站返回空列表，这里也返回空切片加 nil 错误。
func (c *Client) SubtitleTracks(ctx context.Context, video *model.Video) ([]model.SubtitleTrack, error) {
	if video == nil {
		return nil, errors.New("SubtitleTracks：视频为空")
	}
	if strings.TrimSpace(video.CID) == "" {
		return nil, fmt.Errorf("视频「%s」没有 cid，查不了字幕", video.Title)
	}
	params := map[string]string{"cid": video.CID}
	switch {
	case video.BVid != "":
		params["bvid"] = video.BVid
	case video.AID != "":
		params["aid"] = video.AID
	default:
		return nil, fmt.Errorf("视频「%s」既没有 BV 号也没有 av 号，查不了字幕", video.Title)
	}

	response, err := c.playerResponse(ctx, params)
	if err != nil {
		// WBI key 会定期轮换，签名被风控拒掉时换一份新 key 重试一次。
		if isWBIRejected(err) {
			if _, refreshErr := c.refreshWBIKeys(ctx); refreshErr == nil {
				response, err = c.playerResponse(ctx, params)
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("查询字幕轨道失败：%w", err)
	}
	return subtitleTracks(response), nil
}

func (c *Client) playerResponse(ctx context.Context, params map[string]string) (*playerResponse, error) {
	keys, err := c.wbiKeys(ctx)
	if err != nil {
		return nil, err
	}
	query := signQuery(keys, params, time.Now())
	var response playerResponse
	if err := c.apiGet(ctx, apiBase+"/x/player/wbi/v2?"+query, "字幕轨道（player/wbi/v2）", &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func isWBIRejected(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Code == -403 || apiErr.Code == -352
}

func subtitleTracks(response *playerResponse) []model.SubtitleTrack {
	tracks := make([]model.SubtitleTrack, 0, len(response.Data.Subtitle.Subtitles))
	for _, item := range response.Data.Subtitle.Subtitles {
		subtitleURL := normalizeSubtitleURL(item.SubtitleURL)
		if subtitleURL == "" {
			subtitleURL = normalizeSubtitleURL(item.SubtitleURLV2)
		}
		if subtitleURL == "" {
			continue
		}
		lang := strings.TrimSpace(item.Lan)
		if lang == "" {
			lang = fmt.Sprintf("track-%d", item.ID)
		}
		tracks = append(tracks, model.SubtitleTrack{Lang: lang, LangDoc: item.LanDoc, URL: subtitleURL})
	}
	return tracks
}

// normalizeSubtitleURL 补全字幕地址：// 开头补 https:；http:// 也升成 https（aisubtitle 支持 https）。
func normalizeSubtitleURL(raw string) string {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "":
		return ""
	case strings.HasPrefix(raw, "//"):
		return "https:" + raw
	case strings.HasPrefix(raw, "http://"):
		return "https://" + strings.TrimPrefix(raw, "http://")
	default:
		return raw
	}
}

// FetchCues 下载并解析字幕 JSON（aisubtitle.hdslb.com 的 body 数组）。
func FetchCues(ctx context.Context, subtitleURL string) ([]model.Cue, error) {
	rawURL := strings.TrimSpace(subtitleURL)
	if rawURL == "" {
		return nil, errors.New("FetchCues：字幕地址为空")
	}
	body, _, err := defaultClient.get(ctx, rawURL, "字幕下载")
	if err != nil {
		return nil, err
	}
	return parseSubtitleJSON(body)
}

func parseSubtitleJSON(data []byte) ([]model.Cue, error) {
	var payload struct {
		Body []struct {
			From    float64 `json:"from"`
			To      float64 `json:"to"`
			Content string  `json:"content"`
		} `json:"body"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("解析字幕 JSON 失败：%w", err)
	}
	cues := make([]model.Cue, 0, len(payload.Body))
	for _, item := range payload.Body {
		text := strings.TrimSpace(strings.ReplaceAll(item.Content, "\u00a0", " "))
		if text == "" || item.To <= item.From {
			continue
		}
		cues = append(cues, model.Cue{From: item.From, To: item.To, Text: text})
	}
	return cues, nil
}
