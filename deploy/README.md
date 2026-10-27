# deploy · 部署与离线打包

对齐 `architecture.md` §6/§7 与 `security-design-report.md` 的部署安全红线：

| 要求 | 落点 |
| --- | --- |
| 离线部署、依赖自包含、可移植 | `offline/build-offline.sh`（单二进制 + k6 + Chromium + PostgreSQL 运行时 + manifest） |
| 完整性校验（DES-T-001） | `manifest.yaml` 逐文件 SHA256，`install.sh` / `upgrade.sh` 部署前强制校验，含「未登记文件」检测 |
| 升级备份/迁移/健康检查/回滚（DES-T-003） | `scripts/upgrade.sh`：pg_dump + app.tar.gz → 停服 → 替换 → `migrate up` → 健康检查 → 失败自动回滚（`pg_restore` + 恢复旧二进制） |
| 服务低权限运行（DES-E-001） | systemd `User=autoperf` / `NoNewPrivileges=true` / `CapabilityBoundingSet=` / `ProtectSystem=strict`；容器镜像 `USER 10001` |
| KEK 0600、密钥不硬编码（DES-I-002） | `install.sh` 用 `/dev/urandom` 生成 KEK（0600）+ 随机 JWT/审计密钥；容器入口首次启动生成 KEK（0600） |
| 卸载数据保护（DES-I-002） | `uninstall.sh` 默认保留数据；`--purge-data` 需「PURGE + yes」双重确认 |

## 目录结构

```text
deploy/
├── docker/
│   ├── Dockerfile.backend     # 前端构建 → 内嵌 → CGO_ENABLED=0 静态编译 → 非 root 运行镜像
│   ├── Dockerfile.frontend    # 前端构建；target=nginx（独立服务）/ target=dist（产物导出）
│   ├── docker-compose.yml     # backend + frontend(nginx) + postgres + 命名卷
│   ├── nginx.conf             # 静态资源 + /api 反代（含 WS 升级与安全响应头）
│   ├── entrypoint.sh          # 生成 KEK(0600) → migrate up（带重试）→ 启动服务
│   └── .env.example           # compose 环境变量样例（含随机密钥占位）
├── offline/
│   ├── build-offline.sh       # 离线打包：前端 → 内嵌 → 静态编译 → 依赖 → manifest → tar.gz + sha256
│   ├── manifest.txt           # 包构成/依赖来源说明（人读）
│   └── assets/                # 外部依赖本地放置目录（k6 / chromium / postgres）
├── scripts/
│   ├── install.sh             # 安装（校验 → 账号/目录 → KEK → PostgreSQL → 迁移 → systemd）
│   ├── upgrade.sh             # 升级（备份 → 停服 → 替换 → 迁移 → 健康检查 → 失败回滚）
│   ├── uninstall.sh           # 卸载（默认保留数据 / --purge-data 双重确认）
│   └── lib/common.sh          # 公共库（日志、退出码、manifest 校验、健康检查、备份清理）
└── selftest/run-selftest.sh   # stub 环境下的脚本逻辑自测（完整性/升级回滚分支，不改动系统）
```

## 一、离线打包

```bash
# 开发/联调（允许缺 k6/chromium/postgres，manifest 标记 bundled=false）
VERSION=0.1.0 bash deploy/offline/build-offline.sh

# 发布构建（缺任一外部依赖即失败）
VERSION=0.1.0 bash deploy/offline/build-offline.sh --strict
```

产物：

```text
dist/auto_performance-offline-0.1.0.tar.gz
dist/auto_performance-offline-0.1.0.tar.gz.sha256
```

脚本步骤：`npm run build` → 复制到 `backend/web/dist`（`go:embed`）→
`CGO_ENABLED=0 GOOS=linux go build` → 组装包（`bin/`、`runtime/`、`config/`、`scripts/`、`data/`）→
生成 `manifest.yaml` + `manifest.sha256` → **打包前自校验** → `tar.gz` + 整包 SHA256。

外部依赖获取（优先级）：环境变量（`K6_BINARY`/`CHROMIUM_DIR`/`POSTGRES_DIR`）→ `assets/` 约定路径 →
（可选）`ALLOW_DOWNLOAD=1` + `*_DOWNLOAD_URL` 下载。详见 `offline/assets/README.md`。

包内结构：`bin/auto_performance`、`bin/k6`、`runtime/chromium/`、`runtime/postgres/`、
`config/config.example.yaml`、`scripts/{install,upgrade,uninstall}.sh`、`scripts/lib/common.sh`、
`manifest.yaml`、`manifest.sha256`、`VERSION`、`README.md`。

## 二、安装（目标离线环境）

