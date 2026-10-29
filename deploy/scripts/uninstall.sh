#!/usr/bin/env bash
# uninstall.sh —— auto_performance 卸载脚本。
#
# 默认行为（安全默认，DES-I-002）：
#   - 停服 → 卸载 systemd 单元 → 删除二进制/运行时 → 归档并删除配置目录；
#   - **保留**数据目录（数据库集群/数据 + 报告产物 + KEK + 备份），便于重装或取证。
#
# --purge-data：物理清除数据（含数据库 drop、数据目录 rm、KEK 删除），需二次确认：
#   交互式需依次输入 PURGE 与 yes；非交互式必须显式 CONFIRM_PURGE=YES。
#
# 用法：
#   sudo ./scripts/uninstall.sh                     # 卸载程序，保留数据
#   sudo ./scripts/uninstall.sh --keep-data         # 显式声明保留数据（与默认一致）
#   sudo ./scripts/uninstall.sh --purge-data        # 卸载并物理清除数据（双重确认）
#   sudo ./scripts/uninstall.sh --dry-run           # 仅打印将执行的动作
#   sudo ./scripts/uninstall.sh --purge-data --remove-user   # 同时删除服务账号
#   CONFIRM_PURGE=YES sudo ./scripts/uninstall.sh --purge-data --yes   # 非交互（自动化）
set -euo pipefail
umask 027

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

SERVICE_NAME="auto_performance"
PG_SERVICE_NAME="auto_performance-postgres"
SERVICE_USER="autoperf"
SERVICE_GROUP="autoperf"

INSTALL_DIR="${INSTALL_DIR:-/opt/auto_performance}"
DATA_DIR="${DATA_DIR:-/var/lib/auto_performance}"
CONFIG_DIR="${CONFIG_DIR:-/etc/auto_performance}"
CONFIG_FILE="${CONFIG_DIR}/config.yaml"

PURGE_DATA=0
DRY_RUN=0
REMOVE_USER=0
ASSUME_YES=0

usage() {
  sed -n '3,22p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

parse_args() {
  while (($# > 0)); do
    case "$1" in
      --purge-data) PURGE_DATA=1 ;;
      --keep-data) PURGE_DATA=0 ;;
      --dry-run) DRY_RUN=1 ;;
      --remove-user) REMOVE_USER=1 ;;
      --yes) ASSUME_YES=1 ;;
      -h | --help)
        usage
        exit "$EXIT_OK"
        ;;
      *)
        log_error "未知参数: $1"
        usage
        exit "$EXIT_USAGE"
        ;;
    esac
    shift
  done
}

load_config() {
  DB_HOST="127.0.0.1"
  DB_PORT="5432"
  DB_USER="$SERVICE_USER"
  DB_NAME="auto_performance"
  DB_PASSWORD=""
  if [[ -f "$CONFIG_FILE" ]]; then
    DB_HOST="$(yaml_scalar "$CONFIG_FILE" database host)"
    DB_PORT="$(yaml_scalar "$CONFIG_FILE" database port)"
    DB_USER="$(yaml_scalar "$CONFIG_FILE" database user)"
    DB_NAME="$(yaml_scalar "$CONFIG_FILE" database name)"
    DB_PASSWORD="$(yaml_scalar "$CONFIG_FILE" database password)"
    DB_HOST="${DB_HOST:-127.0.0.1}"
    DB_PORT="${DB_PORT:-5432}"
    DB_USER="${DB_USER:-autoperf}"
    DB_NAME="${DB_NAME:-auto_performance}"
  fi
}

is_local_pg() { [[ -s "${DATA_DIR}/postgres/PG_VERSION" ]]; }

pg_client_path() {
  local tool="$1"
  if [[ -x "${INSTALL_DIR}/runtime/postgres/bin/${tool}" ]]; then
    printf '%s' "${INSTALL_DIR}/runtime/postgres/bin/${tool}"
  else
    printf '%s' "$tool"
  fi
}

app_pg() {
  local tool="$1"
  shift
  local bin
  bin="$(pg_client_path "$tool")"
  PGPASSWORD="$DB_PASSWORD" "$bin" -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" "$@"
}

admin_psql() {
  if is_local_pg; then
    app_pg psql -v ON_ERROR_STOP=1 "$@"
  elif [[ -n "${PG_ADMIN_DSN:-}" ]]; then
    psql "$PG_ADMIN_DSN" -v ON_ERROR_STOP=1 "$@"
  elif [[ "$(id -u)" == "0" ]] && id -u postgres >/dev/null 2>&1; then
    runuser -u postgres -- psql -v ON_ERROR_STOP=1 "$@"
  else
    app_pg psql -v ON_ERROR_STOP=1 "$@"
  fi
}

