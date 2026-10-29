package setting

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	settingsvc "auto_performance/internal/service/setting"
)

// secretStubService 系统设置 handler 的证书/SSH 桩（其余方法由嵌入接口提供）。
type secretStubService struct {
	Service

	certInput      settingsvc.CertificateInput
	certCreateErr  error
	certDeleteErr  error
	certReinitErr  error
	reinitCalled   int64
	deleteCalled   int64
	sshInput       settingsvc.SshHostInput
	sshUpdateID    int64
	importFilename string
	importSize     int64
	importContent  string
	importResult   *settingsvc.SshHostImportResult
	certListPage   int
	certListSize   int
}

func (s *secretStubService) ListCertificates(_ context.Context, page, pageSize int) (*settingsvc.CertificatePage, error) {
	s.certListPage, s.certListSize = page, pageSize
	view := &settingsvc.CertificateView{
		ID: 11, Name: "内置自签证书", IsBuiltin: true, Status: domain.CertStatusValid, HasKey: true, Version: 1,
	}
	return &settingsvc.CertificatePage{List: []settingsvc.CertificateView{*view}, Total: 1, Page: page, PageSize: pageSize}, nil
}

func (s *secretStubService) CreateCertificate(_ context.Context, in settingsvc.CertificateInput) (*settingsvc.CertificateView, error) {
	s.certInput = in
	if s.certCreateErr != nil {
		return nil, s.certCreateErr
	}
	return &settingsvc.CertificateView{
		ID: 12, Name: in.Name, Issuer: in.Issuer, IsBuiltin: false,
		Status: domain.CertStatusValid, HasKey: len(in.KeyPEM) > 0, Version: 1,
	}, nil
}

func (s *secretStubService) DeleteCertificate(_ context.Context, certID int64) error {
	s.deleteCalled = certID
	return s.certDeleteErr
}

func (s *secretStubService) ReinitCertificate(_ context.Context, certID int64) (*settingsvc.CertificateView, error) {
	s.reinitCalled = certID
	if s.certReinitErr != nil {
		return nil, s.certReinitErr
	}
	return &settingsvc.CertificateView{ID: certID, Name: "内置自签证书", IsBuiltin: true, Status: domain.CertStatusValid, Version: 2}, nil
}

func (s *secretStubService) ListSshHosts(_ context.Context, q settingsvc.ListSshHostsQuery) (*settingsvc.SshHostPage, error) {
	return &settingsvc.SshHostPage{
		List: []settingsvc.SshHostView{{
			ID: 21, Type: domain.SshHostTypeSSH, Host: "10.0.0.8", Port: 22,
			User: "autoperf", AuthType: domain.SshAuthTypeKey, HasKey: true, Version: 1,
			HostEntries: []string{},
		}},
		Total: 1, Page: q.Page, PageSize: q.PageSize,
	}, nil
}

func (s *secretStubService) CreateSshHost(_ context.Context, in settingsvc.SshHostInput) (*settingsvc.SshHostView, error) {
	s.sshInput = in
	return &settingsvc.SshHostView{
		ID: 22, Type: in.Type, Host: in.Host, Port: in.Port, User: in.User,
		AuthType: in.AuthType, HasKey: in.Key != "", HostEntries: in.HostEntries, Version: 1,
	}, nil
}

func (s *secretStubService) UpdateSshHost(_ context.Context, sshHostID int64, in settingsvc.SshHostInput) (*settingsvc.SshHostView, error) {
	s.sshUpdateID = sshHostID
	s.sshInput = in
	return &settingsvc.SshHostView{
		ID: sshHostID, Type: in.Type, Host: in.Host, Port: in.Port, User: in.User,
		AuthType: in.AuthType, HasKey: true, Version: 2,
	}, nil
}

func (s *secretStubService) DeleteSshHost(_ context.Context, sshHostID int64) error {
	s.deleteCalled = sshHostID
	return nil
}

func (s *secretStubService) ImportSshHosts(_ context.Context, filename string, size int64, content io.Reader) (*settingsvc.SshHostImportResult, error) {
	s.importFilename, s.importSize = filename, size
	raw, err := io.ReadAll(content)
	if err != nil {
		return nil, err
	}
	s.importContent = string(raw)
	if s.importResult != nil {
		return s.importResult, nil
	}
	return &settingsvc.SshHostImportResult{Total: 2, Created: 1, Skipped: 1, Errors: []string{}}, nil
}

func newSecretRouter(svc Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := New(svc)
	r.GET("/api/v1/certificates", h.ListCertificates)
	r.POST("/api/v1/certificates", h.CreateCertificate)
	r.DELETE("/api/v1/certificates/:id", h.DeleteCertificate)
	r.POST("/api/v1/certificates/:id/reinit", h.ReinitCertificate)
	r.GET("/api/v1/ssh-hosts", h.ListSshHosts)
	r.POST("/api/v1/ssh-hosts", h.CreateSshHost)
	r.PUT("/api/v1/ssh-hosts/:id", h.UpdateSshHost)
	r.DELETE("/api/v1/ssh-hosts/:id", h.DeleteSshHost)
	r.POST("/api/v1/ssh-hosts/import", h.ImportSshHosts)
	return r
}