```bash
# 1) 整包完整性
sha256sum -c auto_performance-offline-0.1.0.tar.gz.sha256

# 2) 解压
tar -xzf auto_performance-offline-0.1.0.tar.gz
cd auto_performance-offline-0.1.0

# 3) 预检（不修改系统；逐文件 SHA256 + 依赖 + 版本自证）
sudo ./scripts/install.sh --check-only

# 4) 安装
sudo ./scripts/install.sh

# 5) 验证
systemctl status auto_performance
curl -s http://127.0.0.1:8080/healthz     # {"status":"ok","db":"up"}
ps -o user= -p "$(systemctl show -p MainPID --value auto_performance)"   # -> autoperf（非 root）
ls -l /var/lib/auto_performance/keys/kek.key                             # -> 0600 autoperf
```

常用选项：

| 选项 | 说明 |
| --- | --- |
| `--check-only` | 仅校验包完整性/依赖/版本，不做变更（非 Linux 也可执行） |
| `--db-mode local\|external` | 本地 PostgreSQL 集群（`initdb`）或外部实例；默认按包内是否含 `runtime/postgres` 自动选择 |
| `--db-host/--db-port/--db-name/--db-password` | 外部实例连接参数（口令限制 `[A-Za-z0-9._~-]{12,128}`，避免配置转义问题） |
| `--no-pdf` | 不要求 Chromium（仅 HTML/Word 报告） |
| `--allow-missing-deps` | 允许缺 k6/chromium/postgres（测试用途，功能受限） |
| `--skip-systemd` / `--no-start` | 容器/自定义 init 场景：只准备文件（并执行迁移），不注册或不启动服务；`--skip-systemd` 仅支持 `--db-mode external` |
| `--force-config` | 覆盖已存在的 `config.yaml`（默认复用） |

安装要点：

- 完整性校验（DES-T-001）：`manifest.yaml` 记录的每个文件逐个比对 SHA256；任一缺失/不符/夹带未登记文件即退出码 `11`。
- 低权限账号：创建系统账号 `autoperf`（无登录 shell），服务以该账号运行；`k6`/Chromium 子进程使用 `nobody(65534)`，`/var/lib/auto_performance/k6` 为 `0770 autoperf:65534`。
- 密钥：KEK = 32 字节随机（`/dev/urandom` → hex），`0600` 属主 `autoperf`；`config.yaml` 内含随机 JWT/审计 HMAC 密钥与数据库口令，权限 `0640 root:autoperf`。
- 数据库：本地模式 `initdb`（`auth-local=peer`、`auth-host=scram-sha-256`）+ 独立 `auto_performance-postgres.service`；外部模式创建 `autoperf` 账号（默认 `NOSUPERUSER/NOCREATEDB/NOCREATEROLE`）与库，并收回 `public` schema 建表权限。
- 迁移：以 `autoperf` 身份执行 `auto_performance migrate up`（迁移脚本 `go:embed` 进二进制，无需额外迁移工具）。

安装后路径：

```text
/opt/auto_performance/bin/auto_performance           # 单二进制（内嵌前端/模板）
/opt/auto_performance/bin/k6                         # 压测引擎
/opt/auto_performance/runtime/{chromium,postgres}/   # 外部运行时
/etc/auto_performance/config.yaml                    # 0640 root:autoperf
/etc/auto_performance/auto_perf.env                  # 额外秘密（0640）
/var/lib/auto_performance/{reports,keys,k6,tmp,backups}
```

## 三、升级（含迁移与回滚）

```bash
# 预检/计划（不修改系统）
sudo ./scripts/upgrade.sh --check-only
sudo ./scripts/upgrade.sh --dry-run

# 升级
sudo ./scripts/upgrade.sh
```

执行顺序与失败处理：

```text
1) 预检：包完整性(SHA256) + 版本比较 + 数据库连通性
2) pg_dump -Fc 逻辑备份  → 失败则中止（未做任何变更）
3) 停服（等待进程退出）
4) 备份旧二进制/运行时/配置（app.tar.gz）+ meta.txt + SHA256SUMS
5) 替换二进制（原子 mv）、k6、Chromium；默认不替换 runtime/postgres
6) auto_performance migrate up（以 autoperf 身份）
7) 启动 + 健康检查（HTTP 200 且 "db":"up"）
   └ 6/7 任一失败 → 回滚：停服 → 恢复旧二进制/配置/版本标记 → pg_restore 数据库 → 重启旧版本
      · 回滚成功：退出码 61（需运维确认）
      · 回滚失败：打印 journalctl + 备份路径 + 手工恢复命令
```

手动回滚：`sudo ./scripts/upgrade.sh --rollback /var/lib/auto_performance/backups/<ts>`。

其他选项：`--force`（同版本重装）、`--allow-downgrade`、`--skip-backup`（危险）、
`--replace-pg-runtime`（数据库引擎升级需单独评估）、`--keep-backups N`（默认保留 5 份）。

测试钩子（仅用于验证回滚逻辑，生产禁用）：`SIMULATE_MIGRATE_FAILURE=1`、`SIMULATE_HEALTH_FAILURE=1`。

## 四、卸载

