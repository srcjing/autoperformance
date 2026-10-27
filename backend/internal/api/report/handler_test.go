package report

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	rptsvc "auto_performance/internal/service/report"
)

// stubService 报告服务桩：只实现被测方法，其余由嵌入接口兜底。
type stubService struct {
	Service

	page        *rptsvc.Page
	view        *rptsvc.View
	getErr      error
	artifact    *rptsvc.ArtifactFile
	artifactErr error
	importInput *rptsvc.ImportInput
	importErr   error
	regenerated int
}

func (s *stubService) List(_ context.Context, _ rptsvc.ListQuery) (*rptsvc.Page, error) {
	return s.page, nil
}

func (s *stubService) Get(_ context.Context, _ int64) (*rptsvc.View, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.view, nil
}

func (s *stubService) Regenerate(_ context.Context, _ int64) (*rptsvc.View, error) {
	s.regenerated++
	return s.view, nil
}

func (s *stubService) Artifact(_ context.Context, _ int64, _ string) (*rptsvc.ArtifactFile, error) {
	if s.artifactErr != nil {
		return nil, s.artifactErr
	}
	return s.artifact, nil
}

func (s *stubService) Import(_ context.Context, in rptsvc.ImportInput) (*rptsvc.View, error) {
	content, _ := io.ReadAll(in.Content)
	in.Content = bytes.NewReader(content)
	s.importInput = &in
	if s.importErr != nil {
		return nil, s.importErr
	}
	return s.view, nil
}

// newReportRouter 构造被测路由（无鉴权中间件，专注 handler 行为）。
func newReportRouter(svc Service, maxUpload int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := New(svc, maxUpload)
	r.GET("/api/v1/reports", h.ListReports)
	r.GET("/api/v1/reports/:id", h.GetReport)
	r.POST("/api/v1/reports/:id/generate", h.GenerateReport)
	r.POST("/api/v1/reports/import", h.Import)
	r.GET("/api/v1/reports/:id/export", h.ExportReport)
	r.GET("/api/v1/reports/:id/artifact", h.GetArtifact)
	return r
}

// sampleView 报告视图样例。
func sampleView() *rptsvc.View {
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	return &rptsvc.View{
		ID: 7, TaskID: 9, ProductID: 3,
		Source: domain.ReportSourceInternal, Type: domain.ReportFormatHTML,
		Title: "订单服务 性能测试报告", Status: domain.ReportStatusReady,
		Summary: &domain.ReportSummary{
			State: "fail", TotalInterfaces: 2, PassedInterfaces: 1, FailedInterfaces: 1,
			PassRate: 50, TotalThresholds: 2, PassedThresholds: 1, FailedThresholds: 1,
			Items: []domain.ReportSummaryItem{{InterfaceID: 11, InterfaceName: "查询订单", Metric: "p95", Operator: "≤", Threshold: 500, Actual: 210, HasActual: true, Pass: true}},
		},
		Artifacts: []domain.ReportArtifact{
			{ID: 1, ReportID: 7, Format: domain.ReportFormatHTML, Path: "/data/reports/7/report.html", Size: 1024},
			{ID: 2, ReportID: 7, Format: domain.ReportFormatWord, Path: "/data/reports/7/report.docx", Size: 2048},
		},
		CreatedAt: now, UpdatedAt: now,
	}
}

