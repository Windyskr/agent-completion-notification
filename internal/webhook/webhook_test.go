package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseEndpoint(t *testing.T) {
	cases := []struct {
		raw     string
		wantErr string
	}{
		{"https://open.feishu.cn/hook/key", ""},
		{"  http://127.0.0.1:8080/hook  ", ""},
		{"", "飞书 webhook 地址未配置"},
		{"   ", "飞书 webhook 地址未配置"},
		{"ftp://example.com/hook", "飞书 webhook 地址格式无效"},
		{"open.feishu.cn/hook/key", "飞书 webhook 地址格式无效"},
		{"http://[::1/hook/SECRET", "飞书 webhook 地址格式无效"},
	}
	for _, c := range cases {
		_, err := ParseEndpoint(c.raw, "飞书 webhook 地址")
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%q: 意外错误 %v", c.raw, err)
		case c.wantErr != "" && (err == nil || err.Error() != c.wantErr):
			t.Errorf("%q: 错误 = %v, 期望 %q", c.raw, err, c.wantErr)
		}
		if err != nil && strings.Contains(err.Error(), "SECRET") {
			t.Errorf("%q: 错误泄露了地址: %v", c.raw, err)
		}
	}
}

func TestPostJSONSendsPayload(t *testing.T) {
	var method, contentType string
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, contentType = r.Method, r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
		io.WriteString(w, `{"code":0}`)
	}))
	t.Cleanup(srv.Close)

	endpoint, err := ParseEndpoint(srv.URL, "测试地址")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := PostJSON(context.Background(), NewClient(), endpoint, map[string]string{"text": "a < b && c"})
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || contentType != "application/json; charset=utf-8" {
		t.Errorf("请求 = %s %s", method, contentType)
	}
	if got["text"] != "a < b && c" {
		t.Errorf("请求体 = %v", got)
	}
	if resp.StatusCode != http.StatusAccepted || string(resp.Body) != `{"code":0}` || resp.CheckStatus() != nil {
		t.Errorf("应答 = %+v", resp)
	}
}

// 网络错误默认带完整请求地址（含 token），必须剥掉。
func TestPostJSONErrorHidesEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint, err := ParseEndpoint(srv.URL+"/hook/SECRET-TOKEN", "测试地址")
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()

	_, err = PostJSON(context.Background(), NewClient(), endpoint, map[string]string{})
	if err == nil {
		t.Fatal("连接失败应当报错")
	}
	if strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Errorf("错误泄露了 webhook token: %v", err)
	}
}

// 应答只用于判定成败，超长应答截断读取，不能把整个响应读进内存。
func TestPostJSONLimitsResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Repeat("x", 1<<20))
	}))
	t.Cleanup(srv.Close)

	endpoint, _ := ParseEndpoint(srv.URL, "测试地址")
	resp, err := PostJSON(context.Background(), NewClient(), endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Body) != maxResponseBytes {
		t.Errorf("应答读取 %d 字节, 期望截断为 %d", len(resp.Body), maxResponseBytes)
	}
}

func TestCheckStatus(t *testing.T) {
	if err := (Response{StatusCode: 204}).CheckStatus(); err != nil {
		t.Errorf("2xx 应视为成功: %v", err)
	}
	err := (Response{StatusCode: 503, Body: []byte(" unavailable \n")}).CheckStatus()
	if err == nil || err.Error() != "HTTP 503: unavailable" {
		t.Errorf("错误 = %v, 期望 HTTP 503: unavailable", err)
	}
}
