#!/usr/bin/env bash
# upgrade.sh —— auto_performance 升级脚本（备份 → 停服 → 替换 → 迁移 → 健康检查 → 失败回滚）。
#
# 安全语义（DES-T-003）：
#   - 升级前**必须**完成包完整性校验（DES-T-001）与数据库逻辑备份；
#   - 任一阶段失败自动回滚：恢复旧二进制/配置 + 用备份恢复数据库 + 重启旧版本；
#   - 回滚成功仍以非 0 退出（61），由运维确认；回滚失败打印人工恢复指引；
#   - 升级不覆盖 config.yaml（保留运维本地配置），仅替换二进制/运行时。
#
# 用法：
#   sudo ./scripts/upgrade.sh                 # 在解压后的新版本离线包根目录执行
#   sudo ./scripts/upgrade.sh --dry-run       # 只做预检与计划输出，不改动任何东西
#   sudo ./scripts/upgrade.sh --check-only    # 仅校验包完整性/版本，非 Linux 也可执行
#   sudo ./scripts/upgrade.sh --rollback /var/lib/auto_performance/backups/20261026120000
#
# 选项：
#   --force              允许同版本重装
#   --allow-downgrade    允许降级（默认拒绝）
#   --skip-backup        跳过备份（危险，仅限已人工备份的场景）
#   --replace-pg-runtime 同时替换 runtime/postgres（数据库引擎升级需单独评估）
#   --keep-backups N     保留最近 N 份备份（默认 5）
#
# 测试钩子（仅用于验证回滚逻辑，生产禁用）：
#   SIMULATE_MIGRATE_FAILURE=1   在迁移阶段注入失败 → 走回滚
#   SIMULATE_HEALTH_FAILURE=1    在健康检查阶段注入失败 → 走回滚
set -euo pipefail
umask 027

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PKG_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

SERVICE_NAME="auto_performance"
PG_SERVICE_NAME="auto_performance-postgres"
SERVICE_USER="autoperf"

INSTALL_DIR="${INSTALL_DIR:-/opt/auto_performance}"
DATA_DIR="${DATA_DIR:-/var/lib/auto_performance}"
CONFIG_DIR="${CONFIG_DIR:-/etc/auto_performance}"
CONFIG_FILE="${CONFIG_DIR}/config.yaml"
BACKUP_ROOT="${BACKUP_DIR:-${DATA_DIR}/backups}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-60}"
KEEP_BACKUPS="${KEEP_BACKUPS:-5}"

DRY_RUN=0
CHECK_ONLY=0
FORCE=0
ALLOW_DOWNGRADE=0
SKIP_BACKUP=0
REPLACE_PG_RUNTIME=0
ROLLBACK_FROM=""

BACKUP_DIR_PATH=""
NEW_VERSION=""
OLD_VERSION=""
DB_HOST="127.0.0.1"
DB_PORT="5432"
DB_USER="autoperf"
DB_NAME=""
DB_PASSWORD=""
SERVER_PORT="8080"
HEALTH_URL=""

usage() {
  sed -n '3,30p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

parse_args() {
  while (($# > 0)); do
    case "$1" in
      --dry-run) DRY_RUN=1 ;;
      --check-only) CHECK_ONLY=1 ;;
      --force) FORCE=1 ;;
      --allow-downgrade) ALLOW_DOWNGRADE=1 ;;
      --skip-backup) SKIP_BACKUP=1 ;;
      --replace-pg-runtime) REPLACE_PG_RUNTIME=1 ;;
      --rollback)
        shift
        ROLLBACK_FROM="${1:-}"
        ;;
      --rollback=*) ROLLBACK_FROM="${1#*=}" ;;
      --keep-backups)
        shift
        KEEP_BACKUPS="${1:-5}"
        ;;
      --keep-backups=*) KEEP_BACKUPS="${1#*=}" ;;
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
  [[ "$KEEP_BACKUPS" =~ ^[0-9]+$ ]] || die "$EXIT_USAGE" "--keep-backups 必须为数字"
}