# 物理清除数据的二次确认
confirm_purge() {
  local a b
  if [[ "$ASSUME_YES" == "1" ]]; then
    if [[ "${CONFIRM_PURGE:-}" == "YES" ]]; then
      log_warn "已通过 CONFIRM_PURGE=YES --yes 确认物理删除（非交互）"
      return 0
    fi
    die "$EXIT_USAGE" "--yes 必须与 CONFIRM_PURGE=YES 同时使用（防止误删）"
  fi
  if [[ ! -t 0 ]]; then
    die "$EXIT_USAGE" "非交互环境删除数据必须显式提供 CONFIRM_PURGE=YES 与 --yes"
  fi

  log_warn "即将物理删除以下内容（不可恢复）："
  log_warn "  - 数据库 ${DB_NAME}（${DB_HOST}:${DB_PORT}）及账号 ${DB_USER}"
  log_warn "  - 数据目录 ${DATA_DIR}（报告产物、KEK、备份、本地 PostgreSQL 集群）"
  log_warn "  - 配置目录 ${CONFIG_DIR}"
  if ! confirm_text "输入 PURGE 继续: " "PURGE"; then
    log_error "确认失败，已取消"
    return 1
  fi
  if ! confirm_text "再次确认：输入 yes 执行物理删除: " "yes"; then
    log_error "二次确认失败，已取消"
    return 1
  fi
  return 0
}

print_plan() {
  cat <<EOF

================ 卸载计划（dry-run，未做任何变更）================
服务:         ${SERVICE_NAME}（+ ${PG_SERVICE_NAME}，若存在）
安装目录:     ${INSTALL_DIR}      → 删除
配置目录:     ${CONFIG_DIR}       → $(if [[ "$PURGE_DATA" == "1" ]]; then echo "删除"; else echo "归档到 ${DATA_DIR}/uninstall-backup/ 后删除"; fi)
数据目录:     ${DATA_DIR}         → $(if [[ "$PURGE_DATA" == "1" ]]; then echo "物理删除（含数据库 drop）"; else echo "保留"; fi)
服务账号:     ${SERVICE_USER}     → $(if [[ "$REMOVE_USER" == "1" ]]; then echo "删除"; else echo "保留"; fi)
=================================================================
EOF
}

stop_services() {
  log_step "1/5 停止并注销 systemd 服务"
  if svc_exists "$SERVICE_NAME"; then
    systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true
    svc_stop "$SERVICE_NAME"
    systemctl disable "$SERVICE_NAME" >/dev/null 2>&1 || true
    log_info "已停止服务: ${SERVICE_NAME}"
  else
    log_info "服务单元不存在，跳过: ${SERVICE_NAME}"
  fi

  if svc_exists "$PG_SERVICE_NAME"; then
    systemctl stop "$PG_SERVICE_NAME" >/dev/null 2>&1 || true
    systemctl disable "$PG_SERVICE_NAME" >/dev/null 2>&1 || true
    log_info "已停止本地 PostgreSQL 服务"
  fi

  rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
  rm -f "/etc/systemd/system/${PG_SERVICE_NAME}.service"
  systemd_reload
  log_info "已移除 systemd 单元"
}

archive_config() {
  local dir="${DATA_DIR}/uninstall-backup" ts
  ts="$(date +%Y%m%d%H%M%S)"
  if [[ ! -d "$CONFIG_DIR" ]]; then
    return 0
  fi
  install -d -m 0700 -o root -g root "$dir"
  tar -czf "${dir}/config-${ts}.tar.gz" -C / "${CONFIG_DIR#/}" || {
    log_warn "配置归档失败，保留配置目录避免配置丢失"
    return 0
  }
  chmod 0600 "${dir}/config-${ts}.tar.gz"
  log_info "配置已归档: ${dir}/config-${ts}.tar.gz（含数据库口令等敏感信息，注意保管）"
}

