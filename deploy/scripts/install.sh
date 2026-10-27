#!/usr/bin/env bash
# install.sh —— auto_performance 单二进制形态安装脚本（离线包内执行）。
#
# 流程：
#   0. 参数解析
#   1. 预检：平台/root/命令/磁盘 + **manifest.yaml 逐文件 SHA256 校验（DES-T-001）** + 运行时依赖
#   2. 解析或复用安装配置
#   3. 创建低权限系统账号 autoperf（DES-E-001）
#   4. 创建目录并收敛权限
#   5. 原子安装二进制 / k6 / chromium / postgres
#   6. 生成配置（随机 JWT/审计密钥，权限 0640 root:autoperf）
#   7. 生成 KEK（32 字节随机，0600，属主 autoperf）（DES-I-002）
#   8. 初始化 PostgreSQL（本地集群 initdb 或外部实例建库建号）
#   9. 执行数据库迁移（migrate up）
#  10. 注册 systemd 单元（User=autoperf / NoNewPrivileges=true / 最小 capability）
#  11. 启动服务 + 健康检查（HTTP 200 且 db=up）
#
# 用法：
#   sudo ./scripts/install.sh [选项]
#
# 选项：
#   --check-only            仅做完整性/依赖/配置预检，不安装（可用于校验包，非 Linux 亦可）
#   --no-start              安装但不启动服务
#   --skip-systemd          不注册 systemd（容器/自定义 init 场景）
#   --force-config          覆盖已存在的 config.yaml
#   --db-mode local|external  PostgreSQL 形态（默认：包内带 postgres 则 local，否则 external）
#   --db-host H --db-port P --db-name N --db-password P
#   --allow-missing-deps    允许缺失 k6/chromium/postgres（仅限测试/后续补装）
#   --no-pdf                不要求 chromium（仅 html/word 报告）
#   -h|--help
#
# 退出码见 scripts/lib/common.sh（0 成功 / 10..61 分类失败）。
set -euo pipefail
umask 027

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PKG_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
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
ENV_FILE="${CONFIG_DIR}/auto_perf.env"

DB_MODE=""
DB_HOST="127.0.0.1"
DB_PORT="5432"
DB_NAME="auto_performance"
DB_PASSWORD=""

CHECK_ONLY=0
DO_START=1
SKIP_SYSTEMD=0
FORCE_CONFIG=0
ALLOW_MISSING_DEPS=0
REQUIRE_PDF=1

usage() {
  sed -n '3,40p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

parse_args() {
  while (($# > 0)); do
    case "$1" in
      --check-only) CHECK_ONLY=1 ;;
      --no-start) DO_START=0 ;;
      --skip-systemd) SKIP_SYSTEMD=1 ;;
      --force-config) FORCE_CONFIG=1 ;;
      --allow-missing-deps) ALLOW_MISSING_DEPS=1 ;;
      --no-pdf) REQUIRE_PDF=0 ;;
      --db-mode)
        shift
        DB_MODE="${1:-}"
        ;;
      --db-mode=*) DB_MODE="${1#*=}" ;;
      --db-host)
        shift
        DB_HOST="${1:-}"
        ;;
      --db-host=*) DB_HOST="${1#*=}" ;;
      --db-port)
        shift
        DB_PORT="${1:-}"
        ;;
      --db-port=*) DB_PORT="${1#*=}" ;;
      --db-name)
        shift
        DB_NAME="${1:-}"
        ;;
      --db-name=*) DB_NAME="${1#*=}" ;;
      --db-password)
        shift
        DB_PASSWORD="${1:-}"
        ;;
      --db-password=*) DB_PASSWORD="${1#*=}" ;;
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

  case "$DB_MODE" in
    "" | local | external) : ;;
    *)
      die "$EXIT_USAGE" "--db-mode 仅支持 local | external"
      ;;
  esac
  # 数据库名/端口做严格校验：后续会进入 SQL/URL，禁止任意字符（防注入）
  [[ "$DB_NAME" =~ ^[a-z_][a-z0-9_]{0,62}$ ]] || die "$EXIT_USAGE" "非法数据库名: $DB_NAME"
  [[ "$DB_PORT" =~ ^[0-9]{1,5}$ ]] || die "$EXIT_USAGE" "非法数据库端口: $DB_PORT"
  if [[ -n "$DB_PASSWORD" && ! "$DB_PASSWORD" =~ ^[A-Za-z0-9._~-]{12,128}$ ]]; then
    die "$EXIT_USAGE" "--db-password 仅允许字母/数字/._~-，长度 12~128（避免配置转义问题）"
  fi
  [[ -n "$DB_HOST" ]] || die "$EXIT_USAGE" "--db-host 不能为空"
}