# ---------------------------------------------------------------------------
# 配置 / 版本 / 数据库工具
# ---------------------------------------------------------------------------

load_config() {
  [[ -f "$CONFIG_FILE" ]] || die "$EXIT_CONFIG" "未找到已安装配置: ${CONFIG_FILE}（请先执行 install.sh）"
  DB_HOST="${AUTO_PERF_DATABASE_HOST:-$(yaml_scalar "$CONFIG_FILE" database host)}"
  DB_PORT="${AUTO_PERF_DATABASE_PORT:-$(yaml_scalar "$CONFIG_FILE" database port)}"
  DB_USER="${AUTO_PERF_DATABASE_USER:-$(yaml_scalar "$CONFIG_FILE" database user)}"
  DB_NAME="${AUTO_PERF_DATABASE_NAME:-$(yaml_scalar "$CONFIG_FILE" database name)}"
  DB_PASSWORD="${AUTO_PERF_DATABASE_PASSWORD:-$(yaml_scalar "$CONFIG_FILE" database password)}"
  DB_HOST="${DB_HOST:-127.0.0.1}"
  DB_PORT="${DB_PORT:-5432}"
  DB_USER="${DB_USER:-autoperf}"
  DB_PASSWORD="${DB_PASSWORD:-}"
  [[ -n "$DB_NAME" ]] || die "$EXIT_CONFIG" "配置缺少 database.name"
  SERVER_PORT="$(yaml_scalar "$CONFIG_FILE" server port)"
  SERVER_PORT="${SERVER_PORT:-8080}"
  HEALTH_URL="http://127.0.0.1:${SERVER_PORT}/healthz"
}

is_local_pg() { [[ -s "${DATA_DIR}/postgres/PG_VERSION" ]]; }

# pg_client_path <tool>：本地集群优先用随包的 pg 客户端（版本匹配），否则用系统命令
pg_client_path() {
  local tool="$1"
  if [[ -x "${INSTALL_DIR}/runtime/postgres/bin/${tool}" ]]; then
    printf '%s' "${INSTALL_DIR}/runtime/postgres/bin/${tool}"
  else
    printf '%s' "$tool"
  fi
}

# app_pg <tool> <args...>：以应用账号（autoperf）通过 TCP 连接执行
#   用途：pg_dump / pg_restore / 连通性探测（均为非破坏性、应用权限内操作）
app_pg() {
  local tool="$1"
  shift
  local bin
  bin="$(pg_client_path "$tool")"
  PGPASSWORD="$DB_PASSWORD" "$bin" -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" "$@"
}

# admin_psql <args...>：管理连接（DROP/CREATE DATABASE 需要更高权限）
#   本地集群 → autoperf 即集群超级用户；外部实例 → PG_ADMIN_DSN > postgres 系统用户 > 应用账号
admin_psql() {
  if is_local_pg; then
    app_pg psql -v ON_ERROR_STOP=1 "$@"
  elif [[ -n "${PG_ADMIN_DSN:-}" ]]; then
    psql "$PG_ADMIN_DSN" -v ON_ERROR_STOP=1 "$@"
  elif [[ "$(id -u)" == "0" ]] && id -u postgres >/dev/null 2>&1; then
    runuser -u postgres -- psql -v ON_ERROR_STOP=1 "$@"
  else
    log_warn "无外部实例管理凭据（PG_ADMIN_DSN/postgres peer），尝试以 ${DB_USER} 执行"
    app_pg psql -v ON_ERROR_STOP=1 "$@"
  fi
}

installed_version() {
  if [[ -s "${INSTALL_DIR}/VERSION" ]]; then
    tr -d ' \r\n' <"${INSTALL_DIR}/VERSION"
  elif [[ -x "${INSTALL_DIR}/bin/auto_performance" ]]; then
    "${INSTALL_DIR}/bin/auto_performance" version 2>/dev/null | tr -d ' \r\n'
  fi
}

