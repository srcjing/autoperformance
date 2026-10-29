package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/errcode"
	"auto_performance/web"
)

// newStaticOnlyRouter 构造仅含静态资源兜底的路由（不挂业务路由，避免干扰）。
func newStaticOnlyRouter(dist fs.FS) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.NoRoute(newSPAHandler(dist))
	return r
}

// TestStaticServesEmbeddedIndex 内嵌产物通过根路径可访问（修复「单二进制不提供前端页面」）。
func TestStaticServesEmbeddedIndex(t *testing.T) {
	r, _ := newTestRouter(t, map[string]bool{})

	w := call(r, http.MethodGet, "/", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("GET / Content-Type = %q, want text/html", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("入口 HTML Cache-Control = %q, want no-cache", cc)
	}
	if body := w.Body.String(); !strings.Contains(body, `<div id="app">`) {
		t.Fatalf("GET / 未返回前端入口 HTML: %s", body)
	}
}

// TestStaticServesEmbeddedAssetsWithMIME 内嵌 JS/CSS 产物按正确 MIME + 长缓存返回。
func TestStaticServesEmbeddedAssetsWithMIME(t *testing.T) {
	dist, err := web.Dist()
	if err != nil {
		t.Fatalf("内嵌文件系统不可用: %v", err)
	}
	entries, err := fs.ReadDir(dist, "assets")
	if err != nil {
		t.Fatalf("内嵌 assets 目录不可用: %v", err)
	}

	var jsName, cssName string
	for _, e := range entries {
		switch {
		case jsName == "" && strings.HasSuffix(e.Name(), ".js"):
			jsName = e.Name()
		case cssName == "" && strings.HasSuffix(e.Name(), ".css"):
			cssName = e.Name()
		}
	}
	if jsName == "" || cssName == "" {
		t.Fatalf("内嵌产物缺少 JS/CSS 资源: %v", entries)
	}

	r, _ := newTestRouter(t, map[string]bool{})
	for path, wantType := range map[string]string{
		"/assets/" + jsName:  "text/javascript",
		"/assets/" + cssName: "text/css",
	} {
		w := call(r, http.MethodGet, path, "")
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, wantType) {
			t.Fatalf("GET %s Content-Type = %q, want %s", path, ct, wantType)
		}
		if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
			t.Fatalf("GET %s Cache-Control = %q, want immutable", path, cc)
		}
		if w.Body.Len() == 0 {
			t.Fatalf("GET %s 响应体为空", path)
		}
	}
}

// TestStaticSPAFallbackForClientRoute 前端 history 深链刷新回退 index.html。
func TestStaticSPAFallbackForClientRoute(t *testing.T) {
	r, _ := newTestRouter(t, map[string]bool{})

	for _, p := range []string{"/environments", "/tasks/42", "/reports/detail"} {
		w := call(r, http.MethodGet, p, "")
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200（SPA 回退）", p, w.Code)
		}
		if !strings.Contains(w.Body.String(), `<div id="app">`) {
			t.Fatalf("GET %s 未回退到 index.html", p)
		}
	}
}

// TestStaticKeepsAPI404JSON API 路径未命中仍返回统一 JSON（不被 SPA 回退吞掉）。
func TestStaticKeepsAPI404JSON(t *testing.T) {
	r, _ := newTestRouter(t, map[string]bool{})

	for _, p := range []string{"/api/v1/unknown", "/api", "/api/v1/environments/1/unknown"} {
		w := call(r, http.MethodGet, p, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", p, w.Code)
		}
		var body struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("GET %s 响应不是统一 JSON 结构: %s", p, w.Body.String())
		}
		if body.Code != errcode.ErrNotFound.Code {
			t.Fatalf("GET %s code = %d, want %d", p, body.Code, errcode.ErrNotFound.Code)
		}
	}
}

// TestStaticMissingAsset404 缺失的静态文件返回 404，不回退 HTML（避免 MIME 误判与缓存污染）。
func TestStaticMissingAsset404(t *testing.T) {
	r, _ := newTestRouter(t, map[string]bool{})

	for _, p := range []string{"/assets/not-exist.js", "/favicon.ico", "/robots.txt"} {
		w := call(r, http.MethodGet, p, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", p, w.Code)
		}
	}
}

// TestStaticOnlyServesGetAndHead 非 GET/HEAD 未命中路由一律 404（不返回页面内容）。
func TestStaticOnlyServesGetAndHead(t *testing.T) {
	r, _ := newTestRouter(t, map[string]bool{})

	w := call(r, http.MethodPost, "/environments", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("POST /environments status = %d, want 404", w.Code)
	}

	w = call(r, http.MethodHead, "/", "")
	if w.Code != http.StatusOK {
		t.Fatalf("HEAD / status = %d, want 200", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD / 不应返回响应体, got %d 字节", w.Body.Len())
	}
}

// TestStaticRejectsTraversal 路径穿越（../、绝对路径、点段）不得逃出 dist 根。
func TestStaticRejectsTraversal(t *testing.T) {
	var gotPaths []string
	r := newStaticOnlyRouter(fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html><div id=\"app\"></div></html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	})

	cases := []string{
		"/../../secret.txt",
		"/assets/../../../secret.txt",
		"/..%2fsecret.txt",
		"/%2e%2e/%2e%2e/secret.txt",
	}
	for _, p := range cases {
		req := httptest.NewRequest(http.MethodGet, "http://example.test"+p, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		gotPaths = append(gotPaths, p)
		if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "ROOT_SECRET") {
			t.Fatalf("路径穿越未被拦截: %s", p)
		}
	}
	if len(gotPaths) != len(cases) {
		t.Fatalf("用例未全部执行: %v", gotPaths)
	}

	// 直接校验解析函数：点段被归一化后只能落在 dist 根内，根外文件一律不可达。
	dist := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}}
	if name, ok := resolveStaticFile(dist, "/assets/../index.html"); !ok || name != "index.html" {
		t.Fatalf("点段路径应归一化到 index.html, got %q ok=%v", name, ok)
	}
	if _, ok := resolveStaticFile(dist, "/../secret.txt"); ok {
		t.Fatal("根外文件不应被解析")
	}
}

// TestStaticNilFSFallsBackToNotFound 无内嵌产物时保持统一 JSON 404（不影响 API）。
func TestStaticNilFSFallsBackToNotFound(t *testing.T) {
	r := newStaticOnlyRouter(nil)
	w := call(r, http.MethodGet, "/", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET / status = %d, want 404", w.Code)
	}
	var body struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是统一 JSON: %s", w.Body.String())
	}
	if body.Code != errcode.ErrNotFound.Code {
		t.Fatalf("code = %d, want %d", body.Code, errcode.ErrNotFound.Code)
	}
}
