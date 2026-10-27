#!/usr/bin/env bash
# deploy/scripts/lib/common.sh —— install/upgrade/uninstall 共用函数库。
#
# 约定：
#   - 本文件只定义常量与函数，不产生任何副作用（可安全 source）。
#   - 调用方负责 `set -euo pipefail` 与参数解析。
#   - 所有路径由调用方传入，本文件不假设包位置。
#   - 对齐 security-design-report.md：DES-T-001（包完整性）、DES-T-003（升级回滚）、
#     DES-E-001（低权限运行）、DES-I-002（KEK 0600）。

# ---------------------------------------------------------------------------
# 退出码契约（脚本返回值，便于自动化/CI 判定）
# ---------------------------------------------------------------------------
EXIT_OK=0
EXIT_USAGE=2        # 参数错误
EXIT_ENV=3          # 运行环境不满足（非 root / 非 Linux / 缺命令 / 磁盘不足）
EXIT_INTEGRITY=11   # 包完整性校验失败（DES-T-001）
EXIT_DEPS=12        # 运行时依赖缺失（k6/chromium/postgres）
EXIT_DB=20          # 数据库初始化/连接/迁移失败
EXIT_CONFIG=30      # 配置生成/校验失败
EXIT_SERVICE=40     # systemd 服务注册/启停失败
EXIT_HEALTH=41      # 健康检查失败
EXIT_ROLLBACK=61    # 升级失败并已回滚（需要人工确认）

# ---------------------------------------------------------------------------
# 日志（统一带时间戳；错误输出到 stderr）
# ---------------------------------------------------------------------------
log_info() { printf '[%s] INFO  %s\n' "$(date '+%F %T')" "$*"; }
log_warn() { printf '[%s] WARN  %s\n' "$(date '+%F %T')" "$*" >&2; }
log_error() { printf '[%s] ERROR %s\n' "$(date '+%F %T')" "$*" >&2; }
log_step() { printf '\n[%s] ==== %s ====\n' "$(date '+%F %T')" "$*"; }

# die <退出码> <消息...>
die() {
  local code="$1"
  shift
  log_error "$*"
  exit "$code"
}

# ---------------------------------------------------------------------------
# 环境前置检查
# ---------------------------------------------------------------------------

# require_root：生产路径必须 root（systemd/useradd/psql 需要）。
# AUTO_PERF_ALLOW_NONROOT=1 仅供 deploy/selftest 的 stub 环境使用，生产禁止。
require_root() {
  if [[ "${AUTO_PERF_ALLOW_NONROOT:-0}" == "1" ]]; then
    log_warn "AUTO_PERF_ALLOW_NONROOT=1：跳过 root 校验（仅限 selftest/stub 环境，生产禁止）"
    return 0
  fi
  if [[ "$(id -u)" != "0" ]]; then
    die "$EXIT_ENV" "需要 root 权限执行（请使用 sudo）"
  fi
}

# require_linux：部署目标必须为 Linux（systemd 形态）
require_linux() {
  if [[ "${AUTO_PERF_ALLOW_NONROOT:-0}" == "1" ]]; then
    log_warn "AUTO_PERF_ALLOW_NONROOT=1：跳过平台校验（当前 $(uname -s)）"
    return 0
  fi
  case "$(uname -s)" in
    Linux) : ;;
    *) die "$EXIT_ENV" "仅支持 Linux 部署（当前 $(uname -s)）" ;;
  esac
}

