package handler

import (
	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/auth"
	"auto_performance/internal/errcode"
)

// loginRequest 登录请求体。
type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// loginResponse 登录响应 data。
type loginResponse struct {
	Token              string   `json:"token"`
	ExpiresIn          int64    `json:"expires_in"`
	Admin              gin.H    `json:"admin"`
	Permissions        []string `json:"permissions"`
	MustChangePassword bool     `json:"must_change_password"`
}

// Login POST /api/v1/auth/login（骨架：任意账号返回占位 token，体现统一响应结构）。
//
// 正式实现：bcrypt 比对 + 登录失败统一提示（20001）+ 账号/IP 双维度锁定（10502）。
func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParam)
		return
	}

	ttl := h.cfg.JWT.TTL()
	token, err := auth.GenerateToken("1", req.Username, h.cfg.JWT.Secret, ttl)
	if err != nil {
		response.Error(c, errcode.ErrInternal)
		return
	}

	perms := auth.PermissionList(auth.SkeletonPermissions())

	response.OK(c, loginResponse{
		Token:              token,
		ExpiresIn:          int64(ttl.Seconds()),
		Admin:              gin.H{"id": "1", "username": req.Username, "name": req.Username},
		Permissions:        perms,
		MustChangePassword: false,
	})
}

// Logout POST /api/v1/auth/logout（骨架占位：正式将 token 加入短期失效名单）。
func (h *Handler) Logout(c *gin.Context) {
	response.OK(c, nil)
}

// ChangePassword POST /api/v1/auth/change-password（骨架占位）。
func (h *Handler) ChangePassword(c *gin.Context) {
	response.OK(c, nil)
}

// Me GET /api/v1/auth/me（返回上下文中的当前用户与权限集合）。
func (h *Handler) Me(c *gin.Context) {
	username, _ := c.Get(auth.CtxUsername)
	permsVal, _ := c.Get(auth.CtxPerms)

	var perms []string
	if m, ok := permsVal.(map[string]bool); ok {
		perms = auth.PermissionList(m)
	}

	response.OK(c, gin.H{
		"admin":       gin.H{"id": "1", "username": username},
		"permissions": perms,
	})
}
