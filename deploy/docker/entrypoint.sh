#!/bin/sh
# docker-entrypoint.sh —— 容器入口：确保 KEK 存在（0600）、执行数据库迁移、再启动服务。
#
# 安全要点：
#   - KEK 首次启动时用 /dev/urandom 生成 32 字节并以 0600 落盘（DES-I-002）；
#   - KEK 已存在时**不覆盖**（覆盖会导致历史密文不可解密）；
#   - 迁移失败最多重试 30 次（等待数据库就绪），仍失败则以退出码 20 终止，避免以旧 schema 起动；
#   - 不拼接 shell 命令执行用户输入，仅做固定子命令调用。
set -eu

KEK="${AUTO_PERF_SECURITY_KEK_PATH:-/var/lib/auto_performance/keys/kek.key}"
KEK_DIR="$(dirname "$KEK")"
BIN="/usr/local/bin/auto_performance"

mkdir -p "$KEK_DIR" 2>/dev/null || true
chmod 700 "$KEK_DIR" 2>/dev/null || true
if [ ! -s "$KEK" ]; then
  umask 077
  od -An -tx1 -N32 /dev/urandom | tr -d ' \n' >"$KEK"
  chmod 600 "$KEK"
  echo "[entrypoint] 已生成 KEK: $KEK（0600，32 字节）"
else
  chmod 600 "$KEK" 2>/dev/null || true
fi

# 解析 -config/--config 参数（迁移需要同一份配置）
CFG=""
PREV=""
for ARG in "$@"; do
  case "$ARG" in
    -config=* | --config=*) CFG="${ARG#*=}" ;;
  esac
  if [ "$PREV" = "-config" ] || [ "$PREV" = "--config" ]; then
    CFG="$ARG"
  fi
  PREV="$ARG"
done

if [ "${AUTO_PERF_SKIP_MIGRATE:-0}" != "1" ] && [ -n "$CFG" ]; then
  i=0
  while ! "$BIN" -config "$CFG" migrate up; do
    i=$((i + 1))
    if [ "$i" -ge 30 ]; then
      echo "[entrypoint] migrate up 连续失败 ${i} 次，终止启动（退出码 20）" >&2
      exit 20
    fi
    echo "[entrypoint] migrate up 失败，2s 后重试（${i}/30）" >&2
    sleep 2
  done
fi

exec "$BIN" "$@"
