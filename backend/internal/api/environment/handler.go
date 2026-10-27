// Package environment 环境管理 HTTP handler（分类/标签/环境）。
//
// 对齐 api-contract §2.2 与 §5.2：
//   - 路由权限点：环境/分类 env.list（read/write），标签 env.tag（read/write）；
//   - 环境主机地址按权限裁剪：无 env.list:read 时响应不返回 hosts（DES-I-001）；
//   - ID 一律以字符串输出（api-contract §1.7），请求体兼容字符串/数字 ID。
package environment

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/auth"
	"auto_performance/internal/errcode"
	envsvc "auto_performance/internal/service/environment"
)

// Service 环境管理业务服务能力（*envsvc.Service 实现；便于 handler 层测试注入桩）。
type Service interface {
	// 分类
	ListCategories(ctx context.Context) ([]envsvc.CategoryView, error)
	CreateCategory(ctx context.Context, in envsvc.CategoryInput) (*envsvc.CategoryView, error)
	UpdateCategory(ctx context.Context, categoryID int64, in envsvc.CategoryInput) (*envsvc.CategoryView, error)
	DeleteCategory(ctx context.Context, categoryID int64) error

	// 标签
	ListTags(ctx context.Context, q envsvc.ListTagsQuery) ([]envsvc.TagView, error)
	GetTag(ctx context.Context, tagID int64) (*envsvc.TagView, error)
	CreateTag(ctx context.Context, in envsvc.TagInput) (*envsvc.TagView, error)
	UpdateTag(ctx context.Context, tagID int64, in envsvc.TagInput) (*envsvc.TagView, error)
	DeleteTag(ctx context.Context, tagID int64) error
	TagEnvironments(ctx context.Context, tagID int64, page, pageSize int) (*envsvc.EnvironmentPage, error)

	// 环境
	ListEnvironments(ctx context.Context, q envsvc.ListEnvironmentsQuery) (*envsvc.EnvironmentPage, error)
	GetEnvironment(ctx context.Context, envID int64) (*envsvc.EnvironmentView, error)
	CreateEnvironment(ctx context.Context, in envsvc.EnvironmentInput) (*envsvc.EnvironmentView, error)
	UpdateEnvironment(ctx context.Context, envID int64, in envsvc.EnvironmentInput) (*envsvc.EnvironmentView, error)
	DeleteEnvironment(ctx context.Context, envID int64) error
	UpdateStatus(ctx context.Context, envID int64, status string) error
	BindTags(ctx context.Context, envID int64, tagIDs []int64) error
}

// Handler 环境管理 HTTP handler。
type Handler struct {
	svc Service
}

// New 构造环境管理 handler。
func New(svc Service) *Handler { return &Handler{svc: svc} }

// 编译期断言：业务服务满足 handler 依赖（避免签名漂移）。
var _ Service = (*envsvc.Service)(nil)

// ===================== DTO =====================

// categoryDTO 环境分类响应。
type categoryDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     int64  `json:"version"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// tagDTO 标签响应（含关联环境数）。
type tagDTO struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Color            string `json:"color"`
	Description      string `json:"description"`
	EnvironmentCount int64  `json:"environment_count"`
	Version          int64  `json:"version"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// tagBriefDTO 环境内嵌标签摘要。
type tagBriefDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// environmentDTO 环境响应。
//
// Hosts 为 omitempty：无 env.list:read 权限时不下发主机地址（DES-I-001 按权限裁剪）。
type environmentDTO struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Code         string        `json:"code"`
	CategoryID   string        `json:"category_id"`
	CategoryName string        `json:"category_name"`
	Tags         []tagBriefDTO `json:"tags"`
	Type         string        `json:"type"`
	Hosts        []string      `json:"hosts,omitempty"`
	OS           string        `json:"os"`
	SshHostID    string        `json:"ssh_host_id,omitempty"`
	CertID       string        `json:"cert_id,omitempty"`
	Description  string        `json:"description"`
	Status       string        `json:"status"`
	Version      int64         `json:"version"`
	CreatedAt    string        `json:"created_at"`
	UpdatedAt    string        `json:"updated_at"`
}

// ===================== 请求体 =====================

// flexID 兼容字符串/数字 ID（api-contract §1.7）。
type flexID string

// UnmarshalJSON 接受 "12" / 12 / null。
func (f *flexID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = flexID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		*f = flexID(n.String())
		return nil
	}
	return fmt.Errorf("id 取值非法: %s", string(data))
}

// idList 兼容 ["1","2"] 与 [1,2] 两种 ID 数组写法。
type idList []string

// UnmarshalJSON 逐项接受字符串或数字 ID。
func (l *idList) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("id 列表格式非法: %w", err)
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		var s string
		if err := json.Unmarshal(item, &s); err == nil {
			out = append(out, s)
			continue
		}
		var n json.Number
		if err := json.Unmarshal(item, &n); err == nil {
			out = append(out, n.String())
			continue
		}
		return fmt.Errorf("id 取值非法: %s", string(item))
	}
	*l = out
	return nil
}

