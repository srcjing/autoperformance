// Package api 负责 REST 路由与 handler 装配（接入层）。
//
// 中间件链（对齐 architecture.md §5.3.2）：
//
//	Recovery → CORS → AuditLog(HMAC 链落库) → AuthN(JWT) → AuthZ(RBAC) → Handler
package api

import (
	"io/fs"
	"log"

	"github.com/gin-gonic/gin"

	envapi "auto_performance/internal/api/environment"
	"auto_performance/internal/api/handler"
	logapi "auto_performance/internal/api/log"
	"auto_performance/internal/api/middleware"
	productapi "auto_performance/internal/api/product"
	reportapi "auto_performance/internal/api/report"
	"auto_performance/internal/api/response"
	settingapi "auto_performance/internal/api/setting"
	taskapi "auto_performance/internal/api/task"
	"auto_performance/internal/config"
	"auto_performance/internal/errcode"
	authsvc "auto_performance/internal/service/auth"
	logsvc "auto_performance/internal/service/log"
	"auto_performance/internal/ws"
	"auto_performance/web"
)

// Deps 路由装配依赖（由 cmd/server 统一构造后注入）。
type Deps struct {
	Config *config.Config
	// DB 数据库连通性探测器（可为 nil）。
	DB handler.Pinger
	// Auth 认证服务（认证中间件与登出失效使用；可为 nil，无 DB 时降级）。
	Auth *authsvc.Service
	// Settings 系统设置服务（管理员/角色/权限矩阵；可为 nil，降级为 501 骨架）。
	Settings settingapi.Service
	// Environments 环境管理服务（分类/标签/环境；可为 nil，降级为 501 骨架）。
	Environments envapi.Service
	// Products 产品管理服务（产品/接口/阈值；可为 nil，降级为 501 骨架）。
	Products productapi.Service
	// Tasks 测试管理服务（模板/任务/执行控制；可为 nil，降级为 501 骨架）。
	Tasks taskapi.Service
	// Reports 报告管理服务（列表/详情/生成/在线查看/导出/导入；可为 nil，降级为 501 骨架）。
	Reports reportapi.Service
	// Logs 日志管理服务（用户/系统/测试日志只读查询；可为 nil，降级为 501 骨架）。
	Logs logapi.Service
	// Hub WebSocket 实时通道（可为 nil，/ws 返回 10602）。
	Hub *ws.Hub
	// Audit 审计日志服务（HMAC 链追加；可为 nil，降级为本地打印）。
	Audit *logsvc.Service
	// AuditKey 审计链 HMAC 密钥。
	AuditKey []byte
	// Static 前端静态资源文件系统（以 dist 为根；为 nil 时使用 go:embed 内嵌产物）。
	// 非 /api、/healthz 的未命中路径回退 index.html，支撑单二进制交付与 SPA 深链。
	Static fs.FS
}

