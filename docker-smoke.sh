#!/usr/bin/env bash
# ============================================================================
# Docker 持久化冒烟测试：验证「重建容器（不删除存储卷）后数据不丢」。
#
# 场景对应真实事故：容器重建后账号/配置等数据异常丢失。
# 本脚本在部署机上即可运行：
#   ./docker-smoke.sh            # 使用 docker build 构建镜像后测试
#
# 流程：构建镜像 → 带卷启动 → 写入数据 → 强制删除容器 → 同卷重建 → 校验数据仍在。
# ============================================================================
set -euo pipefail

IMAGE=${SMOKE_IMAGE:-cline2api-smoke}
PORT=${SMOKE_PORT:-39999}
NAME=cline2api-smoke
WORK=${SMOKE_WORKDIR:-"$(mktemp -d)"}

say()  { printf '\n\033[1;36m==> %s\033[0m\n' "$*"; }
pass() { printf '\033[1;32mPASS: %s\033[0m\n' "$*"; }
fail() { printf '\033[1;31mFAIL: %s\033[0m\n' "$*"; exit 1; }

cleanup() {
  docker rm -f "$NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

say "1/6 构建镜像 $IMAGE"
docker build -t "$IMAGE" .

say "2/6 准备数据目录 $WORK/data 并预置账号数据"
mkdir -p "$WORK/data"
cat > "$WORK/data/.cline-accounts.json" <<'EOF'
{
  "accounts": [
    {"accountId": "smoke-1", "email": "smoke@example.com", "refreshToken": "rt-smoke", "status": "active"}
  ],
  "keys": ["sk-smoke-key"]
}
EOF

say "3/6 启动容器（卷挂载 $WORK/data:/app/data，端口 $PORT）"
docker run -d --name "$NAME" -p "$PORT:3457" \
  -v "$WORK/data:/app/data" \
  "$IMAGE" >/dev/null

wait_health() {
  for _ in $(seq 1 60); do
    if curl -sf -m 2 "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  return 1
}
wait_health || { docker logs "$NAME"; fail "容器 1 健康检查超时"; }

say "4/6 写入数据：生成一个 API Key"
NEW_KEY=$(curl -sf -X POST "http://127.0.0.1:$PORT/admin/api/keys/generate" | sed -n 's/.*"key":"\([^"]*\)".*/\1/p')
[ -n "$NEW_KEY" ] || fail "生成 API Key 失败"
echo "    新 Key: $NEW_KEY"

say "5/6 强制删除容器（模拟重建容器，保留存储卷）"
docker rm -f "$NAME" >/dev/null
docker run -d --name "$NAME" -p "$PORT:3457" \
  -v "$WORK/data:/app/data" \
  "$IMAGE" >/dev/null
wait_health || { docker logs "$NAME"; fail "容器 2（重建后）健康检查超时"; }

say "6/6 校验重建后的数据"
KEYS=$(curl -sf "http://127.0.0.1:$PORT/admin/api/keys")
echo "    keys: $KEYS"
echo "$KEYS" | grep -q "sk-smoke-key"  || fail "预置 Key 丢失"
echo "$KEYS" | grep -q "$NEW_KEY"      || fail "重建前生成的 Key 丢失"
pass "预置数据与重建前写入的数据在重建容器后全部保留"

docker rm -f "$NAME" >/dev/null
trap - EXIT
pass "Docker 持久化冒烟测试全部通过（数据目录: $WORK/data）"
