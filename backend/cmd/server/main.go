// Package main 是 auto_performance 后端入口。
//
// 职责：解析子命令 → 加载配置 → 初始化数据库 → 装配服务/路由 → 启动 HTTP server（优雅退出）。
//
// 子命令：
//
//	auto_performance [-config <file>]                  启动 HTTP 服务（缺省）
//	auto_performance [-config <file>] migrate up       执行数据库迁移（golang-migrate 内嵌 SQL）
//	auto_performance [-config <file>] migrate down     回滚一步迁移（升级失败回滚）
//	auto_performance [-config <file>] healthcheck      探测本机 /healthz（部署脚本就绪检查）
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"auto_performance/internal/api"
	envapi "auto_performance/internal/api/environment"
	"auto_performance/internal/api/handler"
	logapi "auto_performance/internal/api/log"
	productapi "auto_performance/internal/api/product"
	reportapi "auto_performance/internal/api/report"
	settingapi "auto_performance/internal/api/setting"
	taskapi "auto_performance/internal/api/task"
	"auto_performance/internal/cleaner"
	"auto_performance/internal/config"
	"auto_performance/internal/crypto"
	"auto_performance/internal/domain"
	"auto_performance/internal/engine/k6"
	"auto_performance/internal/errcode"
	"auto_performance/internal/orchestrator"
	"auto_performance/internal/reportgen/pdf"
	"auto_performance/internal/repository"
	authsvc "auto_performance/internal/service/auth"
	envsvc "auto_performance/internal/service/environment"
	logsvc "auto_performance/internal/service/log"
	productsvc "auto_performance/internal/service/product"
	reportsvc "auto_performance/internal/service/report"
	settingsvc "auto_performance/internal/service/setting"
	testingsvc "auto_performance/internal/service/testing"
	"auto_performance/internal/ws"
	"auto_performance/web"
)

// version 通过 -ldflags "-X main.version=<v>" 注入。
var version = "dev"

func main() {
	configPath, positional, showVersion, err := parseArgs(os.Args[1:])
	if err != nil {
		usage(os.Stderr)
		os.Exit(2)
	}
	if showVersion {
		fmt.Println(version)
		return
	}

	// 1. 加载配置（YAML / 环境变量）
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	cmd, sub := "", ""
	if len(positional) > 0 {
		cmd = positional[0]
	}
	if len(positional) > 1 {
		sub = positional[1]
	}
	if len(positional) > 2 {
		usage(os.Stderr)
		os.Exit(2)
	}

	// 2. 子命令分派
	switch cmd {
	case "":
		serve(cfg)
	case "migrate":
		runMigrate(cfg, sub)
	case "healthcheck":
		runHealthcheck(cfg)
	case "tls-init":
		runTLSInit(cfg)
	case "version":
		fmt.Println(version)
	default:
		log.Printf("未知子命令: %s", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
}

// parseArgs 解析命令行：-config/-version 可出现在任意位置，其余为位置参数（子命令）。
func parseArgs(args []string) (configPath string, positional []string, showVersion bool, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-config" || a == "--config":
			if i+1 >= len(args) {
				return "", nil, false, fmt.Errorf("-config 缺少参数")
			}
			i++
			configPath = args[i]
		case strings.HasPrefix(a, "-config=") || strings.HasPrefix(a, "--config="):
			configPath = a[strings.IndexByte(a, '=')+1:]
		case a == "-version" || a == "--version":
			showVersion = true
		case a == "-h" || a == "--help" || a == "help":
			usage(os.Stdout)
			os.Exit(0)
		case strings.HasPrefix(a, "-"):
			return "", nil, false, fmt.Errorf("未知参数: %s", a)
		default:
			positional = append(positional, a)
		}
	}
	return configPath, positional, showVersion, nil
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `auto_performance v%s

用法:
  auto_performance [-config <file>]                 启动 HTTP 服务
  auto_performance [-config <file>] migrate up      执行数据库迁移
  auto_performance [-config <file>] migrate down    回滚一步迁移
  auto_performance [-config <file>] healthcheck     探测 /healthz（退出码 0=健康）
  auto_performance version                          打印版本
`, version)
}