# ---------------------------------------------------------------------------
# 预检
# ---------------------------------------------------------------------------

preflight() {
  log_step "1/7 预检"

  if [[ "$CHECK_ONLY" == "1" ]]; then
    require_cmd sha256sum tar awk sed grep find sort
  else
    require_linux
    require_root
    require_cmd sha256sum tar install awk sed grep find sort df date mktemp du
    require_any_cmd curl wget
    require_cmd systemctl
    [[ -x "${INSTALL_DIR}/bin/auto_performance" ]] \
      || die "$EXIT_ENV" "未检测到已安装服务: ${INSTALL_DIR}/bin/auto_performance（请先执行 install.sh）"
    load_config
    require_disk_kb "$BACKUP_ROOT" 524288 # 512MB
  fi

  verify_package_integrity "$PKG_ROOT" || die "$EXIT_INTEGRITY" "离线包完整性校验未通过，拒绝升级（DES-T-001）"

  NEW_VERSION="$(manifest_field "${PKG_ROOT}/manifest.yaml" version)"
  [[ -n "$NEW_VERSION" ]] || die "$EXIT_INTEGRITY" "manifest.yaml 缺少 version 字段"

  if [[ "$CHECK_ONLY" == "1" ]]; then
    log_info "check-only 通过：离线包版本 ${NEW_VERSION}，完整性校验全部一致"
    exit "$EXIT_OK"
  fi

  OLD_VERSION="$(installed_version)"
  [[ -n "$OLD_VERSION" ]] || die "$EXIT_ENV" "无法识别已安装版本（缺少 ${INSTALL_DIR}/VERSION）"
  log_info "已安装版本: ${OLD_VERSION} → 待升级版本: ${NEW_VERSION}"

  if [[ "$OLD_VERSION" == "$NEW_VERSION" && "$FORCE" != "1" ]]; then
    die "$EXIT_USAGE" "已安装同版本（${NEW_VERSION}）；如需重装请加 --force"
  fi
  if version_gt "$OLD_VERSION" "$NEW_VERSION" && [[ "$ALLOW_DOWNGRADE" != "1" ]]; then
    die "$EXIT_USAGE" "检测到降级（${OLD_VERSION} → ${NEW_VERSION}）；如确需降级请加 --allow-downgrade"
  fi

  if [[ "$SKIP_BACKUP" != "1" ]]; then
    if ! app_pg psql -d "$DB_NAME" -tAc 'SELECT 1' >/dev/null 2>&1; then
      die "$EXIT_DB" "数据库不可连接（${DB_HOST}:${DB_PORT}/${DB_NAME}），禁止在无备份前提下升级"
    fi
    log_info "数据库连通性检查通过: ${DB_HOST}:${DB_PORT}/${DB_NAME}"
  else
    log_warn "--skip-backup：跳过备份与数据库连通性检查（高风险操作）"
  fi
}

# ---------------------------------------------------------------------------
# 备份
# ---------------------------------------------------------------------------

create_backup_dir() {
  local ts
  ts="$(date +%Y%m%d%H%M%S)"
  BACKUP_DIR_PATH="${BACKUP_ROOT}/${ts}"
  install -d -m 0750 -o root -g "$SERVICE_USER" "$BACKUP_ROOT"
  install -d -m 0700 -o root -g "$SERVICE_USER" "$BACKUP_DIR_PATH"
  log_info "本次备份目录: ${BACKUP_DIR_PATH}"
}

backup_database() {
  log_step "2/7 备份数据库（pg_dump custom format）"
  if [[ "$SKIP_BACKUP" == "1" ]]; then
    log_warn "已跳过数据库备份"
    return 0
  fi
  local rc=0
  app_pg pg_dump -Fc --no-owner --no-privileges -d "$DB_NAME" -f "${BACKUP_DIR_PATH}/db.dump" || rc=$?
  if (( rc != 0 )) || [[ ! -s "${BACKUP_DIR_PATH}/db.dump" ]]; then
    die "$EXIT_DB" "数据库备份失败（pg_dump 退出码 ${rc}），升级中止（未做任何变更）"
  fi
  log_info "数据库备份完成: ${BACKUP_DIR_PATH}/db.dump（$(du -h "${BACKUP_DIR_PATH}/db.dump" | awk '{print $1}')）"
}

