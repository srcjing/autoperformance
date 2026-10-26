// Package main 是 auto_performance 后端入口。
//
// 职责：加载配置 → 初始化数据库 → 装配路由（中间件链 + 内嵌前端）→ 启动 HTTP server（优雅退出）。
// 骨架阶段：仅提供健康检查 + 登录等最小可运行接口，业务逻辑后续按实现计划填充。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"auto_performance/internal/api"
	"auto_performance/internal/config"
	"auto_performance/internal/repository"
)

// version 通过 -ldflags "-X main.version=<v>" 注入。
var version = "dev"

func main() {
	var configPath = flag.String("config", "", "配置文件路径（可选，缺省使用内置默认值）")
	flag.Parse()

	// 1. 加载配置（YAML / 环境变量）
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 2. 初始化数据库（占位：PostgreSQL 连接池，lazy 建连不阻塞启动）
	db, err := repository.Open(cfg.Database)
	if err != nil {
		log.Printf("警告: 数据库初始化失败（骨架降级运行）: %v", err)
	}
	if db != nil {
		defer db.Close()
	}

	// 3. 装配路由（中间件链：Recovery → CORS → AuditLog → AuthN → AuthZ → Handler）
	router := api.NewRouter(cfg, db)

	// 4. 启动 HTTP server（优雅退出）
	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("auto_performance server v%s 启动，监听 %s", version, srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server 异常退出: %v", err)
		}
	}()

	// 5. 优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("收到退出信号，开始优雅关闭...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("强制关闭: %v", err)
	}
	log.Println("server 已退出")
}