// serve 装配并启动 HTTP 服务（优雅退出）。
func serve(cfg *config.Config) {
	// 1. 初始化数据库（pgxpool，lazy 建连：DB 不可达不阻塞启动，健康检查可自证状态）
	db, err := repository.Open(cfg.Database)
	if err != nil {
		log.Printf("警告: 数据库初始化失败（降级运行，鉴权相关接口不可用）: %v", err)
		db = nil
	}
	if db != nil {
		defer db.Close()
	}

	// handler/api 依赖用接口承载：nil 指针不得装入接口（否则健康检查会调用到 nil 池）。
	var dbPinger handler.Pinger
	if db != nil {
		dbPinger = db
	}

	// 2. 装配服务依赖（DB 不可用时认证/审计服务为 nil，中间件按依赖异常处理）
	var authService *authsvc.Service
	var auditService *logsvc.Service
	var settingService *settingsvc.Service
	var environmentService *envsvc.Service
	var productService *productsvc.Service
	var taskService *testingsvc.Service
	var reportService *reportsvc.Service
	var hub *ws.Hub
	var orchestratorService *orchestrator.Orchestrator

	// 接口认证头加密密钥（Phase 3 写入 / Phase 4 执行期解密注入脚本，INV-7）。
	headerKey := productsvc.DeriveHeaderKey("", os.Getenv(cfg.Security.SecretKeyEnv), cfg.JWT.Secret)

	if db != nil {
		repo := repository.NewRepository(db)
		authService = authsvc.New(repo, authsvc.DefaultPermCacheTTL)
		// 审计/日志服务：审计链 HMAC 追加（INV-8）+ 四类日志查询 + 请求详情/日志设置。
		auditService = logsvc.NewService(repo).WithQueryStore(repo).WithRequestStore(repo)
		// 管理员/角色/权限矩阵：角色变更经 authService 即时失效权限缓存（INV-1）。
		// 证书/SSH 私钥加密密钥（KEK）来自 security.kek_path / secret_key_env（DES-I-008）。
		kek, kekSource := settingsKEK(cfg)
		log.Printf("证书/SSH 私钥信封加密 KEK 来源: %s", kekSource)
		settingService = settingsvc.New(repo, authService).WithSecrets(repo, repo, kek)
		// 环境管理：分类/标签/环境 CRUD + 标签绑定（Phase 2）。
		environmentService = envsvc.New(repo)
		// 产品管理：产品/接口/阈值 CRUD（Phase 3）；接口认证头用独立派生密钥加密存储（DES-I-001）。
		productService = productsvc.New(repo, headerKey)

		// 实时通道（WS 握手鉴权 + 频道订阅），推送前统一脱敏（DES-I-001）。
		// 使用 Principal 而非 Permissions：同步校验账号存在/停用/首登改密状态。
		hub = ws.NewHub(cfg.JWT.Secret, func(ctx context.Context, adminID int64) (*ws.Principal, error) {
			p, err := authService.Principal(ctx, adminID)
			if err != nil {
				return nil, err
			}
			return &ws.Principal{
				AdminID:            p.AdminID,
				Permissions:        p.Permissions,
				MustChangePassword: p.MustChangePassword,
			}, nil
		})

		// 执行层：k6 子进程（无 shell + 参数数组 + 低权限）、清理器（SSH 白名单 + 脚本哈希）。
		k6Runner := k6.NewRunner(k6.Config{
			BinaryPath: cfg.K6.BinaryPath,
			WorkDir:    cfg.K6.WorkDir,
			UID:        cfg.Cleaner.LowPrivUID,
			GID:        cfg.Cleaner.LowPrivGID,
		})
		// 清理执行器：SSH HostResolver 由 Phase 7 系统设置提供（未接入前 Run 返回 10602）。
		clean := cleaner.New(cleaner.NewSSHHostRunner(nil), cleaner.NewAllowlist(
			parseIDList(cfg.Cleaner.AllowedHostIDs),
			parseHashList(cfg.Cleaner.AllowedScriptHashes),
		))
		// 报告生成（Phase 5）：HTML 模板 + headless Chromium PDF + gooxml Word；
		// 产物目录 security.report_dir，超时 report.gen_timeout_seconds，导入上限 report.max_size_bytes。
		reportService, err = reportsvc.New(repo, reportsvc.Options{
			Dir:           cfg.Security.ReportDir,
			MaxImportSize: cfg.Report.MaxSizeBytes,
			PDF: pdf.New(pdf.Config{
				Path:    cfg.Report.ChromiumPath,
				Timeout: time.Duration(cfg.Report.GenTimeoutSeconds) * time.Second,
			}),
		})
		if err != nil {
			log.Printf("警告: 报告服务初始化失败（报告生成/导入不可用）: %v", err)
			reportService = nil
		}

		var reportDeps orchestrator.ReportGenerator
		if reportService != nil {
			reportDeps = reportService
		}
		// 请求详情写入能力（受日志设置控制：开关/类型/采样限量在服务层判定）。
		var requestLogWriter orchestrator.RequestLogWriter
		if auditService != nil {
			writer := auditService
			requestLogWriter = orchestrator.RequestLogWriterFunc(
				func(ctx context.Context, taskID int64, entries []domain.RequestLog) error {
					_, err := writer.WriteRequestLogs(ctx, taskID, entries)
					return err
				})
		}
		orchestratorService = orchestrator.New(orchestrator.Deps{
			Store:         repo,
			Runner:        k6Runner,
			Cleaner:       clean,
			Hub:           hub,
			Report:        reportDeps,
			MaxVUs:        cfg.K6.MaxVUs,
			SecretDecoder: interfaceHeaderDecoder(headerKey),
			RequestLogs:   requestLogWriter,
		})
		// 测试管理：模板/任务 + 执行控制（Phase 4）；环境互斥/快照/状态机均在服务层强制。
		taskService = testingsvc.NewTaskService(repo, orchestratorService).WithMaxVUs(cfg.K6.MaxVUs)
	}

	if hub != nil {
		defer hub.Close()
	}
	if orchestratorService != nil {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			orchestratorService.Shutdown(ctx)
		}()
	}
	auditKey := logsvc.DeriveAuditKey(
		cfg.Security.AuditHMACKey,
		os.Getenv(cfg.Security.SecretKeyEnv),
		cfg.JWT.Secret,
	)
	log.Printf("审计链 HMAC 密钥指纹: %s（来源: %s）",
		logsvc.Fingerprint(auditKey), auditKeySource(cfg))

	// 日志保留策略：启动时按当前日志设置清理一次（过期 + 超量），失败仅告警。
	if auditService != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if res, err := auditService.PurgeLogs(ctx); err != nil {
			log.Printf("警告: 日志保留策略清理失败（不影响启动）: %v", err)
		} else if res.RequestLogs+res.TestLogs+res.SystemLogs > 0 {
			log.Printf("日志清理完成：请求详情 %d 条 / 测试日志 %d 条 / 系统日志 %d 条",
				res.RequestLogs, res.TestLogs, res.SystemLogs)
		}
		cancel()
	}

	// 系统设置：确保内置自签证书存在（不可删除，仅可重新初始化有效期）。
	if settingService != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := settingService.EnsureBuiltinCertificate(ctx); err != nil {
			log.Printf("警告: 内置自签证书初始化失败（证书管理不可用）: %v", err)
		}
		cancel()
	}

	// 3. 装配路由（Recovery → CORS → AuditLog → AuthN → AuthZ → Handler）
	// 系统设置服务以接口注入：避免 typed-nil 指针装入接口后误判为已就绪。
	var settingDeps settingapi.Service
	if settingService != nil {
		settingDeps = settingService
	}

	// 环境管理服务以接口注入：同样避免 typed-nil 指针装入接口。
	var environmentDeps envapi.Service
	if environmentService != nil {
		environmentDeps = environmentService
	}

	// 产品管理服务以接口注入：同样避免 typed-nil 指针装入接口。
	var productDeps productapi.Service
	if productService != nil {
		productDeps = productService
	}

	// 测试管理服务以接口注入：避免 typed-nil 指针装入接口。
	var taskDeps taskapi.Service
	if taskService != nil {
		taskDeps = taskService
	}

	// 报告管理服务以接口注入：同样避免 typed-nil 指针装入接口。
	var reportDepsAPI reportapi.Service
	if reportService != nil {
		reportDepsAPI = reportService
	}

	// 日志查询服务以接口注入：避免 typed-nil 指针装入接口。
	var logDepsAPI logapi.Service
	if auditService != nil {
		logDepsAPI = auditService
	}

	// 前端静态资源（go:embed 内嵌 dist）：单二进制直接提供页面，
	// 非 /api、/healthz 的未命中路径回退 index.html（SPA 深链刷新）。
	staticFS, staticErr := web.Dist()
	if staticErr != nil {
		log.Printf("警告: 内嵌前端产物不可用（仅提供 API）: %v", staticErr)
		staticFS = nil
	}

	router := api.NewRouter(api.Deps{
		Config:       cfg,
		DB:           dbPinger,
		Auth:         authService,
		Settings:     settingDeps,
		Environments: environmentDeps,
		Products:     productDeps,
		Tasks:        taskDeps,
		Reports:      reportDepsAPI,
		Logs:         logDepsAPI,
		Hub:          hub,
		Audit:        auditService,
		AuditKey:     auditKey,
		Static:       staticFS,
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 系统日志：记录启动/重启（上次进程未落 stop 则判定为异常重启 → warn）。
	if auditService != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := auditService.RecordStartup(ctx, version); err != nil {
			log.Printf("警告: 写入系统启动日志失败（不阻断启动）: %v", err)
		}
		cancel()
	}

	go func() {
		if cfg.Server.TLS.Enabled {
			log.Printf("auto_performance server v%s 启动，HTTPS 监听 %s（cert=%s）", version, srv.Addr, cfg.Server.TLS.CertFile)
			if err := srv.ListenAndServeTLS(cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("HTTPS server 异常退出: %v", err)
			}
			return
		}
		log.Printf("auto_performance server v%s 启动，HTTP 监听 %s", version, srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server 异常退出: %v", err)
		}
	}()

	// 4. 优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("收到退出信号，开始优雅关闭...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("强制关闭: %v", err)
	}
	// 系统日志：记录正常停止（供下次启动区分 start / restart）。
	// 使用独立 ctx：上面的退出 ctx 可能已被 srv.Shutdown 耗尽。
	if auditService != nil {
		logCtx, logCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := auditService.RecordShutdown(logCtx, version); err != nil {
			log.Printf("警告: 写入系统停止日志失败: %v", err)
		}
		logCancel()
	}
	log.Println("server 已退出")
}