backup_application() {
  log_step "4/7 备份旧二进制与配置"
  local -a paths=()
  if [[ -d "$INSTALL_DIR" ]]; then paths+=("${INSTALL_DIR#/}"); fi
  if [[ -d "$CONFIG_DIR" ]]; then paths+=("${CONFIG_DIR#/}"); fi
  if [[ -d "${DATA_DIR}/reports" ]]; then paths+=("${DATA_DIR#/}/reports"); fi
  if [[ -d "${DATA_DIR}/keys" ]]; then paths+=("${DATA_DIR#/}/keys"); fi
  if (( ${#paths[@]} == 0 )); then
    log_error "没有可备份的安装目录"
    return 1
  fi

  if ! tar -czf "${BACKUP_DIR_PATH}/app.tar.gz" -C / "${paths[@]}"; then
    log_error "应用文件备份失败"
    return 1
  fi

  {
    echo "backup_version=1"
    echo "from_version=${OLD_VERSION}"
    echo "to_version=${NEW_VERSION}"
    echo "created_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    echo "install_dir=${INSTALL_DIR}"
    echo "config_dir=${CONFIG_DIR}"
    echo "data_dir=${DATA_DIR}"
    echo "db_mode=$(if is_local_pg; then echo local; else echo external; fi)"
    echo "db_name=${DB_NAME}"
    echo "db_host=${DB_HOST}"
    echo "db_port=${DB_PORT}"
    echo "manifest_sha256=$(sha256sum "${PKG_ROOT}/manifest.yaml" | awk '{print $1}')"
  } >"${BACKUP_DIR_PATH}/meta.txt"

  {
    sha256sum "${BACKUP_DIR_PATH}/app.tar.gz" | sed "s#${BACKUP_DIR_PATH}/##"
    if [[ -s "${BACKUP_DIR_PATH}/db.dump" ]]; then
      sha256sum "${BACKUP_DIR_PATH}/db.dump" | sed "s#${BACKUP_DIR_PATH}/##"
    fi
  } >"${BACKUP_DIR_PATH}/SHA256SUMS"

  log_info "应用与配置已备份: ${BACKUP_DIR_PATH}/app.tar.gz"
}

# ---------------------------------------------------------------------------
# 替换 / 迁移 / 启动
# ---------------------------------------------------------------------------

replace_binaries() {
  log_step "5/7 替换二进制与运行时"
  local dst="${INSTALL_DIR}/bin/auto_performance"

  install -m 0755 -o root -g root "${PKG_ROOT}/bin/auto_performance" "${dst}.new" || return 1
  mv -f "${dst}.new" "$dst" || return 1
  log_info "已替换后端二进制: ${dst}"

  if [[ -f "${PKG_ROOT}/bin/k6" ]]; then
    install -m 0755 -o root -g root "${PKG_ROOT}/bin/k6" "${INSTALL_DIR}/bin/k6.new" || return 1
    mv -f "${INSTALL_DIR}/bin/k6.new" "${INSTALL_DIR}/bin/k6" || return 1
    log_info "已替换 k6: ${INSTALL_DIR}/bin/k6"
  fi

  if [[ -d "${PKG_ROOT}/runtime/chromium" ]]; then
    rm -rf "${INSTALL_DIR}/runtime/chromium.new"
    cp -a "${PKG_ROOT}/runtime/chromium" "${INSTALL_DIR}/runtime/chromium.new" || return 1
    rm -rf "${INSTALL_DIR}/runtime/chromium"
    mv "${INSTALL_DIR}/runtime/chromium.new" "${INSTALL_DIR}/runtime/chromium" || return 1
    chmod -R a+rX "${INSTALL_DIR}/runtime/chromium"
    log_info "已替换 Chromium 运行时"
  fi

  if [[ -d "${PKG_ROOT}/runtime/postgres" ]]; then
    if [[ "$REPLACE_PG_RUNTIME" == "1" ]]; then
      rm -rf "${INSTALL_DIR}/runtime/postgres.new"
      cp -a "${PKG_ROOT}/runtime/postgres" "${INSTALL_DIR}/runtime/postgres.new" || return 1
      rm -rf "${INSTALL_DIR}/runtime/postgres"
      mv "${INSTALL_DIR}/runtime/postgres.new" "${INSTALL_DIR}/runtime/postgres" || return 1
      log_info "已替换 PostgreSQL 运行时（--replace-pg-runtime）"
    else
      log_warn "包内含 runtime/postgres，默认不替换（数据库引擎升级需单独评估）；如需替换加 --replace-pg-runtime"
    fi
  fi

  install -m 0644 "${PKG_ROOT}/manifest.yaml" "${INSTALL_DIR}/manifest.yaml" || return 1
  log_info "已更新安装清单: ${INSTALL_DIR}/manifest.yaml"
}

run_migration() {
  log_step "6/7 执行数据库迁移（migrate up）"
  if [[ "${SIMULATE_MIGRATE_FAILURE:-0}" == "1" ]]; then
    log_warn "SIMULATE_MIGRATE_FAILURE=1：注入迁移失败（仅用于验证回滚逻辑，生产禁用）"
    return 1
  fi
  if ! run_as "$SERVICE_USER" env HOME="$DATA_DIR" \
    "${INSTALL_DIR}/bin/auto_performance" -config "$CONFIG_FILE" migrate up; then
    log_error "数据库迁移失败（migrate up）"
    return 1
  fi
  log_info "数据库迁移完成"
  return 0
}

start_and_verify() {
  log_step "7/7 启动新版本并健康检查"
  if ! systemctl restart "$SERVICE_NAME"; then
    log_error "服务启动失败"
    return 1
  fi
  if [[ "${SIMULATE_HEALTH_FAILURE:-0}" == "1" ]]; then
    log_warn "SIMULATE_HEALTH_FAILURE=1：注入健康检查失败（仅用于验证回滚逻辑）"
    return 1
  fi
  if ! wait_healthy "$HEALTH_URL" "$HEALTH_TIMEOUT" 1; then
    return 1
  fi
  return 0
}

# ---------------------------------------------------------------------------
# 回滚
# ---------------------------------------------------------------------------

restore_database() {
  local dump="${1}/db.dump"
  if [[ ! -s "$dump" ]]; then
    log_warn "备份中无数据库转储，跳过数据库恢复"
    return 0
  fi
  log_info "恢复数据库（drop/create/restore）: ${DB_NAME}"

  admin_psql -d postgres -c "DROP DATABASE IF EXISTS ${DB_NAME} WITH (FORCE)" || {
    log_warn "DROP DATABASE ... WITH (FORCE) 失败（PG<13），回退为普通 DROP"
    admin_psql -d postgres -c "DROP DATABASE IF EXISTS ${DB_NAME}" || return 1
  }
  admin_psql -d postgres -c "CREATE DATABASE ${DB_NAME} OWNER ${DB_USER} ENCODING 'UTF8'" || return 1

  local rc=0
  app_pg pg_restore --no-owner --no-privileges -d "$DB_NAME" "$dump" || rc=$?
  if (( rc != 0 )); then
    log_error "pg_restore 返回 ${rc}（可能包含无害告警），请人工核对数据完整性"
    return 1
  fi
  log_info "数据库已从备份恢复"
  return 0
}

restore_application() {
  local backup="$1" tmp
  if [[ ! -s "${backup}/app.tar.gz" ]]; then
    log_error "备份缺少 app.tar.gz: ${backup}"
    return 1
  fi
  tmp="$(mktemp -d)"
  if ! tar -xzf "${backup}/app.tar.gz" -C "$tmp"; then
    log_error "解包备份失败: ${backup}/app.tar.gz"
    rm -rf "$tmp"
    return 1
  fi

  local src_bin="${tmp}${INSTALL_DIR}/bin/auto_performance"
  if [[ -f "$src_bin" ]]; then
    if install -m 0755 -o root -g root "$src_bin" "${INSTALL_DIR}/bin/auto_performance.restore"; then
      mv -f "${INSTALL_DIR}/bin/auto_performance.restore" "${INSTALL_DIR}/bin/auto_performance"
      log_info "已恢复旧二进制"
    else
      log_error "恢复旧二进制失败"
    fi
  else
    log_warn "备份中未找到旧二进制，跳过二进制恢复"
  fi

  if [[ -d "${tmp}${CONFIG_DIR}" ]]; then
    mkdir -p "$CONFIG_DIR"
    cp -a "${tmp}${CONFIG_DIR}/." "$CONFIG_DIR/"
    chmod 0640 "$CONFIG_FILE" 2>/dev/null || true
    log_info "已恢复配置目录: ${CONFIG_DIR}"
  fi

  if [[ -s "${tmp}${INSTALL_DIR}/VERSION" ]]; then
    install -m 0644 "${tmp}${INSTALL_DIR}/VERSION" "${INSTALL_DIR}/VERSION"
    log_info "已恢复版本标记: $(tr -d ' \r\n' <"${INSTALL_DIR}/VERSION")"
  fi
  if [[ -s "${tmp}${INSTALL_DIR}/manifest.yaml" ]]; then
    install -m 0644 "${tmp}${INSTALL_DIR}/manifest.yaml" "${INSTALL_DIR}/manifest.yaml"
  fi

  rm -rf "$tmp"
  return 0
}

do_rollback() {
  local reason="$1" backup="${BACKUP_DIR_PATH:-${ROLLBACK_FROM:-}}" failed=0
  set +e
  log_step "开始回滚：${reason}"
  if [[ -z "$backup" || ! -d "$backup" ]]; then
    log_error "无可用备份目录，无法自动回滚，请人工恢复。"
    set -e
    return 1
  fi
  log_info "回滚备份: ${backup}"

  if ! (cd "$backup" && sha256sum -c SHA256SUMS >/dev/null 2>&1); then
    log_warn "备份 SHA256SUMS 校验未通过（备份可能不完整），继续尝试恢复"
  fi

  systemctl stop "$SERVICE_NAME" >/dev/null 2>&1
  svc_stop "$SERVICE_NAME"

  restore_application "$backup" || failed=1
  restore_database "$backup" || failed=1

  systemctl restart "$SERVICE_NAME" >/dev/null 2>&1
  if (( failed == 0 )) && wait_healthy "$HEALTH_URL" "$HEALTH_TIMEOUT" 1; then
    log_warn "回滚完成：已恢复到版本 $(installed_version)（备份保留在 ${backup}）"
    set -e
    return 0
  fi

  log_error "回滚后服务仍异常，请立即人工处理："
  log_error "  1) journalctl -u ${SERVICE_NAME} -n 100 --no-pager"
  log_error "  2) 备份目录: ${backup}（含 db.dump / app.tar.gz / meta.txt）"
  log_error "  3) 手工恢复: tar -xzf ${backup}/app.tar.gz -C / && systemctl restart ${SERVICE_NAME}"
  set -e
  return 1
}

# ---------------------------------------------------------------------------
# 主流程
# ---------------------------------------------------------------------------

print_plan() {
  local mode
  if is_local_pg; then mode="本地集群"; else mode="外部实例"; fi
  cat <<EOF

================ 升级计划（dry-run，未做任何变更）================
当前版本:     ${OLD_VERSION}
目标版本:     ${NEW_VERSION}
安装目录:     ${INSTALL_DIR}
配置:         ${CONFIG_FILE}（升级不覆盖）
数据目录:     ${DATA_DIR}
数据库:       ${DB_HOST}:${DB_PORT}/${DB_NAME}（${mode}）
备份目录:     ${BACKUP_ROOT}/<timestamp>
健康检查:     ${HEALTH_URL}（要求 HTTP 200 且 db=up）
回滚策略:     备份（pg_dump + app.tar.gz）→ 停服 → 替换 → migrate up → 健康检查
              任一阶段失败：恢复旧二进制/配置 + pg_restore + 重启旧版本，退出码 ${EXIT_ROLLBACK}

执行顺序:
  1) 预检（完整性/版本/连通性）  2) pg_dump 备份  3) 停服
  4) 备份二进制与配置            5) 替换二进制    6) migrate up
  7) 启动 + 健康检查             失败 → 自动回滚