// NewRouter 装配路由与中间件链。
func NewRouter(deps Deps) *gin.Engine {
	cfg := deps.Config
	if cfg.Server.Mode == gin.ReleaseMode {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()

	// 全局中间件链
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.AuditLog(deps.Audit, deps.AuditKey))

	h := handler.New(handler.Deps{DB: deps.DB, Config: cfg, Tokens: deps.Auth, Auth: deps.Auth})

	// 健康检查（公开，不经过鉴权）
	r.GET("/healthz", h.Health)

	// 业务路由组（统一前缀 /api/v1）
	v1 := r.Group("/api/v1")
	{
		// 认证模块：登录公开，其余需已认证（无子模块权限要求）
		authGroup := v1.Group("/auth")
		authGroup.POST("/login", h.Login)
		authed := authGroup.Group("")
		authed.Use(middleware.AuthN(cfg.JWT.Secret, deps.Auth))
		authed.POST("/logout", h.Logout)
		authed.POST("/change-password", h.ChangePassword)
		authed.GET("/me", h.Me)

		// 实时通道：WS /api/v1/ws?token=<jwt>（握手鉴权，token 不走 Authorization 头）
		wsHandler := func(c *gin.Context) { response.Error(c, errcode.ErrDependency) }
		if deps.Hub != nil {
			wsHandler = deps.Hub.Handler()
		}
		v1.GET("/ws", wsHandler)

		// 业务模块：AuthN + 逐路由 AuthZ（权限点见 specs/api-contract.md §5.2）
		protected := v1.Group("")
		protected.Use(middleware.AuthN(cfg.JWT.Secret, deps.Auth))

		// 系统设置 · 管理员/角色/权限矩阵（Phase 1 已实现；敏感子模块强制 write）
		var settingHandler *settingapi.Handler
		if deps.Settings != nil {
			settingHandler = settingapi.New(deps.Settings)
		}
		for _, rt := range settingAdminRoutes() {
			fn := h.NotImplemented
			if settingHandler != nil {
				handlerFn, settingFn := rt.Handler, settingHandler
				fn = func(c *gin.Context) { handlerFn(settingFn, c) }
			}
			protected.Handle(rt.Method, rt.Path, middleware.AuthZ("setting.admin", "write"), fn)
		}

		// 环境管理 · 分类/标签/环境（Phase 2 已实现；env.list / env.tag 读写分离）。
		var envHandler *envapi.Handler
		if deps.Environments != nil {
			envHandler = envapi.New(deps.Environments)
		}
		for _, rt := range environmentRoutes() {
			fn := h.NotImplemented
			if envHandler != nil {
				handlerFn, envFn := rt.Handler, envHandler
				fn = func(c *gin.Context) { handlerFn(envFn, c) }
			}
			protected.Handle(rt.Method, rt.Path, middleware.AuthZ(rt.Module, rt.Perm), fn)
		}

		// 产品管理 · 产品/接口/阈值（Phase 3 已实现；product.list / product.interface 读写分离）。
		var prodHandler *productapi.Handler
		if deps.Products != nil {
			prodHandler = productapi.New(deps.Products)
		}
		for _, rt := range productRoutes() {
			fn := h.NotImplemented
			if prodHandler != nil {
				handlerFn, prodFn := rt.Handler, prodHandler
				fn = func(c *gin.Context) { handlerFn(prodFn, c) }
			}
			protected.Handle(rt.Method, rt.Path, middleware.AuthZ(rt.Module, rt.Perm), fn)
		}

		// 测试管理 · 模板/任务/执行控制（Phase 4 已实现；testing.template / testing.task 读写分离）。
		var taskHandler *taskapi.Handler
		if deps.Tasks != nil {
			taskHandler = taskapi.New(deps.Tasks)
		}
		for _, rt := range taskRoutes() {
			fn := h.NotImplemented
			if taskHandler != nil {
				handlerFn, taskFn := rt.Handler, taskHandler
				fn = func(c *gin.Context) { handlerFn(taskFn, c) }
			}
			protected.Handle(rt.Method, rt.Path, middleware.AuthZ(rt.Module, rt.Perm), fn)
		}

		// 报告管理 · 报告列表（Phase 5 已实现；report.list:read/write 分离）。
		var reportHandler *reportapi.Handler
		if deps.Reports != nil {
			reportHandler = reportapi.New(deps.Reports, cfg.Report.MaxSizeBytes)
		}
		for _, rt := range reportRoutes() {
			fn := h.NotImplemented
			if reportHandler != nil {
				handlerFn, reportFn := rt.Handler, reportHandler
				fn = func(c *gin.Context) { handlerFn(reportFn, c) }
			}
			protected.Handle(rt.Method, rt.Path, middleware.AuthZ(rt.Module, rt.Perm), fn)
		}

		// 日志管理 · 用户/系统/测试日志（Phase 6 已实现；日志默认只读，无写路由）。
		var logHandler *logapi.Handler
		if deps.Logs != nil {
			logHandler = logapi.New(deps.Logs)
		}
		for _, rt := range append(logRoutes(), logSettingRoutes()...) {
			fn := h.NotImplemented
			if logHandler != nil {
				handlerFn, logFn := rt.Handler, logHandler
				fn = func(c *gin.Context) { handlerFn(logFn, c) }
			}
			protected.Handle(rt.Method, rt.Path, middleware.AuthZ(rt.Module, rt.Perm), fn)
		}

		// 系统设置 · 证书 / SSH-Host（Phase 7 已实现；敏感子模块统一 write-only）。
		for _, rt := range settingSecretRoutes() {
			fn := h.NotImplemented
			if settingHandler != nil {
				handlerFn, settingFn := rt.Handler, settingHandler
				fn = func(c *gin.Context) { handlerFn(settingFn, c) }
			}
			protected.Handle(rt.Method, rt.Path, middleware.AuthZ(rt.Module, rt.Perm), fn)
		}
	}

	// 静态资源 + 兜底 404：/api 未命中仍返回统一 JSON，其余路径回退前端 index.html（SPA）。
	// 前端产物以 go:embed 内嵌（web.FS），单二进制即可提供页面，无需额外静态服务器。
	staticFS := deps.Static
	if staticFS == nil {
		sub, err := web.Dist()
		if err != nil {
			log.Printf("[router] 内嵌前端产物不可用（仅提供 API）: %v", err)
			staticFS = nil
		} else {
			staticFS = sub
		}
	}
	r.NoRoute(newSPAHandler(staticFS))

	return r
}

