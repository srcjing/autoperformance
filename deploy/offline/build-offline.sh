#!/usr/bin/env bash
# 离线打包脚本：构建前端 + 编译后端（CGO_ENABLED=0 静态编译，内嵌前端）+ 打包 tar.gz + 生成 SHA256。
#
# 用法：VERSION=0.1.0 ./deploy/offline/build-offline.sh
# 环境：Go 1.26+ / Node 20+ / npm
set -euo pipefail

# ===== 配置 =====
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-0.1.0}"
TARGET_OS="${TARGET_OS:-linux}"
TARGET_ARCH="${TARGET_ARCH:-amd64}"           # 可扩展 arm64
OUT_DIR="${OUT_DIR:-${REPO_ROOT}/dist}"
PKG_NAME="auto_performance-offline-${VERSION}"
PKG_DIR="${OUT_DIR}/${PKG_NAME}"

echo "==> 离线打包 ${PKG_NAME} (${TARGET_OS}/${TARGET_ARCH})"
rm -rf "${PKG_DIR}"
mkdir -p "${PKG_DIR}"/{bin,runtime,config,scripts,data}

echo "==> 1. 构建前端"
(cd "${REPO_ROOT}/frontend" && npm ci && npm run build)

echo "==> 2. 内嵌前端产物到 backend/web/dist（go:embed 随编译内嵌进二进制）"
rm -rf "${REPO_ROOT}/backend/web/dist"
cp -r "${REPO_ROOT}/frontend/dist" "${REPO_ROOT}/backend/web/dist"

echo "==> 3. 编译后端（CGO_ENABLED=0 静态编译，内嵌前端 + 报告模板）"
(cd "${REPO_ROOT}/backend" && \
  CGO_ENABLED=0 GOOS="${TARGET_OS}" GOARCH="${TARGET_ARCH}" go build \
    -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o "${PKG_DIR}/bin/auto_performance" ./cmd/server)

echo "==> 4. 复制配置与运维脚本"
cp "${REPO_ROOT}/backend/config/config.example.yaml" "${PKG_DIR}/config/config.yaml"
cp "${REPO_ROOT}/deploy/scripts/install.sh" "${PKG_DIR}/scripts/install.sh"
cp "${REPO_ROOT}/deploy/scripts/upgrade.sh" "${PKG_DIR}/scripts/upgrade.sh"
cp "${REPO_ROOT}/deploy/scripts/uninstall.sh" "${PKG_DIR}/scripts/uninstall.sh"
cp "${REPO_ROOT}/deploy/offline/manifest.txt" "${PKG_DIR}/manifest.txt"
chmod +x "${PKG_DIR}/scripts/"*.sh

# 依赖二进制占位（k6 / chromium / postgres 后续随包分发，见 manifest.txt）
touch "${PKG_DIR}/runtime/.gitkeep"

echo "==> 5. 打包 tar.gz"
tar -czf "${OUT_DIR}/${PKG_NAME}.tar.gz" -C "${OUT_DIR}" "${PKG_NAME}"

echo "==> 6. 生成 SHA256"
(cd "${OUT_DIR}" && sha256sum "${PKG_NAME}.tar.gz" > "${PKG_NAME}.tar.gz.sha256")

echo "==> 完成：${OUT_DIR}/${PKG_NAME}.tar.gz"
