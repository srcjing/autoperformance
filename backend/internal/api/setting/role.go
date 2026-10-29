package setting

import (
	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/auth"
	"auto_performance/internal/errcode"
	settingsvc "auto_performance/internal/service/setting"
)

// roleRequest 角色新建/修改请求体（含权限矩阵；无 status/is_super 等提权字段）。
type roleRequest struct {
	Name        string          `json:"name" binding:"required"`
	Description string          `json:"description"`
	Permissions []permissionDTO `json:"permissions"`
	Version     int64           `json:"version"`
}

// ListRoles GET /api/v1/roles（含权限矩阵）
func (h *Handler) ListRoles(c *gin.Context) {
	roles, err := h.svc.ListRoles(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	list := make([]roleDTO, 0, len(roles))
	for i := range roles {
		list = append(list, toRoleDTO(&roles[i]))
	}
	response.OK(c, gin.H{"list": list})
}

// CreateRole POST /api/v1/roles
func (h *Handler) CreateRole(c *gin.Context) {
	var req roleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	view, err := h.svc.CreateRole(c.Request.Context(), settingsvc.RoleInput{
		Name:        req.Name,
		Description: req.Description,
		Permissions: toPermissionGrants(req.Permissions),
		Version:     req.Version,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toRoleDTO(view))
}

// GetRole GET /api/v1/roles/{id}
func (h *Handler) GetRole(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	view, err := h.svc.GetRole(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toRoleDTO(view))
}

// UpdateRole PUT /api/v1/roles/{id}
func (h *Handler) UpdateRole(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	var req roleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	view, err := h.svc.UpdateRole(c.Request.Context(), id, settingsvc.RoleInput{
		Name:        req.Name,
		Description: req.Description,
		Permissions: toPermissionGrants(req.Permissions),
		Version:     req.Version,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toRoleDTO(view))
}

// DeleteRole DELETE /api/v1/roles/{id}（被引用 → 26003）
func (h *Handler) DeleteRole(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	if err := h.svc.DeleteRole(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}

// ListPermissions GET /api/v1/permissions（子模块授权项元数据，含敏感标记）
func (h *Handler) ListPermissions(c *gin.Context) {
	mods := h.svc.PermissionModules()
	if mods == nil {
		mods = []auth.PermissionModule{}
	}
	response.OK(c, gin.H{"list": mods})
}
