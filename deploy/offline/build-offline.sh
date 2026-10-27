#!/usr/bin/env bash
# build-offline.sh —— 构建自包含离线安装包（前端 → 内嵌 → 静态后端 + 外部运行时依赖 + 清单 + SHA256）。
#
# 产物：
#   ${OUT_DIR}/auto_performance-offline-${VERSION}.tar.gz
#   ${OUT_DIR}/auto_performance-offline-${VERSION}.tar.gz.sha256
#
# 包内结构（对齐 architecture.md §6.2）：
#   bin/auto_performance        后端单二进制（CGO_ENABLED=0，内嵌前端 + 报告模板）
#   bin/k6                      压测引擎（外部依赖，随包分发）
#   runtime/chromium/           headless Chromium（PDF 渲染）
#   runtime/postgres/           PostgreSQL 离线运行时（本地库模式）
#   config/config.example.yaml  配置样例（install.sh 据此渲染生产配置）
#   scripts/{install,upgrade,uninstall}.sh + scripts/lib/common.sh
#   data/                       数据目录占位（安装时使用 /var/lib/auto_performance）
#   manifest.yaml               版本 / 依赖 / 逐文件 SHA256
#   VERSION / README.md
#
# 用法：
#   VERSION=0.1.0 bash deploy/offline/build-offline.sh
#   VERSION=0.1.0 bash deploy/offline/build-offline.sh --strict   # 发布构建：缺依赖即失败
#
# 环境变量：
#   VERSION              版本号（默认 0.1.0）
#   TARGET_OS/ARCH       目标平台（默认 linux/amd64，可 arm64）
#   OUT_DIR              输出目录（默认 <repo>/dist）
#   ASSETS_DIR           依赖素材目录（默认 deploy/offline/assets）
#   K6_BINARY            k6 二进制路径（覆盖素材目录）
#   CHROMIUM_DIR         Chromium 目录（需含 chrome/chromium 可执行文件）
#   POSTGRES_DIR         PostgreSQL 运行时目录（需含 bin/initdb、bin/postgres）
#   K6_DOWNLOAD_URL / CHROMIUM_DOWNLOAD_URL / POSTGRES_DOWNLOAD_URL
#   ALLOW_DOWNLOAD=1     允许在构建机联网下载缺失依赖（离线构建机请保持 0）
#   FORCE_NPM_CI=1       强制 `npm ci`（默认复用已有 node_modules 以支持内网构建）
#
# 选项：
#   --strict             缺少 k6/chromium/postgres 任一依赖即失败（发布必须使用）
#   --skip-frontend      复用已有 frontend/dist，不重新构建前端
#   -h|--help
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../scripts/lib/common.sh
source "${REPO_ROOT}/deploy/scripts/lib/common.sh"

VERSION="${VERSION:-0.1.0}"
TARGET_OS="${TARGET_OS:-linux}"
TARGET_ARCH="${TARGET_ARCH:-amd64}"
OUT_DIR="${OUT_DIR:-${REPO_ROOT}/dist}"
ASSETS_DIR="${ASSETS_DIR:-${REPO_ROOT}/deploy/offline/assets}"
ALLOW_DOWNLOAD="${ALLOW_DOWNLOAD:-0}"
FORCE_NPM_CI="${FORCE_NPM_CI:-0}"
STRICT=0
SKIP_FRONTEND=0

PKG_NAME="auto_performance-offline-${VERSION}"
PKG_DIR="${OUT_DIR}/${PKG_NAME}"
TARBALL="${OUT_DIR}/${PKG_NAME}.tar.gz"

usage() {
  sed -n '3,32p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

parse_args() {
  while (($# > 0)); do
    case "$1" in
      --strict) STRICT=1 ;;
      --skip-frontend) SKIP_FRONTEND=1 ;;
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
  [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]] \
    || die "$EXIT_USAGE" "VERSION 必须为 SemVer（当前: $VERSION）"
  [[ "$TARGET_OS" =~ ^(linux|darwin)$ ]] || die "$EXIT_USAGE" "TARGET_OS 仅支持 linux/darwin"
  [[ "$TARGET_ARCH" =~ ^(amd64|arm64)$ ]] || die "$EXIT_USAGE" "TARGET_ARCH 仅支持 amd64/arm64"
}

# ---------------------------------------------------------------------------
# 依赖准备（本地放置优先，可选下载；--strict 下缺失即失败）
# ---------------------------------------------------------------------------