// TestReportDTOUsesStringIDsAndInlineFlags 校验 ID 字符串化 + 视图/下载语义。
func TestReportDTOUsesStringIDsAndInlineFlags(t *testing.T) {
	svc := &stubService{page: &rptsvc.Page{List: []rptsvc.View{*sampleView()}, Total: 1, Page: 1, PageSize: 20}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports?task_id=9", nil)
	newReportRouter(svc, 1024).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			Total int `json:"total"`
			List  []struct {
				ID      string `json:"id"`
				TaskID  string `json:"task_id"`
				Summary struct {
					Items []struct {
						InterfaceID string `json:"interface_id"`
					} `json:"items"`
				} `json:"summary"`
				Artifacts []struct {
					Format string `json:"format"`
					Inline bool   `json:"inline"`
				} `json:"artifacts"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if body.Data.Total != 1 || len(body.Data.List) != 1 {
		t.Fatalf("列表数据错误: %s", w.Body.String())
	}
	item := body.Data.List[0]
	if item.ID != "7" || item.TaskID != "9" {
		t.Fatalf("ID 应为字符串: id=%s task_id=%s", item.ID, item.TaskID)
	}
	if len(item.Summary.Items) != 1 || item.Summary.Items[0].InterfaceID != "11" {
		t.Fatalf("摘要接口 ID 应为字符串: %s", w.Body.String())
	}
	if len(item.Artifacts) != 2 || !item.Artifacts[0].Inline || item.Artifacts[1].Inline {
		t.Fatalf("html 应内联、word 应下载: %s", w.Body.String())
	}
}

// TestArtifactInlineAndExportAttachment 在线查看内联、导出强制下载（含安全响应头）。
func TestArtifactInlineAndExportAttachment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.html")
	if err := os.WriteFile(path, []byte("<html>report</html>"), 0o600); err != nil {
		t.Fatalf("准备产物失败: %v", err)
	}
	svc := &stubService{artifact: &rptsvc.ArtifactFile{
		Path: path, Format: domain.ReportFormatHTML, Filename: "report-7.html",
		ContentType: "text/html; charset=utf-8", Size: 20, Inline: true,
	}}
	router := newReportRouter(svc, 1024)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/reports/7/artifact?format=html", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("artifact status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Fatalf("在线查看应为 inline, got %q", got)
	}
	if got := w.Header().Get("Content-Security-Policy"); !strings.Contains(got, "sandbox") {
		t.Fatalf("HTML 产物应带沙箱 CSP, got %q", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("应禁止类型嗅探, got %q", got)
	}
	if !strings.Contains(w.Body.String(), "<html>report</html>") {
		t.Fatalf("未返回产物内容: %s", w.Body.String())
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/reports/7/export?format=html", nil))
	if got := w.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Fatalf("导出应为 attachment, got %q", got)
	}
}

// TestArtifactErrorMapsTo404 产物不存在/越界 → 24001（HTTP 404）。
func TestArtifactErrorMapsTo404(t *testing.T) {
	svc := &stubService{artifactErr: rptsvc.ErrReportFileNotFound}
	w := httptest.NewRecorder()
	newReportRouter(svc, 1024).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/reports/7/artifact?format=../../etc/passwd", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if code := bodyCode(t, w); code != 24001 {
		t.Fatalf("code = %d, want 24001", code)
	}
}

// TestReportNotFoundMapsTo10301 详情不存在 → 10301。
func TestReportNotFoundMapsTo10301(t *testing.T) {
	svc := &stubService{getErr: errcode.ErrNotFound}
	w := httptest.NewRecorder()
	newReportRouter(svc, 1024).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/reports/8", nil))
	if w.Code != http.StatusNotFound || bodyCode(t, w) != 10301 {
		t.Fatalf("status/code = %d/%d, want 404/10301", w.Code, bodyCode(t, w))
	}
}

// TestGenerateReportInvokesRegenerate 重新生成调用服务层。
func TestGenerateReportInvokesRegenerate(t *testing.T) {
	svc := &stubService{view: sampleView()}
	w := httptest.NewRecorder()
	newReportRouter(svc, 1024).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/reports/7/generate", nil))
	if w.Code != http.StatusOK || svc.regenerated != 1 {
		t.Fatalf("重新生成未生效: status=%d calls=%d", w.Code, svc.regenerated)
	}
}

// TestImportPassesFileAndOptionalIDs 导入透传文件与可选归属 ID。
func TestImportPassesFileAndOptionalIDs(t *testing.T) {
	svc := &stubService{view: sampleView()}
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, err := mw.CreateFormFile("file", "外部报告.html")
	if err != nil {
		t.Fatalf("构造 multipart 失败: %v", err)
	}
	if _, err := part.Write([]byte("<!DOCTYPE html><html><body>ok</body></html>")); err != nil {
		t.Fatalf("写入文件内容失败: %v", err)
	}
	_ = mw.WriteField("task_id", "9")
	_ = mw.WriteField("product_id", "3")
	if err := mw.Close(); err != nil {
		t.Fatalf("关闭 multipart 失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/import", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	newReportRouter(svc, 1<<20).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if svc.importInput == nil {
		t.Fatalf("服务层未收到导入请求")
	}
	if svc.importInput.Filename != "外部报告.html" || svc.importInput.TaskID != 9 || svc.importInput.ProductID != 3 {
		t.Fatalf("导入参数错误: %+v", *svc.importInput)
	}
	if svc.importInput.Size <= 0 {
		t.Fatalf("导入文件大小未透传: %+v", *svc.importInput)
	}
}

// TestImportTooLarge 文件超过上限 → 24003（handler 层提前拦截）。
func TestImportTooLarge(t *testing.T) {
	svc := &stubService{view: sampleView()}
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, _ := mw.CreateFormFile("file", "big.html")
	_, _ = part.Write(bytes.Repeat([]byte("a"), 512))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/import", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	newReportRouter(svc, 64).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || bodyCode(t, w) != 24003 {
		t.Fatalf("status/code = %d/%d, want 400/24003", w.Code, bodyCode(t, w))
	}
	if svc.importInput != nil {
		t.Fatalf("超限文件不应进入服务层")
	}
}

// TestImportTypeRejected 服务层类型校验失败透传 24002。
func TestImportTypeRejected(t *testing.T) {
	svc := &stubService{importErr: rptsvc.ErrReportTypeNotAllowed}
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, _ := mw.CreateFormFile("file", "shell.php")
	_, _ = part.Write([]byte("<?php ?>"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports/import", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	newReportRouter(svc, 1<<20).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || bodyCode(t, w) != 24002 {
		t.Fatalf("status/code = %d/%d, want 400/24002", w.Code, bodyCode(t, w))
	}
}

// bodyCode 解析统一响应中的业务码。
func bodyCode(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	var body struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v body=%s", err, w.Body.String())
	}
	return body.Code
}
