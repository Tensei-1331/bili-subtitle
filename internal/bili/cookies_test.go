package bili

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadCookiesFileNetscape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.txt")
	content := "# Netscape HTTP Cookie File\n" +
		"# https://curl.se/docs/http-cookies.html\n" +
		"\n" +
		".bilibili.com\tTRUE\t/\tFALSE\t1893456000\tSESSDATA\tabc%2C123%3D\n" +
		"#HttpOnly_.bilibili.com\tTRUE\t/\tTRUE\t0\tbili_jct\tdeadbeef\n" +
		"# 这一行是注释，要跳过\n" +
		"坏行没有制表符\n" +
		".bilibili.com\tTRUE\t/\tFALSE\t0\tDedeUserID\t12345\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写测试文件失败：%v", err)
	}
	got, err := LoadCookiesFile(path)
	if err != nil {
		t.Fatalf("LoadCookiesFile 返回错误：%v", err)
	}
	want := map[string]string{
		"SESSDATA":   "abc%2C123%3D",
		"bili_jct":   "deadbeef",
		"DedeUserID": "12345",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("解析结果不对\n got=%v\nwant=%v", got, want)
	}
}

func TestLoadCookiesFileMissing(t *testing.T) {
	got, err := LoadCookiesFile(filepath.Join(t.TempDir(), "not-exist.txt"))
	if err != nil {
		t.Fatalf("文件不存在不该报错：%v", err)
	}
	if len(got) != 0 {
		t.Fatalf("文件不存在应返回空 map，got=%v", got)
	}
}

func TestSaveCookiesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "cookies.txt")
	cookies := []*http.Cookie{
		{Name: "SESSDATA", Value: "abc%2C123%3D", Domain: ".bilibili.com", Path: "/", Expires: time.Unix(1893456000, 0)},
		{Name: "bili_jct", Value: "deadbeef", Path: "/"},
		{Name: "", Value: "空名字要跳过"},
	}
	saved, err := SaveCookiesFile(path, cookies)
	if err != nil {
		t.Fatalf("SaveCookiesFile 返回错误：%v", err)
	}
	want := map[string]string{"SESSDATA": "abc%2C123%3D", "bili_jct": "deadbeef"}
	if !reflect.DeepEqual(saved, want) {
		t.Fatalf("返回的 map 不对\n got=%v\nwant=%v", saved, want)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回保存的文件失败：%v", err)
	}
	fileText := string(raw)
	if !strings.HasPrefix(fileText, "# Netscape HTTP Cookie File") {
		t.Fatalf("文件头不对：\n%s", fileText)
	}
	// 没带 Domain 的 cookie 补 .bilibili.com，过期时间写成 Unix 秒。
	if !strings.Contains(fileText, ".bilibili.com\tTRUE\t/\tFALSE\t1893456000\tSESSDATA\tabc%2C123%3D") {
		t.Fatalf("SESSDATA 这一行不对：\n%s", fileText)
	}

	loaded, err := LoadCookiesFile(path)
	if err != nil {
		t.Fatalf("LoadCookiesFile 读回失败：%v", err)
	}
	if !reflect.DeepEqual(loaded, want) {
		t.Fatalf("写出去再读回来不一致\n got=%v\nwant=%v", loaded, want)
	}
}