download_if_allowed() {
  local url="$1" dest="$2"
  if [[ -z "$url" || "$ALLOW_DOWNLOAD" != "1" ]]; then
    return 1
  fi
  command -v curl >/dev/null 2>&1 || die "$EXIT_ENV" "ALLOW_DOWNLOAD=1 但缺少 curl"
  log_info "下载依赖: ${url}"
  curl -fL --retry 2 --connect-timeout 10 -o "$dest" "$url"
}

# 在素材目录中探测候选路径
find_asset() {
  local -a candidates=("$@") c
  for c in "${candidates[@]}"; do
    if [[ -e "$c" ]]; then
      printf '%s' "$c"
      return 0
    fi
  done
  return 1
}

stage_k6() {
  local src="" url="${K6_DOWNLOAD_URL:-}"
  if [[ -n "${K6_BINARY:-}" ]]; then
    src="$K6_BINARY"
  else
    src="$(find_asset \
      "${ASSETS_DIR}/k6/k6-${TARGET_OS}-${TARGET_ARCH}" \
      "${ASSETS_DIR}/k6/k6_${TARGET_OS}_${TARGET_ARCH}" \
      "${ASSETS_DIR}/k6/k6" \
      "${ASSETS_DIR}/bin/k6-${TARGET_OS}-${TARGET_ARCH}" \
      "${ASSETS_DIR}/bin/k6" || true)"
  fi

  if [[ -z "$src" && -n "$url" && "$ALLOW_DOWNLOAD" == "1" ]]; then
    local tmp
    tmp="$(mktemp)"
    if download_if_allowed "$url" "$tmp"; then
      src="$tmp"
    fi
  fi

  if [[ -z "$src" || ! -f "$src" ]]; then
    if [[ "$STRICT" == "1" ]]; then
      die "$EXIT_DEPS" "缺少 k6 二进制：请放置 ${ASSETS_DIR}/k6/k6-${TARGET_OS}-${TARGET_ARCH} 或设置 K6_BINARY"
    fi
    log_warn "未提供 k6 二进制（发布构建请加 --strict）；包内不含 bin/k6，压测功能不可用"
    return 1
  fi

  install -m 0755 "$src" "${PKG_DIR}/bin/k6"
  log_info "已打包 k6: $(basename "$src")"
  return 0
}

stage_chromium() {
  local src="" url="${CHROMIUM_DOWNLOAD_URL:-}"
  if [[ -n "${CHROMIUM_DIR:-}" ]]; then
    src="$CHROMIUM_DIR"
  else
    src="$(find_asset \
      "${ASSETS_DIR}/chromium/chromium-${TARGET_OS}-${TARGET_ARCH}" \
      "${ASSETS_DIR}/chromium/${TARGET_OS}-${TARGET_ARCH}" \
      "${ASSETS_DIR}/chromium" || true)"
  fi

  if [[ -z "$src" && -n "$url" && "$ALLOW_DOWNLOAD" == "1" ]]; then
    local tmpdir
    tmpdir="$(mktemp -d)"
    if download_if_allowed "$url" "${tmpdir}/chromium.tar.xz"; then
      tar -xf "${tmpdir}/chromium.tar.xz" -C "$tmpdir"
      src="$(find "$tmpdir" -maxdepth 3 -type f \( -name chrome -o -name chromium -o -name headless_shell \) | head -n1 || true)"
      src="$(dirname "${src:-$tmpdir}")"
    fi
  fi

  if [[ -z "$src" || ! -d "$src" ]]; then
    if [[ "$STRICT" == "1" ]]; then
      die "$EXIT_DEPS" "缺少 Chromium 目录：请放置 ${ASSETS_DIR}/chromium 或设置 CHROMIUM_DIR"
    fi
    log_warn "未提供 Chromium（--strict 时视为失败）；PDF 报告功能不可用（--no-pdf 可显式关闭要求）"
    return 1
  fi

  local exe
  exe="$(find "$src" -maxdepth 3 -type f \( -name chrome -o -name chromium -o -name headless_shell \) | head -n1 || true)"
  if [[ -z "$exe" ]]; then
    if [[ "$STRICT" == "1" ]]; then
      die "$EXIT_DEPS" "Chromium 目录中未找到 chrome/chromium/headless_shell: $src"
    fi
    log_warn "Chromium 目录中未找到可执行文件: $src"
    return 1
  fi

  rm -rf "${PKG_DIR}/runtime/chromium"
  cp -a "$src" "${PKG_DIR}/runtime/chromium"
  chmod -R a+rX "${PKG_DIR}/runtime/chromium"
  log_info "已打包 Chromium: $(basename "$exe")"
  return 0
}

