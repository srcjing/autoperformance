package api

// 前后端接口契约一致性测试（Phase 9 任务 9.1）。
//
// 前端 `src/api/*.ts` 中的请求路径必须全部能在后端路由表（78 个 REST 接口）中命中，
// 防止「前端调用了不存在的接口 / 后端改了路径而前端未同步」这类联调缺口。
// 路径模板统一归一化：`/environments/${id}?x=1` → `/api/v1/environments/:id`。

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// frontendRequestRE 匹配前端 `request<...>('path' | `path` | "path")` 的首个参数。
var frontendRequestRE = regexp.MustCompile("request(?:<[^>]*>)?\\(\\s*(`[^`]*`|'[^']*'|\"[^\"]*\")")

// normalizeFrontendPath 把前端请求路径归一化为后端路由模板。
//
// 规则：
//  1. 去掉引号与查询串（`?` 之后）；
//  2. 路径段内容恰为 `${...}` → `:id`（如 `/environments/${id}`）；
//  3. 变量插值作为路径段后缀（如 `/tags${buildQuery({...})}`）→ 视为查询构建，从此截断。
func normalizeFrontendPath(raw string) string {
	p := strings.Trim(raw, "`'\"")
	if idx := strings.IndexByte(p, '?'); idx >= 0 {
		p = p[:idx]
	}

	var b strings.Builder
	for i := 0; i < len(p); {
		if !strings.HasPrefix(p[i:], "${") {
			b.WriteByte(p[i])
			i++
			continue
		}
		end := matchClosingBrace(p, i+1)
		if end < 0 {
			break // 未闭合模板：保守截断
		}
		current := b.String()
		prefix := current[strings.LastIndexByte(current, '/')+1:]
		if strings.TrimSpace(prefix) == "" {
			b.WriteString(":id")
		}
		// 变量作为段后缀时（`tags${...}`）表示查询串拼接：丢弃剩余部分。
		if strings.TrimSpace(prefix) != "" {
			break
		}
		i = end
	}

	p = strings.TrimSpace(b.String())
	if p == "" || !strings.HasPrefix(p, "/") {
		return ""
	}
	return "/api/v1" + p
}

// matchClosingBrace 返回从 open（指向 `{`）开始匹配的右括号之后的位置；未闭合返回 -1。
//
// 支持嵌套花括号（如 `${buildQuery({ keyword })}`）。
func matchClosingBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// backendRouteTemplates 返回全部后端路由模板（含认证接口与 WS），用于契约比对。
func backendRouteTemplates() map[string]bool {
	routes := map[string]bool{}
	add := func(method, path string) { routes[method+" "+"/api/v1"+path] = true }

	for _, rt := range securedRoutesForTest() {
		add(rt.Method, rt.Path)
	}
	// 认证模块（4）+ 实时通道（1）
	for _, rt := range []struct{ method, path string }{
		{"POST", "/auth/login"},
		{"POST", "/auth/logout"},
		{"POST", "/auth/change-password"},
		{"GET", "/auth/me"},
		{"GET", "/ws"},
	} {
		add(rt.method, rt.path)
	}
	return routes
}

// TestFrontendAPIPathsMatchBackendRoutes 前端所有 API 调用路径在后端均有对应路由（9.1 联调基线）。
func TestFrontendAPIPathsMatchBackendRoutes(t *testing.T) {
	apiDir := filepath.Join("..", "..", "..", "frontend", "src", "api")
	entries, err := os.ReadDir(apiDir)
	if err != nil {
		t.Skipf("前端源码不可用（跳过契约检查）: %v", err)
	}

	// 后端路径（忽略方法）集合：前端同一路径可能对应不同方法，方法级校验见路由测试。
	backendPaths := map[string]bool{}
	for full := range backendRouteTemplates() {
		parts := strings.SplitN(full, " ", 2)
		backendPaths[parts[1]] = true
	}

	seen := map[string][]string{} // path → 出现位置（文件:行）
	var calls int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ts") || e.Name() == "http.ts" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(apiDir, e.Name()))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", e.Name(), err)
		}
		src := string(raw)
		for _, m := range frontendRequestRE.FindAllStringSubmatchIndex(src, -1) {
			lit := src[m[2]:m[3]]
			path := normalizeFrontendPath(lit)
			if path == "" {
				continue
			}
			calls++
			line := 1 + strings.Count(src[:m[0]], "\n")
			seen[path] = append(seen[path], e.Name()+":"+strconv.Itoa(line))
		}
	}

	if calls < 40 {
		t.Fatalf("解析到的前端请求数 = %d，疑似解析规则失效（期望 ≥40）", calls)
	}

	var unknown []string
	for path, where := range seen {
		if !backendPaths[path] {
			unknown = append(unknown, path+" （"+strings.Join(where, ", ")+"）")
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		t.Fatalf("前端调用了后端未注册的接口（%d 处）:\n%s", len(unknown), strings.Join(unknown, "\n"))
	}

	// 覆盖率仅作观测（部分接口由页面直接拼 URL 或暂未使用，不做硬断言）。
	covered := 0
	for p := range backendPaths {
		if len(seen[p]) > 0 {
			covered++
		}
	}
	t.Logf("前端 API 调用 %d 处，覆盖后端路由模板 %d/%d", calls, covered, len(backendPaths))
}