# require_cmd <cmd...>：缺任一命令则失败
require_cmd() {
  local c missing=()
  for c in "$@"; do
    command -v "$c" >/dev/null 2>&1 || missing+=("$c")
  done
  if (( ${#missing[@]} > 0 )); then
    die "$EXIT_ENV" "缺少必需命令: ${missing[*]}"
  fi
}

# require_any_cmd <cmd...>：至少存在一个（如 curl / wget）
require_any_cmd() {
  local c
  for c in "$@"; do
    command -v "$c" >/dev/null 2>&1 && return 0
  done
  die "$EXIT_ENV" "缺少必需命令，需至少提供其一: $*"
}

# require_disk_kb <path> <最小 KB>
require_disk_kb() {
  local path="$1" need="$2" avail
  mkdir -p "$path" 2>/dev/null || true
  avail="$(df -Pk "$path" 2>/dev/null | awk 'NR==2 {print $4}')"
  if [[ -n "$avail" ]] && (( avail < need )); then
    die "$EXIT_ENV" "磁盘空间不足：$path 可用 ${avail}KB < 需要 ${need}KB"
  fi
}

# ---------------------------------------------------------------------------
# 通用工具
# ---------------------------------------------------------------------------

# gen_hex [字节数=32]：crypto 级随机 → hex（用 /dev/urandom，不依赖 openssl）
gen_hex() {
  local bytes="${1:-32}" out
  out="$(od -An -tx1 -N "$bytes" /dev/urandom | tr -d ' \n')"
  if [[ ${#out} -ne $((bytes * 2)) ]]; then
    die "$EXIT_ENV" "生成随机密钥失败（/dev/urandom 不可用）"
  fi
  printf '%s' "$out"
}

# yaml_scalar <file> <section> <key>：读取 install 生成的简单 YAML 标量。
# 仅支持 `section:` 下的一级 key: value（部署配置即此结构），避免引入 yq 依赖。
yaml_scalar() {
  local file="$1" section="$2" key="$3"
  [[ -f "$file" ]] || return 0
  awk -v section="$section" -v key="$key" '
    $0 ~ "^"section":" { insec = 1; next }
    insec && /^[^[:space:]#]/ { insec = 0 }
    insec && $0 ~ "^[[:space:]]+"key":" {
      v = $0
      sub("^[[:space:]]+"key":[[:space:]]*", "", v)
      sub(/[[:space:]]+#.*$/, "", v)
      gsub(/^["\x27]|["\x27]$/, "", v)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", v)
      print v
      exit
    }
  ' "$file" | tr -d '\r'
}

# version_gt <a> <b>：a > b（SemVer 近似比较，sort -V 语义）
version_gt() {
  local a="$1" b="$2"
  [[ "$a" == "$b" ]] && return 1
  [[ "$(printf '%s\n%s\n' "$a" "$b" | sort -V | tail -n1)" == "$a" ]]
}

# write_file_atomic <dst> <mode>：从 stdin 原子写入（同目录临时文件 + mv）
write_file_atomic() {
  local dst="$1" mode="$2" tmp
  tmp="$(mktemp "${dst}.XXXXXX")" || return 1
  cat >"$tmp" || { rm -f "$tmp"; return 1; }
  chmod "$mode" "$tmp" || { rm -f "$tmp"; return 1; }
  mv -f "$tmp" "$dst"
}

# ---------------------------------------------------------------------------
# manifest.yaml 解析与完整性校验（DES-T-001）
# ---------------------------------------------------------------------------

# manifest_field <manifest> <key>：读取顶层标量（version 等）
manifest_field() {
  local file="$1" key="$2"
  awk -v key="$key" '
    $0 ~ "^"key":" {
      v = $0
      sub("^"key":[[:space:]]*", "", v)
      gsub(/"/, "", v)
      sub(/[[:space:]]+#.*$/, "", v)
      gsub(/[[:space:]]+$/, "", v)
      print v
      exit
    }
  ' "$file" | tr -d '\r'
}

# manifest_expected_hashes <manifest>：输出 "<sha256>  <相对路径>" 行（sha256sum -c 格式）
manifest_expected_hashes() {
  local file="$1"
  awk '
    /^files:[[:space:]]*$/ { infiles = 1; next }
    infiles && /^[^[:space:]#]/ { infiles = 0 }
    infiles && /^[[:space:]]*-[[:space:]]*path:/ {
      p = $0
      sub(/^[[:space:]]*-[[:space:]]*path:[[:space:]]*/, "", p)
      gsub(/"/, "", p)
      next
    }
    infiles && /^[[:space:]]*sha256:/ && p != "" {
      h = $0
      sub(/^[[:space:]]*sha256:[[:space:]]*/, "", h)
      gsub(/"/, "", h)
      sub(/[[:space:]]+$/, "", h)
      printf "%s  %s\n", tolower(h), p
      p = ""
    }
  ' "$file" | tr -d '\r'
}

# verify_package_integrity <pkg_root> [--quiet]
#   1) manifest.yaml 存在且记录文件；
#   2) 每个记录文件都存在且 SHA256 一致；
#   3) bin/ config/ scripts/ runtime/ 下不得存在未登记文件（防夹带）。
# 返回 0=通过，非 0=失败（并打印明细）。
verify_package_integrity() {
  local root="$1" quiet="${2:-}"
  local manifest="${root}/manifest.yaml"
  local expected line exp path actual total=0 passed=0 failed=0
  local -a mismatches=()

  if [[ ! -f "$manifest" ]]; then
    log_error "缺少清单文件: ${manifest}"
    return 1
  fi

  expected="$(manifest_expected_hashes "$manifest")"
  if [[ -z "$expected" ]]; then
    log_error "manifest.yaml 未记录任何文件哈希（files: 段为空）"
    return 1
  fi

  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    exp="${line%%  *}"
    path="${line#*  }"
    total=$((total + 1))
    if [[ ! -f "${root}/${path}" ]]; then
      failed=$((failed + 1))
      mismatches+=("缺失: ${path}")
      continue
    fi
    actual="$(cd "$root" && sha256sum -- "$path" 2>/dev/null | awk '{print $1}')"
    if [[ "$actual" == "$exp" ]]; then
      passed=$((passed + 1))
    else
      failed=$((failed + 1))
      mismatches+=("哈希不符: ${path}（期望 ${exp}，实际 ${actual:-<读取失败>}）")
    fi
  done <<< "$expected"

  # 未登记文件检测（防夹带/后门）
  local listed f unlisted=0
  listed="$(printf '%s\n' "$expected" | sed 's/^[0-9a-f]\{64\}  //')"
  while IFS= read -r f; do
    [[ -z "$f" ]] && continue
    if ! grep -Fxq -- "$f" <<< "$listed"; then
      unlisted=$((unlisted + 1))
      mismatches+=("未登记文件: ${f}")
    fi
  done < <(cd "$root" && find bin config scripts runtime -type f 2>/dev/null | sed 's#^\./##' | sort)

  if (( failed == 0 && unlisted == 0 )); then
    # 清单自身二次校验：manifest.sha256 记录 manifest.yaml 的哈希，
    # 防止“同时替换清单与包内文件”绕开逐文件比对。
    if [[ -f "${root}/manifest.sha256" ]]; then
      if ! (cd "$root" && sha256sum -c manifest.sha256 >/dev/null 2>&1); then
        log_error "manifest.yaml 与 manifest.sha256 不一致（清单可能被篡改）"
        return 1
      fi
    fi
    [[ "$quiet" == "--quiet" ]] || log_info "完整性校验通过：${passed} 个文件 SHA256 全部一致"
    return 0
  fi

  log_error "完整性校验失败：${failed} 个文件异常，${unlisted} 个未登记文件（期望清单: ${manifest}）"
  local m
  for m in "${mismatches[@]}"; do
    log_error "  - ${m}"
  done
  return 1
}

# manifest_dep_bundled <manifest> <dep>：输出 true/false（该依赖是否随包分发）
manifest_dep_bundled() {
  local file="$1" dep="$2"
  awk -v dep="$dep" '
    $0 ~ "^  "dep":[[:space:]]*$" { indep = 1; next }
    indep && /^  [a-zA-Z0-9_]+:/ { indep = 0 }
    indep && /^[[:space:]]+bundled:/ {
      v = $0
      sub(/^[[:space:]]+bundled:[[:space:]]*/, "", v)
      gsub(/[[:space:]]|"/, "", v)
      print v
      exit
    }
  ' "$file" | tr -d '\r'
}

# ---------------------------------------------------------------------------
# HTTP / 健康检查
# ---------------------------------------------------------------------------

# http_get <url>：优先 curl，其次 wget（离线最小系统可能只有其一）
http_get() {
  local url="$1"
  local insecure=""
  # HTTPS 使用服务自签证书时跳过校验（仅本机健康检查/探测用）
  [[ "$url" == https://* ]] && insecure="-k"
  if command -v curl >/dev/null 2>&1; then
    curl -fsS $insecure --max-time 5 "$url" 2>/dev/null
  elif command -v wget >/dev/null 2>&1; then
    if [[ -n "$insecure" ]]; then
      wget --no-check-certificate -qO- --timeout=5 "$url" 2>/dev/null
    else
      wget -qO- --timeout=5 "$url" 2>/dev/null
    fi
  else
    return 127
  fi
}

# wait_healthy <url> [超时秒=60] [要求db_up=1]
#   判定：HTTP 200 且响应含 "status":"ok"；要求 db_up=1 时还需 "db":"up"。
#   仅看 HTTP 200 会把「DB 不可用的降级进程」判为健康，因此显式校验 db 状态。
wait_healthy() {
  local url="$1" timeout="${2:-60}" require_db="${3:-1}"
  local i body
  for ((i = 0; i < timeout; i++)); do
    body="$(http_get "$url")"
    if [[ -n "$body" ]] \
      && printf '%s' "$body" | grep -Eq '"status"[[:space:]]*:[[:space:]]*"ok"' \
      && { [[ "$require_db" != "1" ]] || printf '%s' "$body" | grep -Eq '"db"[[:space:]]*:[[:space:]]*"up"'; }; then
      log_info "健康检查通过（${url}）: ${body}"
      return 0
    fi
    sleep 1
  done
  log_error "健康检查失败（${timeout}s 内未就绪）: ${url}${body:+，最后响应: ${body}}"
  return 1
}

# ---------------------------------------------------------------------------
# systemd / 身份切换
# ---------------------------------------------------------------------------

systemd_reload() { systemctl daemon-reload >/dev/null 2>&1 || true; }

svc_exists() { [[ -f "/etc/systemd/system/$1.service" ]]; }

svc_is_active() { systemctl is-active --quiet "$1" 2>/dev/null; }

svc_stop() {
  systemctl stop "$1" >/dev/null 2>&1 || true
  # 等待进程真正退出（避免替换二进制时 Text file busy）
  local i
  for ((i = 0; i < 30; i++)); do
    svc_is_active "$1" || return 0
    sleep 1
  done
  return 0
}

svc_start() { systemctl start "$1"; }

# run_as <user> <cmd...>：以指定用户执行（runuser 优先，回退 su）
run_as() {
  local user="$1"
  shift
  if command -v runuser >/dev/null 2>&1; then
    runuser -u "$user" -- "$@"
  else
    su -s /bin/sh -c "$(printf '%q ' "$@")" "$user"
  fi
}

# ---------------------------------------------------------------------------
# 交互确认 / 备份清理
# ---------------------------------------------------------------------------

# confirm_text <提示> <期望输入>：非交互环境返回失败
confirm_text() {
  local prompt="$1" expect="$2" answer=""
  if [[ ! -t 0 ]]; then
    return 1
  fi
  read -r -p "$prompt" answer || return 1
  [[ "$answer" == "$expect" ]]
}

# prune_backups <dir> <keep>：仅保留最近 keep 个 ts 目录（其余删除）
prune_backups() {
  local dir="$1" keep="${2:-5}" d i=0
  [[ -d "$dir" ]] || return 0
  while IFS= read -r d; do
    i=$((i + 1))
    if (( i > keep )); then
      rm -rf -- "$d"
      log_info "清理旧备份: $d"
    fi
  done < <(find "$dir" -mindepth 1 -maxdepth 1 -type d -name '2*' | sort -r)
}

# is_tls_enabled <config>：判断配置中 server.tls.enabled 是否为 true（用于健康检查 scheme 选择）。
is_tls_enabled() {
  local file="${1:-${CONFIG_FILE:-}}"
  [[ -f "$file" ]] || return 1
  awk '
    /^server:[[:space:]]*$/ { s=1; next }
    s && /^[^[:space:]#]/ { s=0 }
    s && /^[[:space:]]+tls:[[:space:]]*$/ { t=1; next }
    t && /^[[:space:]]+enabled:[[:space:]]*true/ { found=1; exit }
    END { exit(found?0:1) }
  ' "$file"
}