stage_postgres() {
  local src="" url="${POSTGRES_DOWNLOAD_URL:-}"
  if [[ -n "${POSTGRES_DIR:-}" ]]; then
    src="$POSTGRES_DIR"
  else
    src="$(find_asset \
      "${ASSETS_DIR}/postgres/postgres-${TARGET_OS}-${TARGET_ARCH}" \
      "${ASSETS_DIR}/postgres/${TARGET_OS}-${TARGET_ARCH}" \
      "${ASSETS_DIR}/postgres" || true)"
  fi

  if [[ -z "$src" && -n "$url" && "$ALLOW_DOWNLOAD" == "1" ]]; then
    local tmpdir
    tmpdir="$(mktemp -d)"
    if download_if_allowed "$url" "${tmpdir}/postgres.tar.gz"; then
      tar -xzf "${tmpdir}/postgres.tar.gz" -C "$tmpdir"
      src="$(find "$tmpdir" -maxdepth 3 -type d -name bin | head -n1 || true)"
      src="$(dirname "${src:-$tmpdir}")"
    fi
  fi

  if [[ -z "$src" || ! -d "$src" ]]; then
    if [[ "$STRICT" == "1" ]]; then
      die "$EXIT_DEPS" "缺少 PostgreSQL 运行时：请放置 ${ASSETS_DIR}/postgres 或设置 POSTGRES_DIR"
    fi
    log_warn "未提供 PostgreSQL 运行时（--strict 时视为失败）；仅支持 external 数据库模式"
    return 1
  fi

  if [[ ! -f "${src}/bin/initdb" || ! -f "${src}/bin/postgres" ]]; then
    if [[ "$STRICT" == "1" ]]; then
      die "$EXIT_DEPS" "PostgreSQL 运行时缺少 bin/initdb 或 bin/postgres: $src"
    fi
    log_warn "PostgreSQL 运行时结构不完整（缺 bin/initdb 或 bin/postgres）: $src"
    return 1
  fi

  rm -rf "${PKG_DIR}/runtime/postgres"
  cp -a "$src" "${PKG_DIR}/runtime/postgres"
  chmod -R a+rX "${PKG_DIR}/runtime/postgres"
  log_info "已打包 PostgreSQL 运行时: $(basename "$src")"
  return 0
}

dep_version() {
  # dep_version <dep 目录/文件> [VERSION 文件名]
  local path="$1" vf="$2"
  if [[ -n "$vf" && -s "$vf" ]]; then
    head -n1 "$vf" | tr -d '\r\n'
    return 0
  fi
  return 0
}

# ---------------------------------------------------------------------------
# 构建
# ---------------------------------------------------------------------------

build_frontend() {
  local fe="${REPO_ROOT}/frontend"
  log_step "1/6 构建前端（Vite）"
  if [[ "$SKIP_FRONTEND" == "1" ]]; then
    [[ -f "${fe}/dist/index.html" ]] || die "$EXIT_ENV" "--skip-frontend 但缺少 ${fe}/dist/index.html"
    log_info "复用已有前端产物: ${fe}/dist"
    return 0
  fi

  if [[ -d "${fe}/node_modules" && "$FORCE_NPM_CI" != "1" ]]; then
    log_info "复用已有 node_modules（FORCE_NPM_CI=1 可强制重装）"
  else
    (cd "$fe" && npm ci --no-audit --no-fund)
  fi
  (cd "$fe" && npm run build)
  [[ -f "${fe}/dist/index.html" ]] || die "$EXIT_ENV" "前端构建未产出 dist/index.html"
  log_info "前端构建完成"
}

embed_frontend() {
  log_step "2/6 内嵌前端产物到 backend/web/dist"
  rm -rf "${REPO_ROOT}/backend/web/dist"
  mkdir -p "${REPO_ROOT}/backend/web"
  cp -r "${REPO_ROOT}/frontend/dist" "${REPO_ROOT}/backend/web/dist"
  [[ -f "${REPO_ROOT}/backend/web/dist/index.html" ]] || die "$EXIT_ENV" "内嵌前端失败"
  log_info "已复制 $(find "${REPO_ROOT}/backend/web/dist" -type f | wc -l | tr -d ' ') 个静态文件"
}

