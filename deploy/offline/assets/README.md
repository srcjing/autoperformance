# offline/assets

离线包外部运行时依赖的**本地放置目录**（构建机使用，不随仓库提交二进制）。

`deploy/offline/build-offline.sh` 按以下顺序探测；均未命中时：`--strict` 直接失败，否则告警并在
`manifest.yaml` 中标记 `bundled: false`。

## 期望布局

```text
assets/
├── k6/
│   ├── k6-linux-amd64        # 或 k6-linux-arm64 / k6 / k6_linux_amd64
│   └── k6.version            #（可选）一行版本号，写入 manifest
├── chromium/
│   ├── chrome                # 或 chromium / headless_shell（可再套一层目录）
│   └── VERSION               #（可选）一行版本号
└── postgres/
    ├── bin/initdb            # PostgreSQL 官方二进制包的 bin 目录
    ├── bin/postgres
    ├── bin/psql
    ├── bin/pg_dump
    ├── bin/pg_restore
    ├── lib/                  # 动态库（如为动态链接构建）
    └── VERSION               #（可选）一行版本号
```

也可不放在本目录，直接用环境变量指向：

```bash
K6_BINARY=/path/to/k6 \
CHROMIUM_DIR=/path/to/chromium \
POSTGRES_DIR=/path/to/postgres \
VERSION=0.1.0 bash deploy/offline/build-offline.sh --strict
```

## 离线环境注意

- 目标机无外网：所有依赖必须事先放到本目录（或通过环境变量指定），`ALLOW_DOWNLOAD` 保持 0。
- PostgreSQL 若采用静态/自带库构建，需验证目标机 glibc 兼容性；本地集群模式由
  `install.sh --db-mode local` 初始化（`runtime/postgres/bin/initdb`）。
- Chromium 需与目标平台架构一致，并保证 `chrome`/`headless_shell` 具备执行权限。
- 二进制不得包含 `tcpdump/gdb/nmap/gcc` 等排查工具（security-design-report TEST-T-002 供应链核查）。