// settingRoute 系统设置·管理员/角色路由：方法 + 路径 + handler 方法（权限点统一 setting.admin:write）。
type settingRoute struct {
	Method  string
	Path    string
	Handler func(*settingapi.Handler, *gin.Context)
}

// settingAdminRoutes 管理员/角色/权限矩阵路由（Phase 1 任务 1.3/1.4）。
// 路径与 api-contract §2.7 一致；由 AuthZ 强制 setting.admin:write（敏感子模块 write-only）。
func settingAdminRoutes() []settingRoute {
	return []settingRoute{
		{"GET", "/admins", (*settingapi.Handler).ListAdmins},
		{"POST", "/admins", (*settingapi.Handler).CreateAdmin},
		{"GET", "/admins/:id", (*settingapi.Handler).GetAdmin},
		{"PUT", "/admins/:id", (*settingapi.Handler).UpdateAdmin},
		{"DELETE", "/admins/:id", (*settingapi.Handler).DeleteAdmin},
		{"PATCH", "/admins/:id/status", (*settingapi.Handler).UpdateAdminStatus},
		{"POST", "/admins/:id/reset-password", (*settingapi.Handler).ResetAdminPassword},
		{"GET", "/admins/:id/roles", (*settingapi.Handler).GetAdminRoles},
		{"POST", "/admins/:id/roles", (*settingapi.Handler).BindAdminRoles},
		{"GET", "/roles", (*settingapi.Handler).ListRoles},
		{"POST", "/roles", (*settingapi.Handler).CreateRole},
		{"GET", "/roles/:id", (*settingapi.Handler).GetRole},
		{"PUT", "/roles/:id", (*settingapi.Handler).UpdateRole},
		{"DELETE", "/roles/:id", (*settingapi.Handler).DeleteRole},
		{"GET", "/permissions", (*settingapi.Handler).ListPermissions},
	}
}

// environmentRoute 环境管理路由：方法 + 路径 + 权限点 + handler 方法。
type environmentRoute struct {
	Method  string
	Path    string
	Module  string // env.list | env.tag（api-contract §5.1）
	Perm    string // read | write（api-contract §5.2）
	Handler func(*envapi.Handler, *gin.Context)
}

// environmentRoutes 环境管理 16 条路由（Phase 2 任务 2.1~2.3）。
// 路径、权限点与 api-contract §2.2 一致，AuthZ 负责服务端最终鉴权。
func environmentRoutes() []environmentRoute {
	return []environmentRoute{
		// 环境分类
		{"GET", "/environment-categories", "env.list", "read", (*envapi.Handler).ListCategories},
		{"POST", "/environment-categories", "env.list", "write", (*envapi.Handler).CreateCategory},
		{"PUT", "/environment-categories/:id", "env.list", "write", (*envapi.Handler).UpdateCategory},
		{"DELETE", "/environment-categories/:id", "env.list", "write", (*envapi.Handler).DeleteCategory},

		// 标签
		{"GET", "/tags", "env.tag", "read", (*envapi.Handler).ListTags},
		{"POST", "/tags", "env.tag", "write", (*envapi.Handler).CreateTag},
		{"PUT", "/tags/:id", "env.tag", "write", (*envapi.Handler).UpdateTag},
		{"DELETE", "/tags/:id", "env.tag", "write", (*envapi.Handler).DeleteTag},
		{"GET", "/tags/:id/environments", "env.tag", "read", (*envapi.Handler).TagEnvironments},

		// 环境
		{"GET", "/environments", "env.list", "read", (*envapi.Handler).ListEnvironments},
		{"POST", "/environments", "env.list", "write", (*envapi.Handler).CreateEnvironment},
		{"GET", "/environments/:id", "env.list", "read", (*envapi.Handler).GetEnvironment},
		{"PUT", "/environments/:id", "env.list", "write", (*envapi.Handler).UpdateEnvironment},
		{"DELETE", "/environments/:id", "env.list", "write", (*envapi.Handler).DeleteEnvironment},
		{"PUT", "/environments/:id/tags", "env.list", "write", (*envapi.Handler).BindTags},
		{"PATCH", "/environments/:id/status", "env.list", "write", (*envapi.Handler).UpdateStatus},
	}
}