// categoryRequest 分类新建/修改请求体（白名单字段）。
type categoryRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// tagRequest 标签新建/修改请求体（白名单字段）。
type tagRequest struct {
	Name        string `json:"name" binding:"required"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// environmentRequest 环境新建/修改请求体（全量字段 + version 乐观锁）。
//
// 故意不声明 status：环境状态只能经 PATCH /environments/{id}/status 变更（防越权改状态）。
type environmentRequest struct {
	Name        string   `json:"name" binding:"required"`
	Code        string   `json:"code" binding:"required"`
	CategoryID  flexID   `json:"category_id" binding:"required"`
	TagIDs      idList   `json:"tag_ids"`
	Type        string   `json:"type"`
	Hosts       []string `json:"hosts" binding:"required"`
	OS          string   `json:"os"`
	SshHostID   flexID   `json:"ssh_host_id"`
	CertID      flexID   `json:"cert_id"`
	Description string   `json:"description"`
	Version     int64    `json:"version"`
}

// bindTagsRequest 环境标签绑定请求体（全量替换）。
type bindTagsRequest struct {
	TagIDs idList `json:"tag_ids"`
}

// envStatusRequest 环境停用/启用请求体。
type envStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// ===================== 解析 / 转换 =====================

// parseIntQuery 解析整型 query 参数；缺失或非法返回 (0,false)（由服务层取默认值）。
func parseIntQuery(c *gin.Context, key string) (int, bool) {
	raw := c.Query(key)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return v, true
}

// parseID 解析路径参数 :id（数字或字符串形式的 bigint）。
func parseID(c *gin.Context) (int64, error) {
	return parseIDValue(c.Param("id"), true)
}

// parseIDValue 解析 bigint 字符串；required 为 true 时 0 视为非法。
func parseIDValue(raw string, required bool) (int64, error) {
	if raw == "" {
		if required {
			return 0, errcode.ErrParamFmt
		}
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 || (required && id == 0) {
		return 0, errcode.ErrParamFmt
	}
	return id, nil
}

// parseIDList 解析字符串 ID 列表为 int64（逐项校验）。
func parseIDList(raw idList, required bool) ([]int64, error) {
	if len(raw) == 0 && required {
		return nil, errcode.ErrParamFmt
	}
	out := make([]int64, 0, len(raw))
	for _, s := range raw {
		id, err := parseIDValue(s, true)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// parseCSVIDs 解析逗号分隔的 query ID 列表（tag_ids=1,2）。
func parseCSVIDs(raw string) ([]int64, error) {
	if raw == "" {
		return nil, nil
	}
	parts := make([]string, 0, 4)
	start := 0
	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == ',' {
			part := raw[start:i]
			if part != "" {
				parts = append(parts, part)
			}
			start = i + 1
		}
	}
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		id, err := parseIDValue(p, true)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// canReadHosts 判断调用方是否具备 env.list:read（无权限则裁剪 hosts，DES-I-001）。
func canReadHosts(c *gin.Context) bool {
	v, ok := c.Get(auth.CtxPerms)
	if !ok {
		return false
	}
	perms, ok := v.(map[string]bool)
	if !ok {
		return false
	}
	return auth.HasPerm(perms, "env.list", "read")
}

// itoa int64 → 十进制字符串（DTO 的 ID 统一为字符串）。
func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func toCategoryDTO(v *envsvc.CategoryView) categoryDTO {
	return categoryDTO{
		ID:          itoa(v.ID),
		Name:        v.Name,
		Description: v.Description,
		Version:     v.Version,
		CreatedAt:   v.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   v.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func toTagDTO(v *envsvc.TagView) tagDTO {
	return tagDTO{
		ID:               itoa(v.ID),
		Name:             v.Name,
		Color:            v.Color,
		Description:      v.Description,
		EnvironmentCount: v.EnvironmentCount,
		Version:          v.Version,
		CreatedAt:        v.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:        v.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

// toEnvironmentDTO 环境视图 → 响应；includeHosts=false 时不下发主机地址。
func toEnvironmentDTO(v *envsvc.EnvironmentView, includeHosts bool) environmentDTO {
	tags := make([]tagBriefDTO, 0, len(v.Tags))
	for _, t := range v.Tags {
		tags = append(tags, tagBriefDTO{ID: itoa(t.ID), Name: t.Name, Color: t.Color})
	}
	dto := environmentDTO{
		ID:           itoa(v.ID),
		Name:         v.Name,
		Code:         v.Code,
		CategoryID:   itoa(v.CategoryID),
		CategoryName: v.CategoryName,
		Tags:         tags,
		Type:         v.Type,
		OS:           v.OS,
		Description:  v.Description,
		Status:       v.Status,
		Version:      v.Version,
		CreatedAt:    v.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:    v.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if v.SshHostID > 0 {
		dto.SshHostID = itoa(v.SshHostID)
	}
	if v.CertID > 0 {
		dto.CertID = itoa(v.CertID)
	}
	if includeHosts {
		dto.Hosts = v.Hosts
	}
	return dto
}

// toEnvironmentListDTO 批量转换环境视图（统一裁剪主机地址）。
func toEnvironmentListDTO(views []envsvc.EnvironmentView, includeHosts bool) []environmentDTO {
	out := make([]environmentDTO, 0, len(views))
	for i := range views {
		out = append(out, toEnvironmentDTO(&views[i], includeHosts))
	}
	return out
}