// interfaceHeaderDecoder 构造接口认证头解密函数（执行期注入 k6 脚本，明文不落库/日志）。
func interfaceHeaderDecoder(key []byte) func([]byte) (map[string]string, error) {
	return func(enc []byte) (map[string]string, error) {
		if len(enc) == 0 {
			return map[string]string{}, nil
		}
		plain, err := crypto.DecryptGCM(enc, key)
		if err != nil {
			return nil, fmt.Errorf("解密接口认证头失败: %w", errcode.ErrDependency)
		}
		out := make(map[string]string)
		if err := json.Unmarshal(plain, &out); err != nil {
			return nil, fmt.Errorf("解析接口认证头失败: %w", errcode.ErrDependency)
		}
		return out, nil
	}
}

// parseIDList 解析逗号分隔的 ID 白名单（非法项忽略）。
func parseIDList(raw string) []int64 {
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil || v <= 0 {
			continue
		}
		out = append(out, v)
	}
	return out
}

// parseHashList 解析逗号分隔的 SHA256 白名单（小写）。
func parseHashList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.ToLower(strings.TrimSpace(p))
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

// settingsKEK 加载证书/SSH 私钥信封加密根密钥（不硬编码，来源可轮换）。
// 加载失败时返回 nil，业务层加密能力失败关闭（拒绝明文落库，返回 10602）。
func settingsKEK(cfg *config.Config) ([]byte, string) {
	kek, source, err := settingsvc.LoadKEK(cfg.Security.KEKPath,
		os.Getenv(cfg.Security.SecretKeyEnv), cfg.JWT.Secret)
	if err != nil {
		log.Printf("警告: KEK 加载失败（证书/SSH 导入不可用，拒绝明文落库）: %v", err)
		return nil, "unavailable"
	}
	return kek, source
}

