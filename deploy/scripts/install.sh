#!/usr/bin/env bash
# 安装脚本：放置二进制 → 初始化配置/数据目录 → 注册 systemd 服务。
#
# 用法：在解压后的离线包根目录执行 ./scripts/install.sh
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/auto_performance}"
DATA_DIR="${DATA_DIR:-/var/lib/auto_performance}"
CONFIG_DIR="${CONFIG_DIR:-/etc/auto_performance}"
SERVICE_NAME="auto_performance"

echo "==> 安装 auto_performance 到 ${INSTALL_DIR}"
mkdir -p "${INSTALL_DIR}" "${DATA_DIR}" "${CONFIG_DIR}"

# 二进制
cp -f bin/auto_performance "${INSTALL_DIR}/auto_performance"
chmod +x "${INSTALL_DIR}/auto_performance"

# 配置
if [[ -f config/config.yaml ]]; then
  cp -f config/config.yaml "${CONFIG_DIR}/config.yaml"
fi

# 初始化 PostgreSQL（占位：实际调用 runtime/postgres 离线初始化）
echo "==> 初始化 PostgreSQL（占位，后续接入 runtime/postgres）"

# 注册 systemd 服务
echo "==> 注册 systemd 服务 ${SERVICE_NAME}"
cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=auto_performance performance testing platform
After=network.target postgresql.service

[Service]
ExecStart=${INSTALL_DIR}/auto_performance -config ${CONFIG_DIR}/config.yaml
Restart=on-failure
Environment=AUTO_PERF_DATABASE_NAME=auto_performance

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload

echo "==> 安装完成。启动：systemctl start ${SERVICE_NAME}"
echo "==> 健康检查：curl http://127.0.0.1:8080/healthz"