// productRoute 产品管理路由：方法 + 路径 + 权限点 + handler 方法。
type productRoute struct {
	Method  string
	Path    string
	Module  string // product.list | product.interface（api-contract §5.1）
	Perm    string // read | write（api-contract §5.2）
	Handler func(*productapi.Handler, *gin.Context)
}

// productRoutes 产品管理 13 条路由（Phase 3 任务 3.1~3.3）。
// 路径、权限点与 api-contract §2.3 一致，AuthZ 负责服务端最终鉴权（INV-1）。
func productRoutes() []productRoute {
	return []productRoute{
		// 产品
		{"GET", "/products", "product.list", "read", (*productapi.Handler).ListProducts},
		{"POST", "/products", "product.list", "write", (*productapi.Handler).CreateProduct},
		{"GET", "/products/:id", "product.list", "read", (*productapi.Handler).GetProduct},
		{"PUT", "/products/:id", "product.list", "write", (*productapi.Handler).UpdateProduct},
		{"DELETE", "/products/:id", "product.list", "write", (*productapi.Handler).DeleteProduct},
		{"PATCH", "/products/:id/status", "product.list", "write", (*productapi.Handler).UpdateStatus},

		// 接口
		{"GET", "/products/:id/interfaces", "product.interface", "read", (*productapi.Handler).ListInterfaces},
		{"POST", "/products/:id/interfaces", "product.interface", "write", (*productapi.Handler).CreateInterface},
		{"PUT", "/interfaces/:id", "product.interface", "write", (*productapi.Handler).UpdateInterface},
		{"DELETE", "/interfaces/:id", "product.interface", "write", (*productapi.Handler).DeleteInterface},
		{"PATCH", "/interfaces/:id/status", "product.interface", "write", (*productapi.Handler).UpdateInterfaceStatus},

		// 阈值（全量替换）
		{"GET", "/interfaces/:id/thresholds", "product.interface", "read", (*productapi.Handler).ListThresholds},
		{"PUT", "/interfaces/:id/thresholds", "product.interface", "write", (*productapi.Handler).ReplaceThresholds},
	}
}

// taskRoute 测试管理路由：方法 + 路径 + 权限点 + handler 方法。
type taskRoute struct {
	Method  string
	Path    string
	Module  string // testing.template | testing.task（api-contract §5.1）
	Perm    string // read | write
	Handler func(*taskapi.Handler, *gin.Context)
}

// taskRoutes 测试管理 12 条路由（Phase 4 任务 4.1/4.7）。
// 路径与权限点对齐 api-contract §2.4；状态机/环境互斥在服务层强制。
func taskRoutes() []taskRoute {
	return []taskRoute{
		// 模板
		{"GET", "/templates", "testing.template", "read", (*taskapi.Handler).ListTemplates},
		{"POST", "/templates", "testing.template", "write", (*taskapi.Handler).CreateTemplate},
		{"GET", "/templates/:id", "testing.template", "read", (*taskapi.Handler).GetTemplate},
		{"PUT", "/templates/:id", "testing.template", "write", (*taskapi.Handler).UpdateTemplate},
		{"DELETE", "/templates/:id", "testing.template", "write", (*taskapi.Handler).DeleteTemplate},

		// 任务
		{"POST", "/tasks", "testing.task", "write", (*taskapi.Handler).CreateTask},
		{"GET", "/tasks", "testing.task", "read", (*taskapi.Handler).ListTasks},
		{"GET", "/tasks/:id", "testing.task", "read", (*taskapi.Handler).GetTask},
		{"POST", "/tasks/:id/start", "testing.task", "write", (*taskapi.Handler).StartTask},
		{"POST", "/tasks/:id/refresh-snapshot", "testing.task", "write", (*taskapi.Handler).RefreshSnapshotTask},
		{"POST", "/tasks/:id/cancel", "testing.task", "write", (*taskapi.Handler).CancelTask},
		{"POST", "/tasks/:id/retry", "testing.task", "write", (*taskapi.Handler).RetryTask},
		{"POST", "/tasks/:id/cleanup", "testing.task", "write", (*taskapi.Handler).CleanupTask},
	}
}

// reportRoute 报告管理路由：方法 + 路径 + 权限点 + handler 方法。
type reportRoute struct {
	Method  string
	Path    string
	Module  string // report.list（api-contract §5.1）
	Perm    string // read | write（api-contract §5.2）
	Handler func(*reportapi.Handler, *gin.Context)
}