```bash
# 默认：卸载程序与配置，**保留**数据（数据库集群/数据、报告、KEK、备份）
sudo ./scripts/uninstall.sh

# 物理清除（双重确认：先输入 PURGE，再输入 yes）
sudo ./scripts/uninstall.sh --purge-data

# 非交互（自动化）：必须同时显式确认
CONFIRM_PURGE=YES sudo ./scripts/uninstall.sh --purge-data --yes

# 预演
sudo ./scripts/uninstall.sh --dry-run
```

保留数据时，配置会归档到 `/var/lib/auto_performance/uninstall-backup/config-<ts>.tar.gz`（0600），
便于重装复用（**KEK 不可删除**，否则历史密文不可解密）。

## 五、Docker Compose 形态

```bash
cp deploy/docker/.env.example deploy/docker/.env    # 修改 DB 口令 / JWT / 审计密钥
docker compose -f deploy/docker/docker-compose.yml --env-file deploy/docker/.env config -q   # 语法校验
docker compose -f deploy/docker/docker-compose.yml --env-file deploy/docker/.env up -d --build
# 访问 http://127.0.0.1:8088/
```

- `backend`：`Dockerfile.backend`（前端阶段构建 → `backend/web/dist` → `CGO_ENABLED=0` 静态编译 → `alpine` 运行镜像），
  非 root（UID 10001）、`read_only` 根文件系统、`cap_drop: ALL`、`no-new-privileges`；
  入口 `entrypoint.sh` 生成 KEK（0600，独立卷）后执行 `migrate up`（最多重试 30 次）再启动。
- `frontend`：`Dockerfile.frontend --target nginx`，静态资源 + `/api` 反代（含 WebSocket 升级）。
- `postgres`：`postgres:16-alpine`，不对外暴露端口，健康检查后后端才启动。
- 卷：`pgdata`（数据库）、`appdata`（报告/k6/临时）、`kek`（KEK 独立卷）。

离线交付：

```bash
docker compose -f deploy/docker/docker-compose.yml --env-file deploy/docker/.env build
docker save -o auto_performance-images-0.1.0.tar \
  auto_performance-backend:0.1.0 auto_performance-frontend:0.1.0 postgres:16-alpine
# 目标机：docker load -i ... && docker compose ... up -d
# k6/Chromium 通过 volumes 挂载离线包内 bin/k6、runtime/chromium（见 compose 注释）
```

## 六、退出码

| 退出码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 2 | 参数错误 |
| 3 | 运行环境不满足（非 root/非 Linux/缺命令/磁盘不足） |
| 11 | 包完整性校验失败（DES-T-001） |
| 12 | 运行时依赖缺失 |
| 20 | 数据库初始化/连接/迁移失败 |
| 30 | 配置生成/校验失败 |
| 40 / 41 | systemd 服务注册或启动失败 / 健康检查失败 |
| 61 | 升级失败并已执行回滚（需人工确认） |

## 七、本地验证（无需真实部署环境）

```bash
# 语法校验（Git Bash / Linux bash 均可）
bash -n deploy/scripts/*.sh deploy/scripts/lib/*.sh deploy/offline/*.sh

# 离线包构建 + 产物
VERSION=0.1.0 bash deploy/offline/build-offline.sh

# 完整性校验（含篡改检测）
sudo ./scripts/install.sh --check-only          # 包内：通过
echo x >> package/bin/auto_performance && ./scripts/install.sh --check-only   # → 退出码 11

# 升级回滚逻辑自测（stub systemctl/psql/pg_dump/curl，不改动系统）
bash deploy/selftest/run-selftest.sh
```

## 八、已知限制

- **真机验证未覆盖**：本仓库开发机无 Docker daemon、无 k6、无 Chromium、无 systemd，因此
  `docker build`、容器启动、真实 k6 压测、Chromium PDF 渲染、systemd 单元启动需在目标环境验证；
  本地已完成的验证见上节（语法、打包、SHA256、篡改检测、升级回滚分支）。
- **前端静态资源服务**：后端已挂载内嵌前端静态资源（`web.FS`，`go:embed all:dist`，由 `NoRoute`
  SPA 处理器提供：命中内嵌文件即返回、`/api` 前缀保持统一 JSON 404、其余路径回退 `index.html`
  支撑深链刷新），因此单二进制形态无需额外静态服务器即可直接访问页面；容器形态仍可选由
  `frontend`(nginx) 提供 UI 与 `/api` 反代。
- **数据库引擎升级**：`upgrade.sh` 默认不替换 `runtime/postgres`，PostgreSQL 大版本升级需单独评估与演练。
- **外部依赖版本**：k6 / Chromium / PostgreSQL 版本由打包机放入 `assets/` 决定，`manifest.yaml`
  会记录（若提供 `VERSION` 文件），发布前需按 `security-design-report.md` TEST-T-002 做供应链核查（无双引擎杀毒/黑名单工具）。