purge_database() {
  log_step "2/5 删除数据库与账号"
  # 本地集群：数据目录随 --purge-data 一并删除（此时 PG 已停止，无需也无法 DROP）
  if is_local_pg; then
    log_info "本地 PostgreSQL 集群随数据目录一并删除，跳过 DROP DATABASE/ROLE"
    return 0
  fi
  if ! command -v psql >/dev/null 2>&1 && [[ ! -x "${INSTALL_DIR}/runtime/postgres/bin/psql" ]]; then
    log_warn "未找到 psql，跳过数据库删除（本地集群将随数据目录一并删除）"
    return 0
  fi

  if [[ "$(id -u)" != "0" ]] && ! is_local_pg; then
    log_warn "非 root 环境无法执行数据库删除，跳过"
    return 0
  fi

  # 终止连接后删除数据库（DROP ... WITH (FORCE) 需 PG13+）
  if ! admin_psql -d postgres -c "DROP DATABASE IF EXISTS ${DB_NAME} WITH (FORCE)" >/dev/null 2>&1; then
    admin_psql -d postgres -c "DROP DATABASE IF EXISTS ${DB_NAME}" >/dev/null 2>&1 || {
      log_warn "删除数据库 ${DB_NAME} 失败（请人工确认）"
      return 0
    }
  fi
  log_info "已删除数据库: ${DB_NAME}"

  if ! is_local_pg; then
    admin_psql -d postgres -c "DROP ROLE IF EXISTS ${DB_USER}" >/dev/null 2>&1 || {
      log_warn "删除账号 ${DB_USER} 失败（可能仍被其它对象依赖），请人工确认"
    }
    log_info "已尝试删除数据库账号: ${DB_USER}"
  fi
  return 0
}

remove_program() {
  log_step "3/5 删除程序与配置"
  if [[ -d "$INSTALL_DIR" ]]; then
    rm -rf "$INSTALL_DIR"
    log_info "已删除安装目录: ${INSTALL_DIR}"
  fi
  if [[ -d "$CONFIG_DIR" ]]; then
    rm -rf "$CONFIG_DIR"
    log_info "已删除配置目录: ${CONFIG_DIR}"
  fi
}

remove_data() {
  log_step "4/5 物理清除数据"
  if [[ -d "$DATA_DIR" ]]; then
    # KEK 单独先删，避免残留密钥与新数据混用
    rm -f "${DATA_DIR}/keys/kek.key" 2>/dev/null || true
    rm -rf "$DATA_DIR"
    log_info "已删除数据目录: ${DATA_DIR}"
  else
    log_info "数据目录不存在，跳过: ${DATA_DIR}"
  fi
}

remove_user() {
  log_step "5/5 删除服务账号"
  if [[ "$REMOVE_USER" != "1" ]]; then
    log_info "保留服务账号 ${SERVICE_USER}（--remove-user 可删除）"
    return 0
  fi
  if id -u "$SERVICE_USER" >/dev/null 2>&1; then
    userdel "$SERVICE_USER" 2>/dev/null || log_warn "userdel 失败（用户可能仍有进程），请人工确认"
    log_info "已删除服务账号: ${SERVICE_USER}"
  else
    log_info "服务账号不存在，跳过"
  fi
  if getent group "$SERVICE_GROUP" >/dev/null 2>&1; then
    groupdel "$SERVICE_GROUP" 2>/dev/null || true
  fi
}

main() {
  parse_args "$@"
  load_config

  if [[ "$DRY_RUN" == "1" ]]; then
    print_plan
    exit "$EXIT_OK"
  fi

  require_linux
  require_root
  require_cmd tar awk sed grep find sort systemctl date

  if [[ "$PURGE_DATA" == "1" ]]; then
    confirm_purge || die "$EXIT_USAGE" "已取消卸载（未做任何变更）"
  fi

  stop_services

  if [[ "$PURGE_DATA" == "1" ]]; then
    purge_database
    remove_program
    remove_data
  else
    archive_config
    remove_program
    log_warn "数据目录已保留: ${DATA_DIR}"
    log_warn "如需彻底清除，请执行: sudo ./scripts/uninstall.sh --purge-data（双重确认）"
  fi

  remove_user

  if [[ "$PURGE_DATA" == "1" ]]; then
    cat <<EOF

================ 卸载完成（数据已清除）================
程序、配置、数据、数据库均已删除。
=====================================================
EOF
  else
    cat <<EOF

================ 卸载完成（数据保留）================
保留内容:
  - 数据目录: ${DATA_DIR}
    · PostgreSQL 集群: ${DATA_DIR}/postgres（如为本地模式）
    · 报告产物:        ${DATA_DIR}/reports
    · KEK:             ${DATA_DIR}/keys/kek.key（0600）
    · 备份:            ${DATA_DIR}/backups
    · 卸载时配置归档:  ${DATA_DIR}/uninstall-backup/
重装提示: 重新解压离线包执行 install.sh 时会复用数据与 KEK（勿删除 KEK，否则历史密文不可解密）。
=====================================================
EOF
  fi
  exit "$EXIT_OK"
}

main "$@"