// reportRoutes 报告管理 6 条路由（Phase 5 任务 5.4/5.5）。
// 路径与权限点对齐 api-contract §2.5：查询/查看/导出 read，重新生成/导入 write。
func reportRoutes() []reportRoute {
	return []reportRoute{
		{"GET", "/reports", "report.list", "read", (*reportapi.Handler).ListReports},
		{"GET", "/reports/:id", "report.list", "read", (*reportapi.Handler).GetReport},
		{"POST", "/reports/:id/generate", "report.list", "write", (*reportapi.Handler).GenerateReport},
		{"POST", "/reports/import", "report.list", "write", (*reportapi.Handler).Import},
		{"GET", "/reports/:id/export", "report.list", "read", (*reportapi.Handler).ExportReport},
		{"GET", "/reports/:id/artifact", "report.list", "read", (*reportapi.Handler).GetArtifact},
	}
}

// logRoute 日志管理路由：方法 + 路径 + 权限点 + handler 方法。
type logRoute struct {
	Method  string
	Path    string
	Module  string // log.user | log.system | log.test | log.request（api-contract §5.1）
	Perm    string // read（日志默认只读，api-contract §5.3）
	Handler func(*logapi.Handler, *gin.Context)
}

// logRoutes 日志管理 4 条只读查询路由（用户/系统/测试/请求详情）。
// 仅注册 GET 查询：无删除/修改端点，审计防篡改靠 HMAC 链 + DB 触发器（DES-T-006）。
func logRoutes() []logRoute {
	return []logRoute{
		{"GET", "/logs/user", "log.user", "read", (*logapi.Handler).ListUserLogs},
		{"GET", "/logs/system", "log.system", "read", (*logapi.Handler).ListSystemLogs},
		{"GET", "/logs/test", "log.test", "read", (*logapi.Handler).ListTestLogs},
		{"GET", "/logs/request", "log.request", "read", (*logapi.Handler).ListRequestLogs},
	}
}

// logSettingRoutes 日志设置 2 条路由（读写分离：查看 read，修改 write）。
//
// 日志设置归入 setting.log（日志管理 · 配置）子模块：只读角色可查看当前留存策略，
// 修改留存天数/最大条数/启用类型需 write，避免无关角色篡改取证范围。
func logSettingRoutes() []logRoute {
	return []logRoute{
		{"GET", "/log-settings", "setting.log", "read", (*logapi.Handler).GetLogSetting},
		{"PUT", "/log-settings", "setting.log", "write", (*logapi.Handler).UpdateLogSetting},
	}
}

// settingSecretRoute 系统设置·证书/SSH-Host 路由：方法 + 路径 + 权限点 + handler 方法。
type settingSecretRoute struct {
	Method  string
	Path    string
	Module  string // setting.cert | setting.ssh（api-contract §5.1，敏感 write-only）
	Perm    string // write（api-contract §5.3：敏感子模块仅授权 write）
	Handler func(*settingapi.Handler, *gin.Context)
}

// settingSecretRoutes 系统设置证书 4 条 + SSH/Host 5 条路由（Phase 7 任务 7.1/7.2）。
// 路由与权限点在此保持唯一事实源，避免权限漂移（INV-1 / DES-E-007）。
func settingSecretRoutes() []settingSecretRoute {
	return []settingSecretRoute{
		// 证书（setting.cert，敏感 write-only）
		{"GET", "/certificates", "setting.cert", "write", (*settingapi.Handler).ListCertificates},
		{"POST", "/certificates", "setting.cert", "write", (*settingapi.Handler).CreateCertificate},
		{"DELETE", "/certificates/:id", "setting.cert", "write", (*settingapi.Handler).DeleteCertificate},
		{"POST", "/certificates/:id/reinit", "setting.cert", "write", (*settingapi.Handler).ReinitCertificate},

		// SSH/Host（setting.ssh，敏感 write-only）
		{"GET", "/ssh-hosts", "setting.ssh", "write", (*settingapi.Handler).ListSshHosts},
		{"POST", "/ssh-hosts", "setting.ssh", "write", (*settingapi.Handler).CreateSshHost},
		{"POST", "/ssh-hosts/import", "setting.ssh", "write", (*settingapi.Handler).ImportSshHosts},
		{"PUT", "/ssh-hosts/:id", "setting.ssh", "write", (*settingapi.Handler).UpdateSshHost},
		{"DELETE", "/ssh-hosts/:id", "setting.ssh", "write", (*settingapi.Handler).DeleteSshHost},
	}
}