build_backend() {
  log_step "3/6 编译后端单二进制（CGO_ENABLED=0 ${TARGET_OS}/${TARGET_ARCH}）"
  (cd "${REPO_ROOT}/backend" && \
    CGO_ENABLED=0 GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" go build \
      -trimpath -buildvcs=false \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o "${PKG_DIR}/bin/auto_performance" ./cmd/server)
  [[ -s "${PKG_DIR}/bin/auto_performance" ]] || die "$EXIT_ENV" "后端编译失败"
  chmod 0755 "${PKG_DIR}/bin/auto_performance"
  log_info "编译完成: $(du -h "${PKG_DIR}/bin/auto_performance" | awk '{print $1}')"
}

assemble_package() {
  log_step "4/6 组装离线包目录"
  rm -rf "$PKG_DIR"
  mkdir -p "${PKG_DIR}"/{bin,runtime,config,scripts/lib,data}

  install -m 0755 "${REPO_ROOT}/deploy/scripts/install.sh" "${PKG_DIR}/scripts/install.sh"
  install -m 0755 "${REPO_ROOT}/deploy/scripts/upgrade.sh" "${PKG_DIR}/scripts/upgrade.sh"
  install -m 0755 "${REPO_ROOT}/deploy/scripts/uninstall.sh" "${PKG_DIR}/scripts/uninstall.sh"
  install -m 0644 "${REPO_ROOT}/deploy/scripts/lib/common.sh" "${PKG_DIR}/scripts/lib/common.sh"
  install -m 0644 "${REPO_ROOT}/backend/config/config.example.yaml" "${PKG_DIR}/config/config.example.yaml"
  install -m 0644 "${REPO_ROOT}/deploy/README.md" "${PKG_DIR}/README.md"
  printf '%s\n' "$VERSION" >"${PKG_DIR}/VERSION"
  printf '%s\n' "运行时数据目录占位；生产数据位于 /var/lib/auto_performance（安装时创建）。" >"${PKG_DIR}/data/.keep"
  log_info "已复制安装脚本、配置样例与说明文档"
}

# ---------------------------------------------------------------------------
# manifest + 完整性
# ---------------------------------------------------------------------------

generate_manifest() {
  local k6_bundled="false" chromium_bundled="false" postgres_bundled="false"
  [[ -f "${PKG_DIR}/bin/k6" ]] && k6_bundled="true"
  [[ -d "${PKG_DIR}/runtime/chromium" ]] && chromium_bundled="true"
  [[ -d "${PKG_DIR}/runtime/postgres" ]] && postgres_bundled="true"

  local k6_ver="" chromium_ver="" postgres_ver=""
  k6_ver="$(dep_version "${PKG_DIR}/bin/k6" "${PKG_DIR}/bin/k6.version")"
  chromium_ver="$(dep_version "${PKG_DIR}/runtime/chromium" "${PKG_DIR}/runtime/chromium/VERSION")"
  postgres_ver="$(dep_version "${PKG_DIR}/runtime/postgres" "${PKG_DIR}/runtime/postgres/VERSION")"

  local go_ver node_ver npm_ver host_desc built_at
  go_ver="$(go version 2>/dev/null | awk '{print $3" "$4}')"
  node_ver="$(node --version 2>/dev/null || echo unknown)"
  npm_ver="$(npm --version 2>/dev/null || echo unknown)"
  host_desc="$(uname -srm)"
  built_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

  {
    cat <<EOF
# auto_performance 离线包清单（由 deploy/offline/build-offline.sh 生成，勿手工编辑）
# 用途：install.sh / upgrade.sh 部署前逐文件校验 SHA256（DES-T-001），任一不符即中止。
manifest_version: 1
product: auto_performance
version: "${VERSION}"
built_at: "${built_at}"

build:
  target_os: "${TARGET_OS}"
  target_arch: "${TARGET_ARCH}"
  host: "${host_desc}"
  go: "${go_ver}"
  node: "${node_ver}"
  npm: "${npm_ver}"
  builder: "deploy/offline/build-offline.sh"

runtime_dependencies:
  k6:
    required: true
    bundled: ${k6_bundled}
    path: "bin/k6"
    version: "${k6_ver}"
  chromium:
    required: true
    bundled: ${chromium_bundled}
    path: "runtime/chromium"
    version: "${chromium_ver}"
    note: "PDF 报告渲染；安装时可用 --no-pdf 显式关闭要求"
  postgres:
    required: true
    bundled: ${postgres_bundled}
    path: "runtime/postgres"
    version: "${postgres_ver}"
    note: "本地数据库模式依赖；external 模式可使用目标机已安装 PostgreSQL"

files:
EOF

    local f size mode hash filelist
    filelist="$(mktemp)"
    (cd "$PKG_DIR" && find . -type f | sed 's#^\./##' | sort) > "$filelist"
    while IFS= read -r f; do
      [[ -z "$f" ]] && continue
      # 路径含空格会破坏 manifest 的 “<hash>  <path>” 校验格式，构建期直接拒绝
      if [[ "$f" == *[[:space:]]* ]]; then
        die "$EXIT_ENV" "包内路径不得包含空白字符: $f"
      fi
      case "$f" in
        manifest.yaml | manifest.sha256) continue ;;
      esac
      size="$(wc -c <"${PKG_DIR}/${f}" | tr -d ' ')"
      mode="$(stat -c '%a' "${PKG_DIR}/${f}" 2>/dev/null || stat -f '%Lp' "${PKG_DIR}/${f}" 2>/dev/null || echo 644)"
      hash="$(sha256sum "${PKG_DIR}/${f}" | awk '{print $1}')"
      cat <<EOF
  - path: "${f}"
    sha256: "${hash}"
    size: ${size}
    mode: "${mode}"
EOF
    done < "$filelist"
    rm -f "$filelist"
  } >"${PKG_DIR}/manifest.yaml"

  (cd "$PKG_DIR" && sha256sum manifest.yaml >manifest.sha256)
  log_info "已生成 manifest.yaml（$(grep -c '^  - path:' "${PKG_DIR}/manifest.yaml") 个文件）与 manifest.sha256"
}