// auditKeySource 描述审计密钥来源（仅日志展示，不泄露密钥）。
func auditKeySource(cfg *config.Config) string {
	switch {
	case cfg.Security.AuditHMACKey != "":
		return "security.audit_hmac_key"
	case os.Getenv(cfg.Security.SecretKeyEnv) != "":
		return "env:" + cfg.Security.SecretKeyEnv
	default:
		return "derived-from-jwt-secret"
	}
}

// runMigrate 执行数据库迁移子命令（up/down），供安装/升级脚本调用。
func runMigrate(cfg *config.Config, sub string) {
	var err error
	switch sub {
	case "up":
		err = repository.MigrateUp(cfg.Database)
	case "down":
		err = repository.MigrateDown(cfg.Database)
	case "":
		log.Printf("migrate 需要方向参数：up | down")
		usage(os.Stderr)
		os.Exit(2)
	default:
		log.Printf("未知 migrate 方向: %s（支持 up | down）", sub)
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("数据库迁移(%s)失败: %v（目标 %s:%d/%s）",
			sub, err, cfg.Database.Host, cfg.Database.Port, cfg.Database.Name)
	}
	log.Printf("数据库迁移(%s)完成", sub)
}

// runHealthcheck 探测本机 /healthz（部署脚本就绪检查；失败退出码 1）。
func runHealthcheck(cfg *config.Config) {
	host := cfg.Server.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	url := fmt.Sprintf("http://%s:%d/healthz", host, cfg.Server.Port)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Fatalf("健康检查失败（%s）: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("健康检查失败（%s）: HTTP %d body=%s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	fmt.Printf("healthy: %s -> HTTP %d %s\n", url, resp.StatusCode, strings.TrimSpace(string(body)))
}

// runTLSInit 从证书管理中的证书（certID 0 = 内置自签证书）解密并落盘为服务 TLS 证书/私钥。
// 供 install.sh（首装生成 HTTPS 证书）与"设为服务证书"流程使用；重启服务后生效。
func runTLSInit(cfg *config.Config) {
	db, err := repository.Open(cfg.Database)
	if err != nil {
		log.Fatalf("tls-init: 数据库初始化失败: %v", err)
	}
	defer db.Close()

	repo := repository.NewRepository(db)
	kek, kekSource := settingsKEK(cfg)
	log.Printf("tls-init: KEK 来源 %s", kekSource)
	svc := settingsvc.New(repo, nil).WithSecrets(repo, repo, kek)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 首次安装：确保内置自签证书已生成（幂等）。
	if err := svc.EnsureBuiltinCertificate(ctx); err != nil {
		log.Printf("tls-init: 确保内置自签证书失败（继续尝试激活）: %v", err)
	}

	certFile := cfg.Server.TLS.CertFile
	keyFile := cfg.Server.TLS.KeyFile
	c, k, err := svc.Activate(ctx, 0, certFile, keyFile)
	if err != nil {
		log.Fatalf("tls-init: 生成 TLS 证书失败: %v", err)
	}
	log.Printf("tls-init: 已写入 TLS 证书 %s、私钥 %s（重启服务后生效）", c, k)
}
