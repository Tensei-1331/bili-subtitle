package bili

import (
	"strings"
	"testing"
	"time"
)

func TestMixinKey(t *testing.T) {
	// 这组 img_key/sub_key 是 2026-10-26 真实 nav 接口返回的，mixin_key 跟文档样例一致。
	imgKey := "7cd084941338484aae1ad9425b84077c"
	subKey := "4932caff0ff746eab6f01bf08b70ac45"
	want := "ea1db124af3c7062474693fa704f4ff8"
	if got := mixinKey(imgKey, subKey); got != want {
		t.Fatalf("mixinKey 不对：got=%q want=%q", got, want)
	}
}

func TestSignQueryDocumentedSample(t *testing.T) {
	keys := wbiKeys{
		imgKey:   "7cd084941338484aae1ad9425b84077c",
		subKey:   "4932caff0ff746eab6f01bf08b70ac45",
		mixinKey: "ea1db124af3c7062474693fa704f4ff8",
	}
	params := map[string]string{"foo": "114", "bar": "514", "zab": "1919810"}
	want := "bar=514&foo=114&wts=1702204169&zab=1919810&w_rid=8f6f2b5b3d485fe1886cec6a0be8c5d4"
	if got := signQuery(keys, params, time.Unix(1702204169, 0)); got != want {
		t.Fatalf("签名结果不对\n got=%s\nwant=%s", got, want)
	}
}

func TestSignQueryFilterAndEscape(t *testing.T) {
	keys := wbiKeys{mixinKey: "0123456789abcdef0123456789abcdef"}
	got := signQuery(keys, map[string]string{"k": "a b!'()*中"}, time.Unix(0, 0))
	// 先过滤 !'()*，再按 encodeURIComponent 编码：空格是 %20 不是 +。
	wantPrefix := "k=a%20b%E4%B8%AD&wts=0&w_rid="
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("参数编码不对：got=%s", got)
	}
	if rid := strings.TrimPrefix(got, wantPrefix); len(rid) != 32 {
		t.Fatalf("w_rid 应该是 32 位 md5，got=%q", rid)
	}
}

func TestExtractWBIKey(t *testing.T) {
	cases := map[string]string{
		"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png": "7cd084941338484aae1ad9425b84077c",
		"//i0.hdslb.com/bfs/wbi/abcdef.png":                                 "abcdef",
		"":                                                                  "",
	}
	for raw, want := range cases {
		if got := extractWBIKey(raw); got != want {
			t.Errorf("extractWBIKey(%q)=%q，期望 %q", raw, got, want)
		}
	}
}
