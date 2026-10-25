package bili

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"bili-subtitle/internal/model"
)

type viewPage struct {
	CID      int64  `json:"cid"`
	Page     int    `json:"page"`
	Part     string `json:"part"`
	Duration int    `json:"duration"`
}

type viewResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		BVID     string `json:"bvid"`
		AID      int64  `json:"aid"`
		Title    string `json:"title"`
		Duration int    `json:"duration"`
		CID      int64  `json:"cid"`
		Owner    struct {
			Name string `json:"name"`
		} `json:"owner"`
		Pages []viewPage `json:"pages"`
	} `json:"data"`
}

// VideoInfo 取视频元数据（含所选分 P 的 cid）；未登录也可用。
func (c *Client) VideoInfo(ctx context.Context, in *Input) (*model.Video, error) {
	if in == nil {
		return nil, errors.New("VideoInfo：输入为空")
	}
	page := in.Page
	if page < 1 {
		page = 1
	}
	query := url.Values{}
	switch {
	case in.BVid != "":
		query.Set("bvid", in.BVid)
	case in.AID != "":
		query.Set("aid", in.AID)
	default:
		return nil, errors.New("VideoInfo：输入里既没有 BV 号也没有 av 号")
	}

	var response viewResponse
	if err := c.apiGet(ctx, apiBase+"/x/web-interface/view?"+query.Encode(), "视频信息（view）", &response); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			if hint := viewErrorHint(apiErr.Code); hint != "" {
				return nil, fmt.Errorf("取视频信息失败：%s（code=%d）", hint, apiErr.Code)
			}
		}
		return nil, fmt.Errorf("取视频信息失败：%w", err)
	}

	cid, pickedPage, err := pickPageCID(response.Data.Pages, response.Data.CID, page)
	if err != nil {
		return nil, err
	}
	bvid := strings.TrimSpace(response.Data.BVID)
	if bvid == "" {
		bvid = in.BVid
	}
	aid := in.AID
	if response.Data.AID != 0 {
		aid = strconv.FormatInt(response.Data.AID, 10)
	}
	videoURL := "https://www.bilibili.com/video/"
	switch {
	case bvid != "":
		videoURL += bvid
	case aid != "":
		videoURL += "av" + aid
	}
	if page > 1 {
		videoURL += fmt.Sprintf("?p=%d", page)
	}

	// 多 P 视频顶层 duration 是所有分 P 之和，用当前 P 的时长更符合笔记和分段判断。
	duration := response.Data.Duration
	if pickedPage != nil && pickedPage.Duration > 0 {
		duration = pickedPage.Duration
	}

	return &model.Video{
		BVid:     bvid,
		AID:      aid,
		CID:      strconv.FormatInt(cid, 10),
		Title:    response.Data.Title,
		Owner:    response.Data.Owner.Name,
		Duration: duration,
		Page:     page,
		URL:      videoURL,
	}, nil
}

// pickPageCID 按分 P 序号从 pages 里找 cid，没有 pages 就用顶层 cid。
func pickPageCID(pages []viewPage, topCID int64, page int) (int64, *viewPage, error) {
	if len(pages) == 0 {
		if topCID == 0 {
			return 0, nil, fmt.Errorf("视频信息里没有 cid，取不了字幕")
		}
		return topCID, nil, nil
	}
	for i := range pages {
		if pages[i].Page == page {
			return pages[i].CID, &pages[i], nil
		}
	}
	// 极少数接口 page 字段从 0 开始或者没填，按下标兜底。
	if page >= 1 && page <= len(pages) {
		return pages[page-1].CID, &pages[page-1], nil
	}
	return 0, nil, fmt.Errorf("分 P %d 超出范围（这个视频共 %d P）", page, len(pages))
}

func viewErrorHint(code int) string {
	switch code {
	case -404:
		return "视频不存在或已被删除"
	case -400:
		return "请求参数有误"
	case -403:
		return "没有访问权限"
	case 62002:
		return "稿件不可见（可能被锁定或仅作者可见）"
	case 62004:
		return "稿件正在审核中"
	case 62012:
		return "仅 UP 主自己可见"
	}
	return ""
}