=================================================================
EOF
}

main() {
  parse_args "$@"

  if [[ -n "$ROLLBACK_FROM" ]]; then
    require_linux
    require_root
    require_cmd sha256sum tar install awk sed grep find sort systemctl
    require_any_cmd curl wget
    [[ -d "$ROLLBACK_FROM" ]] || die "$EXIT_USAGE" "--rollback 目录不存在: ${ROLLBACK_FROM}"
    ROLLBACK_FROM="$(cd "$ROLLBACK_FROM" && pwd)"
    load_config
    if do_rollback "--rollback 手工回滚"; then
      log_info "手工回滚完成"
      exit "$EXIT_OK"
    fi
    die "$EXIT_ROLLBACK" "手工回滚失败，请按上方指引人工处理"
  fi

  preflight

  if [[ "$DRY_RUN" == "1" ]]; then
    print_plan
    exit "$EXIT_OK"
  fi

  create_backup_dir
  backup_database

  log_step "3/7 停止服务（等待进程退出）"
  if ! systemctl stop "$SERVICE_NAME"; then
    log_warn "systemctl stop 返回非 0（服务可能未运行）"
  fi
  svc_stop "$SERVICE_NAME"

  if [[ "$SKIP_BACKUP" != "1" ]]; then
    if ! backup_application; then
      systemctl start "$SERVICE_NAME" >/dev/null 2>&1 || true
      die "$EXIT_ENV" "应用备份失败，升级中止（服务已尝试恢复启动；备份目录: ${BACKUP_DIR_PATH}）"
    fi
  else
    log_warn "--skip-backup：未备份应用文件，回滚能力受限"
  fi

  if ! replace_binaries; then
    log_error "替换二进制失败"
    systemctl start "$SERVICE_NAME" >/dev/null 2>&1 || true
    die "$EXIT_ENV" "升级中止（未迁移数据），服务已尝试恢复启动；备份: ${BACKUP_DIR_PATH}"
  fi

  if ! run_migration; then
    do_rollback "数据库迁移失败" || true
    die "$EXIT_ROLLBACK" "升级失败，已执行回滚（备份: ${BACKUP_DIR_PATH}）"
  fi

  if ! start_and_verify; then
    do_rollback "新版本健康检查失败" || true
    die "$EXIT_ROLLBACK" "升级失败，已执行回滚（备份: ${BACKUP_DIR_PATH}）"
  fi

  printf '%s\n' "$NEW_VERSION" | write_file_atomic "${INSTALL_DIR}/VERSION" 0644
  prune_backups "$BACKUP_ROOT" "$KEEP_BACKUPS"

  cat <<EOF

================ 升级完成 ================
版本:     ${OLD_VERSION} → ${NEW_VERSION}
备份:     ${BACKUP_DIR_PATH}（保留最近 ${KEEP_BACKUPS} 份）
健康检查: ${HEALTH_URL} 通过
服务:     systemctl status ${SERVICE_NAME}
回滚命令: sudo ./scripts/upgrade.sh --rollback ${BACKUP_DIR_PATH}
=========================================
EOF
  exit "$EXIT_OK"
}

main "$@"
