#!/usr/bin/env bash
# run-selftest.sh —— 部署脚本逻辑自测（**stub 环境**，不改动真实系统）。
#
# 目的：在没有 systemd / PostgreSQL / Docker / 目标二进制的开发机上，验证
#   1) install.sh --check-only：manifest 逐文件 SHA256 校验
#   2) 篡改包内文件 / 夹带未登记文件 → 校验失败中止（退出码 11，TEST-T-001）
#   3) upgrade.sh 正常升级路径（退出码 0，版本更新）
#   4) upgrade.sh 迁移失败 → 自动回滚（退出码 61，二进制/VERSION 恢复）（TEST-T-003）
#   5) uninstall.sh --dry-run 计划输出（不做变更）
#
# 原理：在临时目录构造「安装态 + 离线包」夹具，并用 stub 覆盖系统命令
#       （systemctl / psql / pg_dump / pg_restore / curl / journalctl / runuser / install / chown），
#       仅用于驱动脚本控制流；生产环境请勿设置 AUTO_PERF_ALLOW_NONROOT。
#
# 用法：bash deploy/selftest/run-selftest.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORK="$(mktemp -d)"
STUB="${WORK}/stub"
STATE="${WORK}/state"
LOG="${WORK}/cmd.log"
PASS=0
FAIL=0

cleanup() {
  rm -rf "$WORK"
}
trap cleanup EXIT

ok() {
  PASS=$((PASS + 1))
  printf '  \033[32mPASS\033[0m %s\n' "$1"
}

bad() {
  FAIL=$((FAIL + 1))
  printf '  \033[31mFAIL\033[0m %s\n' "$1"
  if [[ -s "$LOG" ]]; then
    printf '       ---- 最近输出 ----\n'
    tail -n 20 "$LOG" | sed 's/^/       /'
  fi
}

# run_expect <期望退出码> <描述> <命令...>
run_expect() {
  local expect="$1" desc="$2"
  shift 2
  local rc=0
  : >"$LOG"
  set +e
  "$@" >>"$LOG" 2>&1
  rc=$?
  set -e
  if [[ "$rc" == "$expect" ]]; then
    ok "${desc}（退出码 ${rc}）"
  else
    bad "${desc}：期望退出码 ${expect}，实际 ${rc}"
  fi
}

# ---------------------------------------------------------------------------
# stub 环境
# ---------------------------------------------------------------------------
create_stubs() {
  mkdir -p "$STUB" "$STATE"

  cat >"${STUB}/systemctl" <<'EOF'
#!/bin/sh
STATE="${STUB_STATE:?}"
case "$1" in
  is-active) [ -f "${STATE}/active" ] && exit 0 || exit 3 ;;
  stop) rm -f "${STATE}/active"; exit 0 ;;
  start | restart) touch "${STATE}/active"; exit 0 ;;
  *) exit 0 ;;
esac
EOF

  cat >"${STUB}/curl" <<'EOF'
#!/bin/sh
echo '{"code":0,"message":"ok","data":{"status":"ok","db":"up"}}'
EOF

  cat >"${STUB}/psql" <<'EOF'
#!/bin/sh
for a in "$@"; do
  case "$a" in
    -tAc)
      echo 1
      exit 0
      ;;
  esac
done
exit 0
EOF

  cat >"${STUB}/pg_dump" <<'EOF'
#!/bin/sh
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -f)
      out="$2"
      shift 2
      ;;
    *) shift ;;
  esac
done
if [ -n "$out" ]; then
  printf 'PGDUMP-FAKE\n' >"$out"
fi
exit 0
EOF

  cat >"${STUB}/pg_restore" <<'EOF'
#!/bin/sh
exit 0
EOF

  cat >"${STUB}/journalctl" <<'EOF'
#!/bin/sh
exit 0
EOF

  cat >"${STUB}/runuser" <<'EOF'
#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    -u) shift 2 ;;
    --) shift; break ;;
    *) break ;;
  esac
done
exec "$@"
EOF

  cat >"${STUB}/chown" <<'EOF'
#!/bin/sh
exit 0
EOF

  # 非 root 自测：install 简化实现（忽略 -o/-g/-m 失败；Windows 文件系统 chmod 常受限）
  cat >"${STUB}/install" <<'EOF'
#!/usr/bin/env bash
set -e
mode=""
mkdirs=0
paths=()
while (($#)); do
  case "$1" in
    -d) mkdirs=1; shift ;;
    -m) mode="$2"; shift 2 ;;
    -o|-g) shift 2 ;;
    -o=*|-g=*) shift ;;
    *) paths+=("$1"); shift ;;
  esac
