#!/usr/bin/env bash
# 卸载脚本：停服 → 卸载服务 → 清理二进制与配置。
#
# 用法：./scripts/uninstall.sh [--keep-data | --purge-data]
#   默认 --keep-data：保留数据目录（数据库卷 + 报告产物 + 证书/密钥），便于重装或取证。
#   --purge-data：物理清除数据目录（需二次确认）。
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/auto_performance}"
DATA_DIR="${DATA_DIR:-/var/lib/auto_performance}"
CONFIG_DIR="${CONFIG_DIR:-/etc/auto_performance}"
SERVICE_NAME="auto_performance"

PURGE_DATA=false
if [[ "${1:-}" == "--purge-data" ]]; then
  PURGE_DATA=true
fi

echo "==> 停服并卸载 systemd 服务"
systemctl stop "${SERVICE_NAME}" || true
systemctl disable "${SERVICE_NAME}" || true
rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
systemctl daemon-reload

echo "==> 清理二进制与配置"
rm -f "${INSTALL_DIR}/auto_performance"
rm -f "${CONFIG_DIR}/config.yaml"

if [[ "${PURGE_DATA}" == true ]]; then
  read -r -p "确认物理删除数据目录 ${DATA_DIR}？(yes/no) " confirm
  if [[ "${confirm}" == "yes" ]]; then
    rm -rf "${DATA_DIR}"
    echo "==> 数据已清除"
  else
    echo "==> 已取消数据清除"
  fi
else
  echo "==> 保留数据目录 ${DATA_DIR}（--purge-data 可物理清除）"
fi

echo "==> 卸载完成"
