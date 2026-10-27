package api

import (
	"bytes"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/errcode"
)

// staticContentTypes 前端产物 MIME 映射。
//
// 显式声明而不依赖宿主机 mime 注册表（Windows 注册表可能把 .js 识别成 text/plain，
// 导致浏览器拒绝执行 ES Module）。
var staticContentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".map":   "application/json; charset=utf-8",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".txt":   "text/plain; charset=utf-8",
}

// staticCacheControl 静态资源缓存策略。
//
// Vite 产物文件名带内容哈希，可长缓存；入口 HTML 必须每次回源（否则前端发版不生效）。
func staticCacheControl(name string) string {
	if name == "index.html" {
		return "no-cache"
	}
	if strings.HasPrefix(name, "assets/") {
		return "public, max-age=31536000, immutable"
	}
	return "public, max-age=3600"
}

// newSPAHandler 构造前端静态资源处理器（go:embed 内嵌 dist）。
//
// 行为：
//  1. /api 前缀不在此处理（保持统一 JSON 404，避免把 API 拼写错误当成前端路由）；
//  2. 命中内嵌文件（含目录 → index.html）即返回，按扩展名设置 MIME 与缓存策略；
//  3. 未命中且路径形如文件（最后一段含扩展名，如缺失的 /assets/x.js）→ 404；
//  4. 其余路径回退 index.html（Vue Router history 模式的 SPA 深链刷新）。
func newSPAHandler(dist fs.FS) gin.HandlerFunc {
	if dist == nil {
		// 前端产物缺失：保持原统一 JSON 404，不影响 API 行为。
		return func(c *gin.Context) { response.Error(c, errcode.ErrNotFound) }
	}

	return func(c *gin.Context) {
		reqPath := c.Request.URL.Path
		if isAPIPath(reqPath) || (c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) {
			response.Error(c, errcode.ErrNotFound)
			return
		}

		if name, ok := resolveStaticFile(dist, reqPath); ok {
			serveStaticFile(c, dist, name)
			return
		}
		if looksLikeFileRequest(reqPath) {
			response.Error(c, errcode.ErrNotFound)
			return
		}
		serveStaticFile(c, dist, "index.html")
	}
}

// isAPIPath 判断是否为 API 路径（/api 与 /api/...）。
func isAPIPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/")
}

// resolveStaticFile 把请求路径解析为内嵌文件系统中的文件（含 SPA 目录入口）。
//
// 只接受合法的相对路径（禁止 ".."、绝对路径、NUL），杜绝路径穿越读取内嵌产物之外的内容。
func resolveStaticFile(dist fs.FS, reqPath string) (string, bool) {
	name := strings.TrimPrefix(path.Clean("/"+reqPath), "/")
	if name == "" {
		name = "index.html"
	}
	if !fs.ValidPath(name) || strings.ContainsRune(name, 0) {
		return "", false
	}
	info, err := fs.Stat(dist, name)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		if idx := path.Join(name, "index.html"); idx != "index.html" {
			if sub, err := fs.Stat(dist, idx); err == nil && sub.Mode().IsRegular() {
				return idx, true
			}
		}
		return "", false
	}
	if !info.Mode().IsRegular() {
		return "", false
	}
	return name, true
}

// serveStaticFile 输出内嵌文件（支持 HEAD/Range/条件请求）。
func serveStaticFile(c *gin.Context, dist fs.FS, name string) {
	data, err := fs.ReadFile(dist, name)
	if err != nil {
		log.Printf("[static] 读取内嵌资源失败 name=%s: %v", name, err)
		response.Error(c, errcode.ErrNotFound)
		return
	}

	if ct, ok := staticContentTypes[strings.ToLower(path.Ext(name))]; ok {
		c.Header("Content-Type", ct)
	}
	c.Header("Cache-Control", staticCacheControl(name))
	c.Header("X-Content-Type-Options", "nosniff")

	// ServeContent 处理 Content-Length/Range/HEAD 与 304 语义；时间置零避免误判未修改。
	http.ServeContent(c.Writer, c.Request, path.Base(name), time.Time{}, bytes.NewReader(data))
}

// looksLikeFileRequest 判断请求是否形如静态文件（末段含扩展名）。
//
// 目的：缺失的 /assets/xxx.js 应返回 404，而不是回退 index.html（否则浏览器把 HTML
// 当 JS 解析，报 MIME 错误并污染缓存）。
func looksLikeFileRequest(reqPath string) bool {
	base := path.Base(path.Clean("/" + reqPath))
	if base == "/" || base == "." {
		return false
	}
	ext := path.Ext(base)
	return ext != "" && ext != "."
}