verify_staged_package() {
  # SKIP_PREVERIFY=1：跳过逐文件二次 SHA256（构建机较慢时使用；交付前请在目标机 install.sh --check-only 复核）
  if [[ "${SKIP_PREVERIFY:-0}" == "1" ]]; then
    log_warn "SKIP_PREVERIFY=1：跳过 5/6 打包前自校验（请在目标机 --check-only 复核）"
    return 0
  fi
  log_step "5/6 打包前自校验（逐文件 SHA256）"
  verify_package_integrity "$PKG_DIR" || die "$EXIT_INTEGRITY" "自校验失败：生成的包与清单不一致"
}

package() {
  log_step "6/6 打包 tar.gz 并生成 SHA256"
  mkdir -p "$OUT_DIR"
  rm -f "$TARBALL" "${TARBALL}.sha256"
  tar -czf "$TARBALL" -C "$OUT_DIR" "$PKG_NAME"
  (cd "$OUT_DIR" && sha256sum "${PKG_NAME}.tar.gz" >"${PKG_NAME}.tar.gz.sha256")

  cat <<EOF

================ 离线包构建完成 ================
版本:      ${VERSION}
目标平台:  ${TARGET_OS}/${TARGET_ARCH}
包文件:    ${TARBALL}（$(du -h "$TARBALL" | awk '{print $1}')）
校验文件:  ${TARBALL}.sha256
校验值:    $(awk '{print $1}' "${TARBALL}.sha256")
安装:      tar -xzf $(basename "$TARBALL") && cd ${PKG_NAME} && sudo ./scripts/install.sh
注意:      发布构建请使用 --strict，确保 k6/chromium/postgres 全部随包分发。
================================================
EOF
}

main() {
  parse_args "$@"
  require_cmd sha256sum tar awk sed grep find sort install wc stat date
  log_step "构建 ${PKG_NAME}（${TARGET_OS}/${TARGET_ARCH}）"

  mkdir -p "$OUT_DIR" "${PKG_DIR}"/{bin,runtime}

  build_frontend
  embed_frontend
  assemble_package
  build_backend

  # 外部运行时依赖（k6 / chromium / postgres）：可本地放置或（联网构建机）下载
  log_step "准备外部运行时依赖"
  stage_k6 || true
  stage_chromium || true
  stage_postgres || true

  generate_manifest
  verify_staged_package
  package
  exit "$EXIT_OK"
}

main "$@"
