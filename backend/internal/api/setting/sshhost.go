package setting

import (
	"errors"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/errcode"
	settingsvc "auto_performance/internal/service/setting"
)

// sshHostDTO SSH/Host 响应（私钥脱敏为 has_key 标记，绝不回传密钥材料）。
type sshHostDTO struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	User        string   `json:"user"`
	AuthType    string   `json:"auth_type"`
	HasKey      bool     `json:"has_key"`
	HostEntries []string `json:"host_entries"`
	Description string   `json:"description"`
	Version     int64    `json:"version"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// sshHostRequest 新增/修改请求体（白名单字段；key 为私钥明文，仅入参不落库）。
type sshHostRequest struct {
	Type        string   `json:"type" binding:"required"`
	Host        string   `json:"host" binding:"required"`
	Port        int      `json:"port"`
	User        string   `json:"user"`
	AuthType    string   `json:"auth_type"`
	Key         string   `json:"key"`
	HostEntries []string `json:"host_entries"`
	Description string   `json:"description"`
	Version     int64    `json:"version"`
}

// toInput 请求体 → 服务输入。
func (r sshHostRequest) toInput() settingsvc.SshHostInput {
	return settingsvc.SshHostInput{
		Type:        r.Type,
		Host:        r.Host,
		Port:        r.Port,
		User:        r.User,
		AuthType:    r.AuthType,
		Key:         r.Key,
		HostEntries: r.HostEntries,
		Description: r.Description,
		Version:     r.Version,
	}
}

// ListSshHosts GET /api/v1/ssh-hosts（setting.ssh:write）
func (h *Handler) ListSshHosts(c *gin.Context) {
	page, _ := parseIntQuery(c, "page")
	pageSize, _ := parseIntQuery(c, "page_size")

	res, err := h.svc.ListSshHosts(c.Request.Context(), settingsvc.ListSshHostsQuery{
		Keyword:  c.Query("keyword"),
		Type:     c.Query("type"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	list := make([]sshHostDTO, 0, len(res.List))
	for i := range res.List {
		list = append(list, toSshHostDTO(&res.List[i]))
	}
	response.OK(c, gin.H{
		"list":      list,
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
	})
}

// CreateSshHost POST /api/v1/ssh-hosts（setting.ssh:write）
func (h *Handler) CreateSshHost(c *gin.Context) {
	var req sshHostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	view, err := h.svc.CreateSshHost(c.Request.Context(), req.toInput())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toSshHostDTO(view))
}

// UpdateSshHost PUT /api/v1/ssh-hosts/{id}（10301 / 10402）
func (h *Handler) UpdateSshHost(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	var req sshHostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	view, err := h.svc.UpdateSshHost(c.Request.Context(), id, req.toInput())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toSshHostDTO(view))
}

// DeleteSshHost DELETE /api/v1/ssh-hosts/{id}（被环境引用 → 26005）
func (h *Handler) DeleteSshHost(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	if err := h.svc.DeleteSshHost(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}

// ImportSshHosts POST /api/v1/ssh-hosts/import（multipart：JSON/CSV 文件，重复项跳过）
func (h *Handler) ImportSshHosts(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body,
		settingsvc.SshHostMaxImportBytes+(1<<20))

	fileHeader, err := c.FormFile("file")
	if err != nil {
		if isBodyTooLarge(err) || errors.Is(err, multipart.ErrMessageTooLarge) {
			response.Error(c, errcode.ErrParamFmt)
			return
		}
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	if fileHeader.Size <= 0 {
		response.Error(c, errcode.ErrParamMiss)
		return
	}

	content, err := fileHeader.Open()
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	defer func() { _ = content.Close() }()

	res, err := h.svc.ImportSshHosts(c.Request.Context(), fileHeader.Filename, fileHeader.Size, content)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	errorsOut := res.Errors
	if errorsOut == nil {
		errorsOut = []string{}
	}
	response.OK(c, gin.H{
		"total":   res.Total,
		"created": res.Created,
		"skipped": res.Skipped,
		"invalid": res.Invalid,
		"errors":  errorsOut,
	})
}

// toSshHostDTO SSH/Host 视图 → 响应。
func toSshHostDTO(v *settingsvc.SshHostView) sshHostDTO {
	entries := v.HostEntries
	if entries == nil {
		entries = []string{}
	}
	return sshHostDTO{
		ID:          itoa(v.ID),
		Type:        v.Type,
		Host:        v.Host,
		Port:        v.Port,
		User:        v.User,
		AuthType:    v.AuthType,
		HasKey:      v.HasKey,
		HostEntries: entries,
		Description: v.Description,
		Version:     v.Version,
		CreatedAt:   v.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   v.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