# ---------------------------------------------------------------------------
# 1. 预检
# ---------------------------------------------------------------------------

detect_db_mode() {
  if [[ -n "$DB_MODE" ]]; then
    return 0
  fi
  if [[ -x "${PKG_ROOT}/runtime/postgres/bin/initdb" ]]; then
    DB_MODE="local"
  else
    DB_MODE="external"
  fi
  log_info "未指定 --db-mode，自动选择: ${DB_MODE}"
}

check_runtime_deps() {
  local manifest="${PKG_ROOT}/manifest.yaml"
  local missing=()

  if [[ ! -x "${PKG_ROOT}/bin/k6" ]]; then
    missing+=("bin/k6（压测引擎）")
    if [[ "$(manifest_dep_bundled "$manifest" k6)" == "true" ]]; then
      log_error "manifest 声明 k6 已随包，但 bin/k6 缺失 → 包不完整"
    fi
  fi
  if [[ "$REQUIRE_PDF" == "1" && ! -d "${PKG_ROOT}/runtime/chromium" ]]; then
    missing+=("runtime/chromium（PDF 渲染）")
  fi
  if [[ "$DB_MODE" == "local" && ! -x "${PKG_ROOT}/runtime/postgres/bin/initdb" ]]; then
    missing+=("runtime/postgres/bin/initdb（本地 PostgreSQL）")
  fi

  if (( ${#missing[@]} == 0 )); then
    log_info "运行时依赖检查通过（k6、chromium、PostgreSQL 模式=${DB_MODE}）"
    return 0
  fi

  local m
  for m in "${missing[@]}"; do
    log_error "缺少运行时依赖: ${m}"
  done
  if [[ "$ALLOW_MISSING_DEPS" == "1" ]]; then
    log_warn "--allow-missing-deps：继续安装，但相关功能（压测/PDF 报告/本地库）不可用"
    return 0
  fi
  die "$EXIT_DEPS" "运行时依赖缺失，安装中止（如为测试用途可加 --allow-missing-deps）"
}

preflight() {
  log_step "1/9 预检"

  if [[ "$CHECK_ONLY" == "1" ]]; then
    require_cmd sha256sum tar awk sed grep find sort
  else
    require_linux
    require_root
    require_cmd sha256sum tar install awk sed grep find sort df date mktemp getent groupadd useradd
    require_any_cmd curl wget
    if [[ "$SKIP_SYSTEMD" != "1" ]]; then
      require_cmd systemctl
      if [[ ! -d /run/systemd/system ]]; then
        die "$EXIT_ENV" "当前 init 不是 systemd（/run/systemd/system 不存在）；容器环境请使用 --skip-systemd"
      fi
    fi
  fi

  detect_db_mode
  if [[ "$DB_MODE" == "external" && "$CHECK_ONLY" != "1" ]]; then
    require_cmd psql
  fi

  # 包完整性（DES-T-001）：任何一个文件哈希不符/缺失/夹带即中止
  verify_package_integrity "$PKG_ROOT" || die "$EXIT_INTEGRITY" "离线包完整性校验未通过，拒绝安装"

  PKG_VERSION="$(manifest_field "${PKG_ROOT}/manifest.yaml" version)"
  [[ -n "$PKG_VERSION" ]] || die "$EXIT_INTEGRITY" "manifest.yaml 缺少 version 字段"
  log_info "离线包版本: ${PKG_VERSION}"

  # 二进制版本自证（仅在与本机架构匹配的 Linux 上执行）
  if [[ "$CHECK_ONLY" != "1" && "$(uname -m)" =~ ^(x86_64|aarch64|arm64)$ ]]; then
    local bin_ver
    if bin_ver="$("${PKG_ROOT}/bin/auto_performance" version 2>/dev/null)"; then
      if [[ "$bin_ver" != "$PKG_VERSION" ]]; then
        die "$EXIT_INTEGRITY" "二进制版本(${bin_ver}) 与 manifest(${PKG_VERSION}) 不一致"
      fi
      log_info "二进制版本自证通过: ${bin_ver}"
    else
      log_warn "无法执行包内二进制（架构不匹配或缺少执行权限），跳过版本自证"
    fi
  fi

  check_runtime_deps

  if [[ "$CHECK_ONLY" != "1" ]]; then
    require_disk_kb "$DATA_DIR" 524288 # 512MB
    if [[ -e "$CONFIG_FILE" && "$FORCE_CONFIG" != "1" ]]; then
      log_warn "已存在配置 ${CONFIG_FILE}，将复用（--force-config 可覆盖）"
    fi
  fi

  if [[ "$CHECK_ONLY" == "1" ]]; then
    log_info "check-only 预检通过：完整性/依赖/版本均满足（未做任何变更）"
    log_info "包根目录: ${PKG_ROOT}"
    exit "$EXIT_OK"
  fi
}

# ---------------------------------------------------------------------------
# 2. 账号与目录
# ---------------------------------------------------------------------------

ensure_user() {
  log_step "2/9 创建低权限系统账号 ${SERVICE_USER}"
  if getent group "$SERVICE_GROUP" >/dev/null 2>&1; then
    log_info "用户组已存在: ${SERVICE_GROUP}"
  else
    groupadd --system "$SERVICE_GROUP"
    log_info "已创建用户组: ${SERVICE_GROUP}"
  fi
  if id -u "$SERVICE_USER" >/dev/null 2>&1; then
    log_info "用户已存在: ${SERVICE_USER}"
  else
    useradd --system --gid "$SERVICE_GROUP" --home-dir "$DATA_DIR" \
      --shell /usr/sbin/nologin --comment "auto_performance service" "$SERVICE_USER"
    log_info "已创建系统账号: ${SERVICE_USER}（无登录 shell，非 root）"
  fi
  if id -u "$SERVICE_USER" | grep -qx '0'; then
    die "$EXIT_ENV" "服务账号 ${SERVICE_USER} 的 UID 为 0，拒绝继续（违反低权限要求）"
  fi
}

ensure_dirs() {
  log_step "3/9 创建目录并收敛权限"
  install -d -m 0755 -o root -g root "$INSTALL_DIR" "${INSTALL_DIR}/bin" "${INSTALL_DIR}/runtime"
  install -d -m 0750 -o "$SERVICE_USER" -g "$SERVICE_GROUP" "$DATA_DIR"
  install -d -m 0750 -o "$SERVICE_USER" -g "$SERVICE_GROUP" \
    "${DATA_DIR}/tmp" "${DATA_DIR}/reports" "${DATA_DIR}/keys"
  # k6 / Chromium 子进程以 nobody(65534) 运行，工作目录需组可写
  install -d -m 0770 -o "$SERVICE_USER" -g 65534 "${DATA_DIR}/k6"
  install -d -m 0750 -o root -g "$SERVICE_GROUP" "$CONFIG_DIR"

  # 已存在的 KEK 权限收敛（历史安装可能放宽）
  if [[ -f "${DATA_DIR}/keys/kek.key" ]]; then
    chown "$SERVICE_USER:$SERVICE_GROUP" "${DATA_DIR}/keys/kek.key" 2>/dev/null || true
    chmod 0600 "${DATA_DIR}/keys/kek.key" || die "$EXIT_CONFIG" "KEK 权限收敛失败"
    log_info "已收敛既有 KEK 权限为 0600"
  fi
  log_info "安装目录: ${INSTALL_DIR}；数据目录: ${DATA_DIR}；配置目录: ${CONFIG_DIR}"
}

# ---------------------------------------------------------------------------
# 3. 二进制与运行时依赖
# ---------------------------------------------------------------------------

install_binaries() {
  log_step "4/9 安装二进制与运行时依赖"
  local dst

  dst="${INSTALL_DIR}/bin/auto_performance"
  install -m 0755 -o root -g root "${PKG_ROOT}/bin/auto_performance" "${dst}.new"
  mv -f "${dst}.new" "$dst"
  log_info "已安装后端二进制: ${dst}"

  if [[ -f "${PKG_ROOT}/bin/k6" ]]; then
    install -m 0755 -o root -g root "${PKG_ROOT}/bin/k6" "${INSTALL_DIR}/bin/k6.new"
    mv -f "${INSTALL_DIR}/bin/k6.new" "${INSTALL_DIR}/bin/k6"
    log_info "已安装 k6: ${INSTALL_DIR}/bin/k6"
  else
    log_warn "包内无 bin/k6，压测功能不可用（后续可手工放置到 ${INSTALL_DIR}/bin/k6）"
  fi

  if [[ -d "${PKG_ROOT}/runtime/chromium" ]]; then
    rm -rf "${INSTALL_DIR}/runtime/chromium"
    cp -a "${PKG_ROOT}/runtime/chromium" "${INSTALL_DIR}/runtime/chromium"
    chmod -R a+rX "${INSTALL_DIR}/runtime/chromium"
    log_info "已安装 Chromium: ${INSTALL_DIR}/runtime/chromium"
  elif [[ "$REQUIRE_PDF" == "1" ]]; then
    log_warn "包内无 runtime/chromium，PDF 报告不可用（--no-pdf 可显式关闭该要求）"
  fi

  if [[ -d "${PKG_ROOT}/runtime/postgres" ]]; then
    rm -rf "${INSTALL_DIR}/runtime/postgres"
    cp -a "${PKG_ROOT}/runtime/postgres" "${INSTALL_DIR}/runtime/postgres"
    chmod -R a+rX "${INSTALL_DIR}/runtime/postgres"
    log_info "已安装 PostgreSQL 运行时: ${INSTALL_DIR}/runtime/postgres"
  fi

  # 记录版本与清单，供 upgrade/uninstall/审计使用
  printf '%s\n' "$PKG_VERSION" | write_file_atomic "${INSTALL_DIR}/VERSION" 0644
  install -m 0644 "${PKG_ROOT}/manifest.yaml" "${INSTALL_DIR}/manifest.yaml"
}

# ---------------------------------------------------------------------------
# 4. 配置与 KEK
# ---------------------------------------------------------------------------

chromium_binary_path() {
  local dir="${INSTALL_DIR}/runtime/chromium" cand
  for cand in chrome chromium headless_shell chrome-headless-shell; do
    if [[ -f "${dir}/${cand}" ]]; then
      printf '%s' "${dir}/${cand}"
      return 0
    fi
  done
  # 允许 chromium/ 下再套一层目录
  cand="$(find "$dir" -maxdepth 3 -type f \( -name chrome -o -name chromium -o -name headless_shell \) 2>/dev/null | head -n1 || true)"
  printf '%s' "${cand}"
}

render_config() {
  log_step "5/9 生成配置"

  local jwt_secret audit_key chromium_path
  if [[ -f "$CONFIG_FILE" && "$FORCE_CONFIG" != "1" ]]; then
    DB_HOST="$(yaml_scalar "$CONFIG_FILE" database host)"
    DB_PORT="$(yaml_scalar "$CONFIG_FILE" database port)"
    DB_NAME="$(yaml_scalar "$CONFIG_FILE" database name)"
    DB_PASSWORD="$(yaml_scalar "$CONFIG_FILE" database password)"
    DB_PASSWORD_SOURCE="已存在配置"
    log_info "复用已有配置: ${CONFIG_FILE}（数据库 ${DB_HOST}:${DB_PORT}/${DB_NAME}）"
    chown root:"$SERVICE_GROUP" "$CONFIG_FILE" 2>/dev/null || true
    chmod 0640 "$CONFIG_FILE" || die "$EXIT_CONFIG" "配置权限设置失败"
    return 0
  fi

  [[ -n "$DB_PASSWORD" ]] || DB_PASSWORD="$(gen_hex 16)"
  jwt_secret="$(gen_hex 32)"
  audit_key="$(gen_hex 32)"
  chromium_path="$(chromium_binary_path)"
  [[ -n "$chromium_path" ]] || chromium_path="${INSTALL_DIR}/runtime/chromium/chrome"

  {
    cat <<EOF
# auto_performance 生产配置（由 install.sh 生成于 $(date '+%F %T')，权限 0640 root:${SERVICE_GROUP}）
# 覆盖优先级：环境变量 AUTO_PERF_* > 本文件 > 内置默认值。
server:
  host: "0.0.0.0"
  port: 8080
  mode: "release"
  static_dir: "./web/dist"          # 前端已内嵌进二进制，此项保留兼容
  tmp_dir: "${DATA_DIR}/tmp"

database:
  host: "${DB_HOST}"
  port: ${DB_PORT}
  user: "${SERVICE_USER}"
  password: "${DB_PASSWORD}"
  name: "${DB_NAME}"
  sslmode: "disable"

jwt:
  secret: "${jwt_secret}"
  expire_seconds: 86400

security:
  kek_path: "${DATA_DIR}/keys/kek.key"
  report_dir: "${DATA_DIR}/reports"
  secret_key_env: "AUTO_PERF_SECRET_KEY"
  audit_hmac_key: "${audit_key}"

report:
  max_size_bytes: 52428800
  gen_timeout_seconds: 120
  chromium_path: "${chromium_path}"

k6:
  binary_path: "${INSTALL_DIR}/bin/k6"
  max_vus: 10000
  work_dir: "${DATA_DIR}/k6"

cleaner:
  low_priv_uid: 65534
  low_priv_gid: 65534
  allowed_host_ids: ""
  allowed_script_hashes: ""
EOF
  } | write_file_atomic "$CONFIG_FILE" 0640

  chown root:"$SERVICE_GROUP" "$CONFIG_FILE" 2>/dev/null || log_warn "配置属主设置失败（非 root 环境）"
  chmod 0640 "$CONFIG_FILE" || die "$EXIT_CONFIG" "配置权限设置失败"
  DB_PASSWORD_SOURCE="本次生成"

  # 额外秘密通过环境文件下发（0600），避免进入进程参数/日志
  {
    echo "# 由 install.sh 生成；权限 0600 root:${SERVICE_GROUP}。用于 AUTO_PERF_SECURITY_SECRET_KEY_ENV。"
    echo "AUTO_PERF_SECRET_KEY=$(gen_hex 32)"
  } | write_file_atomic "$ENV_FILE" 0640
  chown root:"$SERVICE_GROUP" "$ENV_FILE" 2>/dev/null || true
  log_info "配置已生成: ${CONFIG_FILE}（数据库口令来源: ${DB_PASSWORD_SOURCE}）"
}

ensure_kek() {
  log_step "6/9 生成/校验 KEK（信封加密根密钥）"
  local kek="${DATA_DIR}/keys/kek.key"
  if [[ -s "$kek" ]]; then
    log_info "KEK 已存在，保持不轮换（轮换会使历史密文不可解密）"
  else
    # 64 位 hex = 32 字节原始密钥；后端 loadKEKFile 兼容 hex 写法
    (umask 077 && gen_hex 32 | write_file_atomic "$kek" 0600) || die "$EXIT_CONFIG" "KEK 生成失败"
    log_info "已生成 KEK: ${kek}（32 字节，权限 0600）"
  fi
  chown "$SERVICE_USER:$SERVICE_GROUP" "$kek" 2>/dev/null || true
  chmod 0600 "$kek" || die "$EXIT_CONFIG" "KEK 权限设置失败"
  chmod 0700 "${DATA_DIR}/keys" 2>/dev/null || true

  local mode
  mode="$(stat -c '%a' "$kek" 2>/dev/null || echo unknown)"
  [[ "$mode" == "600" ]] || die "$EXIT_CONFIG" "KEK 权限异常: ${mode}（要求 600）"
  log_info "KEK 权限校验通过: 0600，属主 ${SERVICE_USER}"
}

# ---------------------------------------------------------------------------
# 5. PostgreSQL
# ---------------------------------------------------------------------------

pg_local_psql() {
  run_as "$SERVICE_USER" "${INSTALL_DIR}/runtime/postgres/bin/psql" \
    -h "${DATA_DIR}/run" -p "$DB_PORT" -U "$SERVICE_USER" -v ON_ERROR_STOP=1 "$@"
}

install_pg_unit() {
  local unit="/etc/systemd/system/${PG_SERVICE_NAME}.service"
  {
    cat <<EOF
[Unit]
Description=auto_performance local PostgreSQL
After=network.target
Before=auto_performance.service

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_GROUP}
ExecStart=${INSTALL_DIR}/runtime/postgres/bin/postgres -D ${DATA_DIR}/postgres \
  -c listen_addresses=127.0.0.1 -c port=${DB_PORT} -c unix_socket_directories=${DATA_DIR}/run
RuntimeDirectory=auto_performance
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
CapabilityBoundingSet=
AmbientCapabilities=
ReadWritePaths=${DATA_DIR}
UMask=0027

[Install]
WantedBy=multi-user.target
EOF
  } >"${unit}.tmp"
  install -m 0644 "${unit}.tmp" "$unit"
  rm -f "${unit}.tmp"
  install -d -m 0750 -o "$SERVICE_USER" -g "$SERVICE_GROUP" "${DATA_DIR}/run"
  systemd_reload
}

setup_database_local() {
  log_info "初始化本地 PostgreSQL 集群: ${DATA_DIR}/postgres"
  local pgb="${INSTALL_DIR}/runtime/postgres/bin"
  if [[ ! -x "${pgb}/initdb" ]]; then
    die "$EXIT_DEPS" "缺少 ${pgb}/initdb（本地模式需要 runtime/postgres 随包分发）"
  fi

  if [[ ! -s "${DATA_DIR}/postgres/PG_VERSION" ]]; then
    run_as "$SERVICE_USER" "${pgb}/initdb" -D "${DATA_DIR}/postgres" -U "$SERVICE_USER" \
      -E UTF8 --locale=C --auth-local=peer --auth-host=scram-sha-256 \
      || die "$EXIT_DB" "initdb 失败"
    log_info "initdb 完成（auth-local=peer, auth-host=scram-sha-256）"
  else
    log_info "检测到已有数据目录，跳过 initdb（幂等）"
  fi

  install -d -m 0750 -o "$SERVICE_USER" -g "$SERVICE_GROUP" "${DATA_DIR}/run"
  install_pg_unit
  if [[ "$SKIP_SYSTEMD" == "1" ]]; then
    log_warn "--skip-systemd：请自行启动 ${pgb}/postgres -D ${DATA_DIR}/postgres"
  else
    systemctl enable "$PG_SERVICE_NAME" >/dev/null 2>&1 || die "$EXIT_SERVICE" "PostgreSQL 单元 enable 失败"
    systemctl restart "$PG_SERVICE_NAME" || die "$EXIT_SERVICE" "PostgreSQL 启动失败"
    local i
    for ((i = 0; i < 30; i++)); do
      run_as "$SERVICE_USER" "${pgb}/pg_isready" -h "${DATA_DIR}/run" -p "$DB_PORT" >/dev/null 2>&1 && break
      sleep 1
    done
    run_as "$SERVICE_USER" "${pgb}/pg_isready" -h "${DATA_DIR}/run" -p "$DB_PORT" \
      || die "$EXIT_DB" "本地 PostgreSQL 未就绪"
    log_info "本地 PostgreSQL 已就绪（127.0.0.1:${DB_PORT}）"
  fi

  # 设置应用账号口令（psql 变量 :'pw' 由 psql 负责转义，避免拼接注入）
  pg_local_psql -d postgres -v pw="$DB_PASSWORD" <<'SQL' || die "$EXIT_DB" "设置数据库口令失败"
ALTER ROLE autoperf WITH LOGIN PASSWORD :'pw';
SQL

  local exists
  exists="$(pg_local_psql -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '${DB_NAME}'" | tr -d ' \r\n')"
  if [[ "$exists" == "1" ]]; then
    log_info "数据库已存在: ${DB_NAME}"
  else
    pg_local_psql -d postgres -c "CREATE DATABASE ${DB_NAME} OWNER ${SERVICE_USER} ENCODING 'UTF8'" \
      || die "$EXIT_DB" "创建数据库 ${DB_NAME} 失败"
    log_info "已创建数据库: ${DB_NAME}（owner=${SERVICE_USER}）"
  fi
  pg_local_psql -d "$DB_NAME" -c "REVOKE CREATE ON SCHEMA public FROM PUBLIC" >/dev/null \
    || log_warn "收回 public schema 建表权限失败（请人工复核）"
}

# admin_psql：外部实例的管理连接（PG_ADMIN_DSN > postgres 系统用户 peer > 当前环境变量）
admin_psql() {
  if [[ -n "${PG_ADMIN_DSN:-}" ]]; then
    psql "$PG_ADMIN_DSN" -v ON_ERROR_STOP=1 "$@"
  elif [[ "$(id -u)" == "0" ]] && id -u postgres >/dev/null 2>&1; then
    runuser -u postgres -- psql -v ON_ERROR_STOP=1 "$@"
  else
    psql -v ON_ERROR_STOP=1 "$@"
  fi
}

setup_database_external() {
  log_info "初始化外部 PostgreSQL 实例（${DB_HOST}:${DB_PORT}）"
  admin_psql -tAc 'SELECT 1' >/dev/null 2>&1 \
    || die "$EXIT_DB" "无法连接 PostgreSQL 管理会话（请设置 PG_ADMIN_DSN 或以 postgres 系统用户 peer 认证执行）"

  local exists
  exists="$(admin_psql -tAc "SELECT 1 FROM pg_roles WHERE rolname = '${SERVICE_USER}'" | tr -d ' \r\n')"
  if [[ "$exists" == "1" ]]; then
    admin_psql -v pw="$DB_PASSWORD" <<'SQL' || die "$EXIT_DB" "更新账号口令失败"
ALTER ROLE autoperf WITH LOGIN PASSWORD :'pw';
SQL
    log_info "账号已存在，已同步口令: ${SERVICE_USER}"
  else
    admin_psql -v pw="$DB_PASSWORD" <<'SQL' || die "$EXIT_DB" "创建低权限账号失败"
CREATE ROLE autoperf WITH LOGIN PASSWORD :'pw' NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
SQL
    log_info "已创建低权限账号: ${SERVICE_USER}（NOSUPERUSER/NOCREATEDB/NOCREATEROLE）"
  fi

  exists="$(admin_psql -tAc "SELECT 1 FROM pg_database WHERE datname = '${DB_NAME}'" | tr -d ' \r\n')"
  if [[ "$exists" == "1" ]]; then
    log_info "数据库已存在: ${DB_NAME}"
  else
    admin_psql -c "CREATE DATABASE ${DB_NAME} OWNER ${SERVICE_USER} ENCODING 'UTF8'" \
      || die "$EXIT_DB" "创建数据库 ${DB_NAME} 失败"
    log_info "已创建数据库: ${DB_NAME}（owner=${SERVICE_USER}）"
  fi
  admin_psql -d "$DB_NAME" -c "REVOKE CREATE ON SCHEMA public FROM PUBLIC" >/dev/null \
    || log_warn "收回 public schema 建表权限失败（请人工复核）"
  admin_psql -c "REVOKE ALL ON DATABASE ${DB_NAME} FROM PUBLIC" >/dev/null || true
  admin_psql -c "GRANT CONNECT ON DATABASE ${DB_NAME} TO ${SERVICE_USER}" >/dev/null || true
}

setup_database() {
  log_step "7/9 初始化 PostgreSQL（模式: ${DB_MODE}）"
  if [[ "$DB_MODE" == "local" ]]; then
    if [[ "$SKIP_SYSTEMD" == "1" ]]; then
      die "$EXIT_ENV" "--skip-systemd 不支持 --db-mode local（本地集群需要 systemd 托管）；请改用 --db-mode external"
    fi
    setup_database_local
  else
    setup_database_external
  fi
}

# ---------------------------------------------------------------------------
# 6. 迁移 + systemd + 启动
# ---------------------------------------------------------------------------

run_migrate() {
  log_step "8/9 执行数据库迁移"
  run_as "$SERVICE_USER" env HOME="$DATA_DIR" \
    "${INSTALL_DIR}/bin/auto_performance" -config "$CONFIG_FILE" migrate up \
    || die "$EXIT_DB" "数据库迁移失败（migrate up），请检查数据库连通性/权限"
  log_info "数据库迁移完成（migrate up）"
}

install_service_unit() {
  local unit="/etc/systemd/system/${SERVICE_NAME}.service"
  local after="network-online.target"
  if [[ "$DB_MODE" == "local" ]]; then
    after="${after} ${PG_SERVICE_NAME}.service"
  fi

  {
    cat <<EOF
[Unit]
Description=auto_performance performance testing platform
Documentation=file://${INSTALL_DIR}/README.md
After=${after}
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_GROUP}
WorkingDirectory=${DATA_DIR}
Environment=HOME=${DATA_DIR}
EnvironmentFile=-${ENV_FILE}
ExecStart=${INSTALL_DIR}/bin/auto_performance -config ${CONFIG_FILE}
Restart=on-failure
RestartSec=5
TimeoutStopSec=30

# ---- 安全加固（DES-E-001：非 root + 最小权限）----
NoNewPrivileges=true
CapabilityBoundingSet=
AmbientCapabilities=
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
ProtectClock=true
ProtectHostname=true
RestrictSUIDSGID=true
LockPersonality=true
RestrictRealtime=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
# 说明：不使用 RestrictNamespaces/SystemCallFilter，避免破坏 Chromium 沙箱与 PDF 渲染；
#       低权限运行 + NoNewPrivileges + ProtectSystem 已满足 DES-E-001 的服务非 root 要求。
ReadWritePaths=${DATA_DIR}
UMask=0027
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
  } >"${unit}.tmp"
  install -m 0644 "${unit}.tmp" "$unit"
  rm -f "${unit}.tmp"
  systemd_reload
  log_info "已注册 systemd 单元: ${unit}（User=${SERVICE_USER}, NoNewPrivileges=true）"
}