done
if ((mkdirs)); then
  for p in "${paths[@]}"; do
    mkdir -p "$p"
    if [ -n "$mode" ]; then chmod "$mode" "$p" 2>/dev/null || true; fi
  done
  exit 0
fi
dest="${paths[${#paths[@]}-1]}"
for ((i = 0; i < ${#paths[@]} - 1; i++)); do
  cp -f "${paths[i]}" "$dest"
  if [ -n "$mode" ]; then chmod "$mode" "$dest" 2>/dev/null || true; fi
done
exit 0
EOF

  chmod +x "${STUB}"/*
}

# ---------------------------------------------------------------------------
# 夹具
# ---------------------------------------------------------------------------

gen_manifest() {
  local dir="$1" version="$2"
  {
    echo "# selftest fixture manifest"
    echo "manifest_version: 1"
    echo "product: auto_performance"
    echo "version: \"${version}\""
    echo "files:"
    (cd "$dir" && find . -type f ! -name manifest.yaml ! -name manifest.sha256 | sed 's#^\./##' | sort) |
      while IFS= read -r f; do
        printf '  - path: "%s"\n    sha256: "%s"\n' "$f" "$(sha256sum "${dir}/${f}" | awk '{print $1}')"
      done
  } >"${dir}/manifest.yaml"
  (cd "$dir" && sha256sum manifest.yaml >manifest.sha256)
}

# make_pkg <目录> <版本>
make_pkg() {
  local dir="$1" version="$2"
  rm -rf "$dir"
  mkdir -p "${dir}"/{bin,config,scripts/lib,runtime}
  install -m 0755 "${REPO_ROOT}/deploy/scripts/install.sh" "${dir}/scripts/install.sh"
  install -m 0755 "${REPO_ROOT}/deploy/scripts/upgrade.sh" "${dir}/scripts/upgrade.sh"
  install -m 0755 "${REPO_ROOT}/deploy/scripts/uninstall.sh" "${dir}/scripts/uninstall.sh"
  install -m 0644 "${REPO_ROOT}/deploy/scripts/lib/common.sh" "${dir}/scripts/lib/common.sh"
  install -m 0644 "${REPO_ROOT}/backend/config/config.example.yaml" "${dir}/config/config.example.yaml"
  cat >"${dir}/bin/auto_performance" <<EOF
#!/bin/sh
if [ "\${1:-}" = "version" ]; then
  echo "${version}"
  exit 0
fi
exit 0
EOF
  chmod +x "${dir}/bin/auto_performance"
  printf '%s\n' "$version" >"${dir}/VERSION"
  gen_manifest "$dir" "$version"
}

# make_installed <根目录> <版本>
make_installed() {
  local base="$1" version="$2"
  rm -rf "$base"
  mkdir -p "${base}"/{opt/bin,etc,data/keys,data/reports,backups}
  cat >"${base}/opt/bin/auto_performance" <<EOF
#!/bin/sh
if [ "\${1:-}" = "version" ]; then
  echo "${version}"
  exit 0
fi
exit 0
EOF
  chmod +x "${base}/opt/bin/auto_performance"
  printf '%s\n' "$version" >"${base}/opt/VERSION"
  cat >"${base}/etc/config.yaml" <<'EOF'
server:
  host: "127.0.0.1"
  port: 18080
  mode: "release"
database:
  host: "127.0.0.1"
  port: 5432
  user: "autoperf"
  password: "selftest-password-1234"
  name: "auto_performance"
  sslmode: "disable"
jwt:
  secret: "selftest-jwt-secret"
EOF
  chmod 0640 "${base}/etc/config.yaml" 2>/dev/null || true
}

run_pkg_script() {
  local base="$1" script="$2"
  shift 2
  env INSTALL_DIR="${base}/opt" CONFIG_DIR="${base}/etc" DATA_DIR="${base}/data" \
    BACKUP_DIR="${base}/backups" AUTO_PERF_ALLOW_NONROOT=1 STUB_STATE="${STATE}" \
    PATH="${STUB}:${PATH}" bash "${script}" "$@"
}

# ---------------------------------------------------------------------------
# 用例
# ---------------------------------------------------------------------------

main() {
  command -v sha256sum >/dev/null || {
    echo "需要 sha256sum（Git Bash/WSL/Linux）" >&2
    exit 3
  }
  create_stubs

  local pkg="${WORK}/pkg" base="${WORK}/inst"
  make_pkg "$pkg" "0.2.0"
  make_installed "$base" "0.1.0"

  printf '\n[1] install.sh --check-only 完整性校验\n'
  run_expect 0 "未篡改包应通过校验" \
    run_pkg_script "$base" "${pkg}/scripts/install.sh" --check-only --allow-missing-deps --no-pdf

  printf '\n[2] 篡改检测（DES-T-001 / TEST-T-001）\n'
  printf '\n# tampered\n' >>"${pkg}/bin/auto_performance"
  run_expect 11 "篡改包内文件应中止（退出码 11）" \
    run_pkg_script "$base" "${pkg}/scripts/install.sh" --check-only --allow-missing-deps --no-pdf
  make_pkg "$pkg" "0.2.0"

  printf '\n[3] 夹带未登记文件检测\n'
  printf '#!/bin/sh\necho pwned\n' >"${pkg}/bin/extra-tool"
  run_expect 11 "存在未登记文件应中止（退出码 11）" \
    run_pkg_script "$base" "${pkg}/scripts/install.sh" --check-only --allow-missing-deps --no-pdf
  rm -f "${pkg}/bin/extra-tool"

  printf '\n[4] upgrade.sh 正常升级路径\n'
  make_installed "$base" "0.1.0"
  rm -f "${STATE}/active"
  run_expect 0 "升级成功（退出码 0）" run_pkg_script "$base" "${pkg}/scripts/upgrade.sh"
  if [[ "$(tr -d ' \r\n' <"${base}/opt/VERSION")" == "0.2.0" ]]; then
    ok "VERSION 已更新为 0.2.0"
  else
    bad "VERSION 未更新（实际 $(tr -d ' \r\n' <"${base}/opt/VERSION" 2>/dev/null || echo missing)）"
  fi
  if [[ "$("${base}/opt/bin/auto_performance" version)" == "0.2.0" ]]; then
    ok "二进制已替换为新版本"
  else
    bad "二进制未替换"
  fi
  if compgen -G "${base}/backups/2*/app.tar.gz" >/dev/null && compgen -G "${base}/backups/2*/db.dump" >/dev/null; then
    ok "备份产物齐备（app.tar.gz + db.dump）"
  else
    bad "备份产物缺失"
  fi

  printf '\n[5] upgrade.sh 迁移失败 → 自动回滚（DES-T-003 / TEST-T-003）\n'
  make_installed "$base" "0.1.0"
  rm -f "${STATE}/active"
  run_expect 61 "迁移失败应回滚并以 61 退出" \
    env INSTALL_DIR="${base}/opt" CONFIG_DIR="${base}/etc" DATA_DIR="${base}/data" \
    BACKUP_DIR="${base}/backups" AUTO_PERF_ALLOW_NONROOT=1 STUB_STATE="${STATE}" \
    SIMULATE_MIGRATE_FAILURE=1 PATH="${STUB}:${PATH}" \
    bash "${pkg}/scripts/upgrade.sh"

  if [[ "$("${base}/opt/bin/auto_performance" version)" == "0.1.0" ]]; then
    ok "回滚后旧二进制已恢复（0.1.0）"
  else
    bad "回滚后二进制未恢复（实际 $("${base}/opt/bin/auto_performance" version)）"
  fi
  if [[ "$(tr -d ' \r\n' <"${base}/opt/VERSION")" == "0.1.0" ]]; then
    ok "回滚后 VERSION 已恢复为 0.1.0"
  else
    bad "回滚后 VERSION 未恢复"
  fi
  if [[ -f "${STATE}/active" ]]; then
    ok "回滚后服务已重新启动（stub 状态 active）"
  else
    bad "回滚后服务未启动"
  fi

  printf '\n[6] uninstall.sh --dry-run（只读计划）\n'
  local out
  out="$(env INSTALL_DIR="${base}/opt" CONFIG_DIR="${base}/etc" DATA_DIR="${base}/data" \
    PATH="${STUB}:${PATH}" bash "${pkg}/scripts/uninstall.sh" --dry-run --purge-data 2>&1)"
  if [[ "$out" == *"物理删除"* && -d "${base}/opt" ]]; then
    ok "dry-run 输出删除计划且未改动文件"
  else
    bad "dry-run 行为异常"
  fi

  printf '\n================ selftest 结果 ================\n'
  printf 'PASS: %d   FAIL: %d\n' "$PASS" "$FAIL"
  if (( FAIL > 0 )); then
    exit 1
  fi
}

main "$@"
