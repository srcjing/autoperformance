package setting

import (
	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/errcode"
	settingsvc "auto_performance/internal/service/setting"
)

// createAdminRequest 新建管理员请求体（白名单字段，DES-E-002）。
//
// 故意不声明 role / role_ids / status / is_builtin / must_change_password：
// 请求体夹带这些字段会被 JSON 解码器忽略，新建账号默认启用、默认无权限、首登强制改密。
type createAdminRequest struct {
	Username    string `json:"username" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Description string `json:"description"`
	Password    string `json:"password" binding:"required"`
}

// updateAdminRequest 修改管理员请求体（用户名/状态/内置标记不可改）。
type updateAdminRequest struct {
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Description string `json:"description"`
	Version     int64  `json:"version"`
}

// adminStatusRequest 停用/启用请求体。
type adminStatusRequest struct {
	Status  string `json:"status" binding:"required"`
	Version int64  `json:"version"`
}

// resetPasswordRequest 重置口令请求体。
type resetPasswordRequest struct {
	NewPassword string `json:"new_password" binding:"required"`
}

// bindRolesRequest 角色授权请求体（全量替换）。
type bindRolesRequest struct {
	RoleIDs idList `json:"role_ids"`
}

// ListAdmins GET /api/v1/admins
func (h *Handler) ListAdmins(c *gin.Context) {
	page, _ := parseIntQuery(c, "page")
	pageSize, _ := parseIntQuery(c, "page_size")

	res, err := h.svc.ListAdmins(c.Request.Context(), settingsvc.ListAdminsQuery{
		Keyword:  c.Query("keyword"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	list := make([]adminDTO, 0, len(res.List))
	for i := range res.List {
		list = append(list, toAdminDTO(&res.List[i]))
	}
	response.OK(c, gin.H{
		"list":      list,
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
	})
}

// CreateAdmin POST /api/v1/admins
func (h *Handler) CreateAdmin(c *gin.Context) {
	var req createAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return
	}

	view, err := h.svc.CreateAdmin(c.Request.Context(), settingsvc.CreateAdminInput{
		Username:    req.Username,
		Name:        req.Name,
		Phone:       req.Phone,
		Email:       req.Email,
		Description: req.Description,
		Password:    req.Password,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toAdminDTO(view))
}

// GetAdmin GET /api/v1/admins/{id}
func (h *Handler) GetAdmin(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	view, err := h.svc.GetAdmin(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toAdminDTO(view))
}

// UpdateAdmin PUT /api/v1/admins/{id}
func (h *Handler) UpdateAdmin(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	var req updateAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParam)
		return
	}

	view, err := h.svc.UpdateAdmin(c.Request.Context(), id, settingsvc.UpdateAdminInput{
		Name:        req.Name,
		Phone:       req.Phone,
		Email:       req.Email,
		Description: req.Description,
		Version:     req.Version,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toAdminDTO(view))
}

// DeleteAdmin DELETE /api/v1/admins/{id}（内置 admin → 26002）
func (h *Handler) DeleteAdmin(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	if err := h.svc.DeleteAdmin(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}

// UpdateAdminStatus PATCH /api/v1/admins/{id}/status（内置 admin → 26002）
func (h *Handler) UpdateAdminStatus(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	var req adminStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	if err := h.svc.UpdateAdminStatus(c.Request.Context(), id, req.Status, req.Version); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}

// ResetAdminPassword POST /api/v1/admins/{id}/reset-password
func (h *Handler) ResetAdminPassword(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	var req resetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	if err := h.svc.ResetAdminPassword(c.Request.Context(), id, req.NewPassword); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}

// GetAdminRoles GET /api/v1/admins/{id}/roles
func (h *Handler) GetAdminRoles(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	roles, err := h.svc.AdminRoles(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	list := make([]roleBriefDTO, 0, len(roles))
	for _, r := range roles {
		list = append(list, roleBriefDTO{ID: itoa(r.ID), Name: r.Name, Description: r.Description})
	}
	response.OK(c, gin.H{"list": list})
}

// BindAdminRoles POST /api/v1/admins/{id}/roles（全量替换，权限即时生效）
func (h *Handler) BindAdminRoles(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	var req bindRolesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParam)
		return
	}
	roleIDs, err := parseIDs(req.RoleIDs)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	if err := h.svc.BindAdminRoles(c.Request.Context(), id, roleIDs); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}
