package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/auth"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	authsvc "auto_performance/internal/service/auth"
)

// ErrNotImplemented 路由骨架占位错误：HTTP 501，沿用框架级兜底码 10601（不新增 1xxxx 码值）。
// Phase 2~7 各模块实现后，对应路由替换为真实 handler，该占位不再返回。
var ErrNotImplemented = &errcode.Error{
	Code:    errcode.ErrInternal.Code,
	HTTP:    http.StatusNotImplemented,
	Message: "接口尚未实现",
}

// NotImplemented 统一占位 handler：路由/中间件/权限点已就位，业务逻辑待对应 Phase 实现。
func (h *Handler) NotImplemented(c *gin.Context) {
	response.Error(c, ErrNotImplemented)
}

// loginRequest 登录请求体。
type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// loginResponse 登录响应 data（对齐 api-contract §2.1）。
type loginResponse struct {
	Token              string   `json:"token"`
	ExpiresIn          int64    `json:"expires_in"`
	Admin              gin.H    `json:"admin"`
	Permissions        []string `json:"permissions"`
	MustChangePassword bool     `json:"must_change_password"`
}

// changePasswordRequest 改密请求体（首登强制改密同此接口）。
type changePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// Login POST /api/v1/auth/login。
//
// 安全约束（DES-S-001/003/006）：
//   - bcrypt 比对，失败统一提示「用户名或密码错误」（20001），不区分账号不存在/口令错误；
//   - 连续失败锁定（账号 + IP 双维度，429/10502）；
//   - 内置 admin 首登返回 must_change_password=true，其余业务接口由 AuthN 拦截（10103）。
func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParam)
		return
	}
	if h.auth == nil {
		response.Error(c, errcode.ErrDependency)
		return
	}

	res, err := h.auth.Login(c.Request.Context(), authsvc.LoginInput{
		Username: req.Username,
		Password: req.Password,
		IP:       c.ClientIP(),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	ttl := h.cfg.JWT.TTL()
	token, err := auth.GenerateToken(
		strconv.FormatInt(res.Admin.ID, 10), res.Admin.Username, h.cfg.JWT.Secret, ttl)
	if err != nil {
		response.Error(c, errcode.ErrInternal)
		return
	}

	response.OK(c, loginResponse{
		Token:              token,
		ExpiresIn:          int64(ttl.Seconds()),
		Admin:              adminJSON(res.Admin),
		Permissions:        auth.PermissionList(res.Permissions),
		MustChangePassword: res.Admin.MustChangePassword,
	})
}

// Logout POST /api/v1/auth/logout：将当前 token 的 jti 加入失效名单（DES-S-006 登出失效）。
func (h *Handler) Logout(c *gin.Context) {
	if h.tokens != nil {
		if jti, ok := ctxString(c, auth.CtxTokenID); ok && jti != "" {
			h.tokens.RevokeToken(jti, remainingTTL(c, h.cfg.JWT.TTL()))
		}
	}
	response.OK(c, nil)
}

// ChangePassword POST /api/v1/auth/change-password。
//
// 旧口令错误 20003、复杂度不足 20004、新旧相同 20005；成功后清除 must_change_password
// 并失效当前 token（需重新登录）。
func (h *Handler) ChangePassword(c *gin.Context) {
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParam)
		return
	}
	if h.auth == nil {
		response.Error(c, errcode.ErrDependency)
		return
	}

	adminID, ok := ctxAdminID(c)
	if !ok {
		response.Error(c, errcode.ErrUnauth)
		return
	}

	if err := h.auth.ChangePassword(c.Request.Context(), adminID, req.OldPassword, req.NewPassword); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 改密后当前会话失效：强制重新登录（避免旧 token 继续使用）。
	if h.tokens != nil {
		if jti, ok := ctxString(c, auth.CtxTokenID); ok && jti != "" {
			h.tokens.RevokeToken(jti, remainingTTL(c, h.cfg.JWT.TTL()))
		}
	}

	response.OK(c, nil)
}

// Me GET /api/v1/auth/me：返回当前用户信息与权限并集（由 AuthN 注入）。
func (h *Handler) Me(c *gin.Context) {
	adminID, ok := ctxAdminID(c)
	if !ok {
		response.Error(c, errcode.ErrUnauth)
		return
	}

	var perms []string
	if v, ok := c.Get(auth.CtxPerms); ok {
		if m, ok := v.(map[string]bool); ok {
			perms = auth.PermissionList(m)
		}
	}
	if perms == nil {
		perms = []string{}
	}

	mustPwd, _ := ctxBool(c, auth.CtxMustPwd)

	admin := gin.H{
		"id":                   strconv.FormatInt(adminID, 10),
		"username":             "",
		"must_change_password": mustPwd,
	}
	if username, ok := ctxString(c, auth.CtxUsername); ok {
		admin["username"] = username
	}

	// 从仓储读取完整资料（口令哈希不出栈，见 adminJSON）。
	if h.auth != nil {
		a, err := h.auth.GetAdmin(c.Request.Context(), adminID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		admin = adminJSON(a)
		admin["must_change_password"] = mustPwd
	}

	response.OK(c, gin.H{"admin": admin, "permissions": perms})
}

// adminJSON 管理员 DTO（显式字段白名单：绝不输出 password_hash）。
func adminJSON(a *domain.Admin) gin.H {
	out := gin.H{
		"id":                   strconv.FormatInt(a.ID, 10),
		"username":             a.Username,
		"name":                 a.Name,
		"phone":                a.Phone,
		"email":                a.Email,
		"description":          a.Description,
		"status":               a.Status,
		"is_builtin":           a.IsBuiltin,
		"must_change_password": a.MustChangePassword,
		"version":              a.Version,
	}
	if a.LastLoginAt != nil {
		out["last_login_at"] = a.LastLoginAt.UTC().Format(time.RFC3339)
	} else {
		out["last_login_at"] = nil
	}
	return out
}

// ctxAdminID 读取 AuthN 注入的管理员 ID（十进制字符串）。
func ctxAdminID(c *gin.Context) (int64, bool) {
	s, ok := ctxString(c, auth.CtxAdminID)
	if !ok || s == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// ctxBool 读取布尔型上下文值。
func ctxBool(c *gin.Context, key string) (bool, bool) {
	v, ok := c.Get(key)
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// ctxString 读取字符串型上下文值。
func ctxString(c *gin.Context, key string) (string, bool) {
	v, ok := c.Get(key)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// remainingTTL 计算 token 剩余有效期（用于 jti 黑名单 TTL）；取不到时用配置 TTL 兜底。
func remainingTTL(c *gin.Context, fallback time.Duration) time.Duration {
	v, ok := c.Get(auth.CtxTokenExp)
	if !ok {
		return fallback
	}
	exp, ok := v.(time.Time)
	if !ok {
		return fallback
	}
	if d := time.Until(exp); d > 0 {
		return d
	}
	return time.Second
}
