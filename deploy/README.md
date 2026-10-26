# deploy · 部署与离线打包

对齐 architecture.md §6/§7 的部署运维硬约束：**离线部署、可移植、依赖自包含、升级与卸载**。

## 目录结构

```text
deploy/
├── docker/
│   ├── Dockerfile.backend      # 后端多阶段构建
│   ├── Dockerfile.frontend     # 前端构建（nginx 服务形态）
│   └── docker-compose.yml      # 后端 + PostgreSQL 编排
├── offline/
│   ├── build-offline.sh        # 离线打包脚本
│   ├── manifest.txt            # 依赖/产物清单
│   └── assets/                 # 需随包分发的依赖（k6/chromium/postgres）
└── scripts/
    ├── install.sh              # 安装
    ├── upgrade.sh              # 升级（含数据迁移/回滚）
    └── uninstall.sh            # 卸载（--keep-data / --purge-data）
```

## 构建流程（离线打包）

```bash
# 1. 在打包机（需 Go + Node，可联网）执行
VERSION=0.1.0 ./deploy/offline/build-offline.sh

# 2. 产物
#    dist/auto_performance-offline-0.1.0.tar.gz
#    dist/auto_performance-offline-0.1.0.tar.gz.sha256
```

打包脚本会：
1. `npm ci && npm run build` 构建前端；
2. 将 `frontend/dist` 复制到 `backend/web/dist`，由 `go:embed` 内嵌；
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build` 静态编译后端单二进制；
4. 打包 `tar.gz` 并生成 SHA256 校验和。

## 部署流程（目标离线环境）

```bash
# 1. 校验完整性
sha256sum -c auto_performance-offline-0.1.0.tar.gz.sha256

# 2. 解压并安装
tar -xzf auto_performance-offline-0.1.0.tar.gz
cd auto_performance-offline-0.1.0
sudo ./scripts/install.sh

# 3. 启动与验证
sudo systemctl start auto_performance
curl http://127.0.0.1:8080/healthz
```

## 升级 / 卸载

```bash
# 升级（自动备份 + 迁移 + 健康检查 + 失败回滚）
sudo ./scripts/upgrade.sh

# 卸载（默认保留数据）
sudo ./scripts/uninstall.sh
# 物理清除数据（需二次确认）
sudo ./scripts/uninstall.sh --purge-data
```

## 说明

- **单二进制形态**：后端内嵌前端与报告模板，外部依赖仅 PostgreSQL + k6 + Chromium，随离线包分发。
- **Docker Compose 形态**：`docker compose -f deploy/docker/docker-compose.yml up --build`。
- 当前为**骨架**：`runtime/` 下的 k6/chromium/postgres 与数据库迁移、报告模板为占位，后续按实现计划填充。