start_service() {
  log_step "9/9 启动服务与健康检查"
  if [[ "$SKIP_SYSTEMD" == "1" ]]; then
    log_warn "--skip-systemd：未启动服务，可手工执行："
    log_warn "  runuser -u ${SERVICE_USER} -- ${INSTALL_DIR}/bin/auto_performance -config ${CONFIG_FILE}"
    return 0
  fi

  systemctl enable "$SERVICE_NAME" >/dev/null 2>&1 || die "$EXIT_SERVICE" "服务 enable 失败"
  if ! systemctl restart "$SERVICE_NAME"; then
    journalctl -u "$SERVICE_NAME" -n 50 --no-pager >&2 || true
    die "$EXIT_SERVICE" "服务启动失败（见上方 journalctl 输出）"
  fi

  local port url
  port="$(yaml_scalar "$CONFIG_FILE" server port)"
  url="http://127.0.0.1:${port:-8080}/healthz"
  if ! wait_healthy "$url" 60 1; then
    journalctl -u "$SERVICE_NAME" -n 50 --no-pager >&2 || true
    die "$EXIT_HEALTH" "服务健康检查失败（${url}）"
  fi
}

print_summary() {
  local port url
  port="$(yaml_scalar "$CONFIG_FILE" server port)"
  url="http://127.0.0.1:${port:-8080}/"

  cat <<EOF

================ 安装完成 ================
版本:        ${PKG_VERSION}
安装目录:    ${INSTALL_DIR}
数据目录:    ${DATA_DIR}（权限 0750，属主 ${SERVICE_USER}）
配置:        ${CONFIG_FILE}（0640 root:${SERVICE_GROUP}）
KEK:         ${DATA_DIR}/keys/kek.key（0600，属主 ${SERVICE_USER}）
数据库:      ${DB_MODE} 模式 ${DB_HOST}:${DB_PORT}/${DB_NAME}（账号 ${SERVICE_USER}）
服务:        systemctl status ${SERVICE_NAME}
访问地址:    ${url}
日志:        journalctl -u ${SERVICE_NAME} -f

后续动作:
  1. 浏览器访问 ${url}，使用内置 admin 账号登录（首次登录强制修改口令）。
  2. 在「系统设置 → SSH/Host」录入可信清理主机，并在配置 cleaner.allowed_host_ids 中放行。
  3. 升级: sudo ./scripts/upgrade.sh；卸载: sudo ./scripts/uninstall.sh
=========================================
EOF
}

main() {
  parse_args "$@"
  preflight
  ensure_user
  ensure_dirs
  install_binaries
  render_config
  ensure_kek
  setup_database
  run_migrate

  if [[ "$SKIP_SYSTEMD" == "1" ]]; then
    log_info "跳过 systemd 注册（--skip-systemd）"
  else
    install_service_unit
    if [[ "$DO_START" == "1" ]]; then
      start_service
    else
      log_warn "--no-start：已注册单元但未启动"
    fi
  fi

  print_summary
  exit "$EXIT_OK"
}

main "$@"
