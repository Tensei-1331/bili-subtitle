package bili

import "testing"

func TestPickPageCID(t *testing.T) {
	pages := []viewPage{
		{CID: 101, Page: 1, Duration: 30},
		{CID: 102, Page: 2, Duration: 40},
		{CID: 103, Page: 3, Duration: 50},
	}
	cid, picked, err := pickPageCID(pages, 999, 2)
	if err != nil {
		t.Fatalf("取第 2 P 失败：%v", err)
	}
	if cid != 102 || picked == nil || picked.Duration != 40 {
		t.Fatalf("第 2 P 取错了：cid=%d picked=%+v", cid, picked)
	}

	// 没有 pages 时退回顶层 cid
	cid, picked, err = pickPageCID(nil, 999, 1)
	if err != nil || cid != 999 || picked != nil {
		t.Fatalf("没有 pages 时应退回顶层 cid：cid=%d picked=%+v err=%v", cid, picked, err)
	}
	// 没有 pages 也没有顶层 cid 要报错
	if _, _, err := pickPageCID(nil, 0, 1); err == nil {
		t.Fatal("没有 cid 应该报错")
	}
	// 序号超出范围要报错
	if _, _, err := pickPageCID(pages, 999, 9); err == nil {
		t.Fatal("分 P 超出范围应该报错")
	}
	// page 字段没填时按下标兜底
	weird := []viewPage{{CID: 201}, {CID: 202}}
	cid, _, err = pickPageCID(weird, 0, 2)
	if err != nil || cid != 202 {
		t.Fatalf("page 字段缺失时按下标兜底失败：cid=%d err=%v", cid, err)
	}
}