// multipartBody 构造 multipart 请求体（文本字段 + 可选文件）。
func multipartBody(t *testing.T, fields map[string]string, files map[string]struct{ Name, Content string }) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatalf("写入表单字段失败: %v", err)
		}
	}
	for field, file := range files {
		part, err := writer.CreateFormFile(field, file.Name)
		if err != nil {
			t.Fatalf("创建文件表单项失败: %v", err)
		}
		if _, err := part.Write([]byte(file.Content)); err != nil {
			t.Fatalf("写入文件内容失败: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭 multipart 失败: %v", err)
	}
	return body, writer.FormDataContentType()
}

// TestCreateCertificateMultipart 导入证书：文件与表单字段正确进入业务层，响应不含密钥材料。
func TestCreateCertificateMultipart(t *testing.T) {
	stub := &secretStubService{}
	r := newSecretRouter(stub)

	body, contentType := multipartBody(t, map[string]string{
		"name":               "外部证书",
		"issuer":             "Test CA",
		"valid_from":         "2026-01-01",
		"valid_to":           "2027-01-01",
		"associated_service": "gateway",
	}, map[string]struct{ Name, Content string }{
		"file": {Name: "server.crt", Content: "-----BEGIN CERTIFICATE-----\nCERT\n-----END CERTIFICATE-----\n"},
		"key":  {Name: "server.key", Content: "-----BEGIN PRIVATE KEY-----\nKEY\n-----END PRIVATE KEY-----\n"},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/certificates", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if stub.certInput.Name != "外部证书" || stub.certInput.Issuer != "Test CA" {
		t.Fatalf("表单字段未正确绑定: %+v", stub.certInput)
	}
	if stub.certInput.ValidFrom != "2026-01-01" || stub.certInput.ValidTo != "2027-01-01" {
		t.Fatalf("有效期字段未正确绑定: %+v", stub.certInput)
	}
	if !strings.Contains(string(stub.certInput.CertPEM), "BEGIN CERTIFICATE") {
		t.Fatal("证书文件内容未传入业务层")
	}
	if !strings.Contains(string(stub.certInput.KeyPEM), "BEGIN PRIVATE KEY") {
		t.Fatal("私钥文件内容未传入业务层")
	}

	// 响应红线：不得回传私钥/证书内容。
	if strings.Contains(w.Body.String(), "PRIVATE KEY") || strings.Contains(w.Body.String(), "BEGIN CERTIFICATE") {
		t.Fatalf("响应泄露密钥材料: %s", w.Body.String())
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if _, ok := resp.Data["key"]; ok {
		t.Fatal("响应不应包含 key 字段")
	}
	if resp.Data["has_key"] != true || resp.Data["is_builtin"] != false {
		t.Fatalf("响应字段异常: %v", resp.Data)
	}
}

// TestCertificateImportRequiresFile 缺少证书文件 → 10002。
func TestCertificateImportRequiresFile(t *testing.T) {
	r := newSecretRouter(&secretStubService{})
	body, contentType := multipartBody(t, map[string]string{"name": "无文件证书"}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/certificates", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if code := decodeBody(t, w)["code"]; code != float64(10002) {
		t.Fatalf("code = %v, want 10002", code)
	}
}

// TestReinitCertificateRejectsNonBuiltin 非内置证书 reinit → 409/10401。
func TestReinitCertificateRejectsNonBuiltin(t *testing.T) {
	stub := &secretStubService{certReinitErr: errcode.ErrState}
	r := newSecretRouter(stub)

	w := doJSON(r, http.MethodPost, "/api/v1/certificates/9/reinit", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
	}
	if code := decodeBody(t, w)["code"]; code != float64(10401) {
		t.Fatalf("code = %v, want 10401", code)
	}
	if stub.reinitCalled != 9 {
		t.Fatalf("reinit 目标 ID = %d, want 9", stub.reinitCalled)
	}
}

// TestDeleteBuiltinCertificateForbidden 内置证书删除 → 403/26004。
func TestDeleteBuiltinCertificateForbidden(t *testing.T) {
	stub := &secretStubService{certDeleteErr: settingsvc.ErrBuiltinCertProtected}
	r := newSecretRouter(stub)

	w := doJSON(r, http.MethodDelete, "/api/v1/certificates/11", "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
	if code := decodeBody(t, w)["code"]; code != float64(26004) {
		t.Fatalf("code = %v, want 26004", code)
	}
}

// TestCreateSshHostParsesRequest 新增 SSH/Host：请求体字段进入业务层，响应不含私钥。
func TestCreateSshHostParsesRequest(t *testing.T) {
	stub := &secretStubService{}
	r := newSecretRouter(stub)

	body := `{
		"type": "ssh",
		"host": "10.0.0.8",
		"port": 2222,
		"user": "autoperf",
		"auth_type": "key",
		"key": "-----BEGIN PRIVATE KEY-----\nKEY\n-----END PRIVATE KEY-----\n",
		"description": "压测执行机"
	}`
	w := doJSON(r, http.MethodPost, "/api/v1/ssh-hosts", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	got := stub.sshInput
	if got.Type != domain.SshHostTypeSSH || got.Host != "10.0.0.8" || got.Port != 2222 ||
		got.User != "autoperf" || got.AuthType != domain.SshAuthTypeKey {
		t.Fatalf("请求字段绑定异常: %+v", got)
	}
	if !strings.Contains(got.Key, "BEGIN PRIVATE KEY") {
		t.Fatal("私钥未传入业务层")
	}

	if strings.Contains(w.Body.String(), "PRIVATE KEY") {
		t.Fatalf("响应泄露私钥: %s", w.Body.String())
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if _, ok := resp.Data["key"]; ok {
		t.Fatal("响应不应包含 key 字段")
	}
	if resp.Data["has_key"] != true || resp.Data["user"] != "autoperf" {
		t.Fatalf("响应字段异常: %v", resp.Data)
	}
}

// TestUpdateSshHostPassesVersionAndID 修改：路径 ID/乐观锁 version 透传到业务层（10402 由服务层产生）。
func TestUpdateSshHostPassesVersionAndID(t *testing.T) {
	stub := &secretStubService{}
	r := newSecretRouter(stub)

	body := `{"type":"host","host":"h1.example.com","host_entries":["10.0.0.1 h1.example.com"],"version":3}`
	w := doJSON(r, http.MethodPut, "/api/v1/ssh-hosts/21", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if stub.sshUpdateID != 21 {
		t.Fatalf("path ID 未透传: %d", stub.sshUpdateID)
	}
	if stub.sshInput.Version != 3 {
		t.Fatalf("version 未透传: %d", stub.sshInput.Version)
	}
	if len(stub.sshInput.HostEntries) != 1 {
		t.Fatalf("host_entries 未透传: %v", stub.sshInput.HostEntries)
	}
}

// TestImportSshHostsReadsUploadedFile 批量导入：文件内容与文件名透传，响应含导入统计。
func TestImportSshHostsReadsUploadedFile(t *testing.T) {
	stub := &secretStubService{
		importResult: &settingsvc.SshHostImportResult{
			Total: 3, Created: 1, Skipped: 1, Invalid: 1, Errors: []string{"第 3 行：参数缺失"},
		},
	}
	r := newSecretRouter(stub)

	csv := "type,host\nhost,a.example.com\n"
	body, contentType := multipartBody(t, nil, map[string]struct{ Name, Content string }{
		"file": {Name: "hosts.csv", Content: csv},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh-hosts/import", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if stub.importFilename != "hosts.csv" || stub.importContent != csv {
		t.Fatalf("导入文件未透传: name=%q content=%q", stub.importFilename, stub.importContent)
	}
	if stub.importSize != int64(len(csv)) {
		t.Fatalf("文件大小 = %d, want %d", stub.importSize, len(csv))
	}

	var resp struct {
		Data struct {
			Total   int      `json:"total"`
			Created int      `json:"created"`
			Skipped int      `json:"skipped"`
			Invalid int      `json:"invalid"`
			Errors  []string `json:"errors"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Data.Total != 3 || resp.Data.Created != 1 || resp.Data.Skipped != 1 || resp.Data.Invalid != 1 {
		t.Fatalf("导入统计响应异常: %+v", resp.Data)
	}
	if len(resp.Data.Errors) != 1 {
		t.Fatalf("错误样例响应异常: %v", resp.Data.Errors)
	}
}

// TestImportSshHostsRequiresFile 缺少导入文件 → 10002。
func TestImportSshHostsRequiresFile(t *testing.T) {
	r := newSecretRouter(&secretStubService{})
	body, contentType := multipartBody(t, map[string]string{"note": "x"}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh-hosts/import", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || decodeBody(t, w)["code"] != float64(10002) {
		t.Fatalf("缺少文件应返回 400/10002，got %d %s", w.Code, w.Body.String())
	}
}

// TestListCertificatesPaging 列表：分页参数透传并返回统一分页结构。
func TestListCertificatesPaging(t *testing.T) {
	stub := &secretStubService{}
	r := newSecretRouter(stub)

	w := doJSON(r, http.MethodGet, "/api/v1/certificates?page=2&page_size=5", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if stub.certListPage != 2 || stub.certListSize != 5 {
		t.Fatalf("分页参数未透传: page=%d size=%d", stub.certListPage, stub.certListSize)
	}
}
