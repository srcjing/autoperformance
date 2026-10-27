package setting

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/errcode"
	settingsvc "auto_performance/internal/service/setting"
)

// 证书导入上限（单个文件 1 MiB；multipart 整体 4 MiB）。
const (
	certMaxFileBytes    int64 = 1 << 20
	certMaxRequestBytes int64 = 4 << 20
	certDateLayout            = "2006-01-02"
)

// certificateDTO 证书响应（脱敏：不下发证书内容与私钥，仅 has_key 标记）。
type certificateDTO struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Issuer            string `json:"issuer"`
	ValidFrom         string `json:"valid_from"`
	ValidTo           string `json:"valid_to"`
	AssociatedService string `json:"associated_service"`
	IsBuiltin         bool   `json:"is_builtin"`
	Status            string `json:"status"`
	HasKey            bool   `json:"has_key"`
	Version           int64  `json:"version"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

// ListCertificates GET /api/v1/certificates（setting.cert:write）
func (h *Handler) ListCertificates(c *gin.Context) {
	page, _ := parseIntQuery(c, "page")
	pageSize, _ := parseIntQuery(c, "page_size")

	res, err := h.svc.ListCertificates(c.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	list := make([]certificateDTO, 0, len(res.List))
	for i := range res.List {
		list = append(list, toCertificateDTO(&res.List[i]))
	}
	response.OK(c, gin.H{
		"list":      list,
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
	})
}

// CreateCertificate POST /api/v1/certificates（setting.cert:write）
//
// multipart/form-data：file（证书文件，必填）、key（私钥文件，可选）、name（必填）、
// issuer、valid_from/valid_to（YYYY-MM-DD，缺省时按证书内容回填）、associated_service。
// 证书内容与私钥由服务层信封加密落库（DES-I-002），响应不含任何密钥材料。
func (h *Handler) CreateCertificate(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, certMaxRequestBytes)

	certPEM, err := readUploadedFile(c, []string{"file", "cert"}, certMaxFileBytes, true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	keyPEM, err := readUploadedFile(c, []string{"key", "key_file"}, certMaxFileBytes, false)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	view, err := h.svc.CreateCertificate(c.Request.Context(), settingsvc.CertificateInput{
		Name:              c.PostForm("name"),
		Issuer:            c.PostForm("issuer"),
		ValidFrom:         c.PostForm("valid_from"),
		ValidTo:           c.PostForm("valid_to"),
		AssociatedService: c.PostForm("associated_service"),
		CertPEM:           certPEM,
		KeyPEM:            keyPEM,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toCertificateDTO(view))
}

// DeleteCertificate DELETE /api/v1/certificates/{id}（内置证书 → 26004）
func (h *Handler) DeleteCertificate(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	if err := h.svc.DeleteCertificate(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}

// ReinitCertificate POST /api/v1/certificates/{id}/reinit（仅内置证书，否则 10401）
func (h *Handler) ReinitCertificate(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	view, err := h.svc.ReinitCertificate(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toCertificateDTO(view))
}

// readUploadedFile 读取 multipart 文件（按候选字段名依次尝试）；required=false 且缺失时返回 nil。
func readUploadedFile(c *gin.Context, fields []string, maxBytes int64, required bool) ([]byte, error) {
	var header *multipart.FileHeader
	for _, field := range fields {
		if fh, err := c.FormFile(field); err == nil && fh != nil {
			header = fh
			break
		}
	}
	if header == nil {
		if required {
			return nil, errcode.ErrParamMiss
		}
		return nil, nil
	}
	if header.Size <= 0 {
		return nil, errcode.ErrParamMiss
	}
	if header.Size > maxBytes {
		return nil, errcode.ErrParamFmt
	}

	file, err := header.Open()
	if err != nil {
		return nil, errcode.ErrParamFmt
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		if isBodyTooLarge(err) || errors.Is(err, multipart.ErrMessageTooLarge) {
			return nil, errcode.ErrParamFmt
		}
		return nil, errcode.ErrParamFmt
	}
	if int64(len(content)) > maxBytes {
		return nil, errcode.ErrParamFmt
	}
	return content, nil
}

// toCertificateDTO 证书视图 → 响应（日期 YYYY-MM-DD；无有效期时为空串）。
func toCertificateDTO(v *settingsvc.CertificateView) certificateDTO {
	return certificateDTO{
		ID:                itoa(v.ID),
		Name:              v.Name,
		Issuer:            v.Issuer,
		ValidFrom:         formatCertDate(v.ValidFrom),
		ValidTo:           formatCertDate(v.ValidTo),
		AssociatedService: v.AssociatedService,
		IsBuiltin:         v.IsBuiltin,
		Status:            v.Status,
		HasKey:            v.HasKey,
		Version:           v.Version,
		CreatedAt:         v.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:         v.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// isBodyTooLarge 判断是否命中 http.MaxBytesReader 上限。
func isBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

// formatCertDate 有效期格式化（nil → 空串）。
func formatCertDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(certDateLayout)
}
