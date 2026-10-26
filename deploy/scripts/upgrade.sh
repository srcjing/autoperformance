#!/usr/bin/env bash
# 升级脚本：备份 → 停服 → 替换二进制 → 数据迁移 → 健康检查 → 失败回滚。
#
# 用法：在解压后的新版本离线包根目录执行 ./scripts/upgrade.sh
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/auto_performance}"
DATA_DIR="${DATA_DIR:-/var/lib/auto_performance}"
BACKUP_DIR="${BACKUP_DIR:-/var/lib/auto_performance/backups}"
SERVICE_NAME="auto_performance"
TIMESTAMP="$(date +%Y%m%d%H%M%S)"

echo "==> 1. 备份数据卷 + 配置 + 旧二进制"
mkdir -p "${BACKUP_DIR}"
tar -czf "${BACKUP_DIR}/backup-${TIMESTAMP}.tar.gz" \
  -C "$(dirname "${DATA_DIR}")" "$(basename "${DATA_DIR}")" \
  -C "$(dirname "${INSTALL_DIR}")" "$(basename "${INSTALL_DIR}")/auto_performance" 2>/dev/null || true

echo "==> 2. 停服"
systemctl stop "${SERVICE_NAME}" || true

echo "==> 3. 替换二进制"
cp -f bin/auto_performance "${INSTALL_DIR}/auto_performance"
chmod +x "${INSTALL_DIR}/auto_performance"

echo "==> 4. 执行数据库迁移（golang-migrate up，占位）"
# "${INSTALL_DIR}/migrate" -path "${INSTALL_DIR}/migrations" -database "${DB_DSN}" up

echo "==> 5. 启动并健康检查"
systemctl start "${SERVICE_NAME}"
if curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
  echo "==> 升级完成"
else
  echo "==> 健康检查失败，执行回滚..."
  systemctl stop "${SERVICE_NAME}" || true
  tar -xzf "${BACKUP_DIR}/backup-${TIMESTAMP}.tar.gz" -C / 2>/dev/null || true
  systemctl start "${SERVICE_NAME}" || true
  echo "==> 已回滚到备份 ${BACKUP_DIR}/backup-${TIMESTAMP}.tar.gz"
fi
