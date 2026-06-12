#!/bin/bash
# 完整流程测试：启动 bootstrap → 启动两个节点 → 发现 → 连接 → 发消息
set -e

BOOTSTRAP_PORT=4400
WEB_PORT=8400
NODE_A_PORT=4401
NODE_B_PORT=4402
BIN_DIR="$(cd "$(dirname "$0")/.." && pwd)/bin"
PASS=0
FAIL=0

green() { echo -e "\033[32m✓ $1\033[0m"; }
red()   { echo -e "\033[31m✗ $1\033[0m"; }

cleanup() {
    [ -n "$BOOTSTRAP_PID" ] && kill "$BOOTSTRAP_PID" 2>/dev/null
    wait 2>/dev/null
}
trap cleanup EXIT

echo "=== 完整流程测试 ==="
echo

# 构建
echo "--- 构建 ---"
go build -o "$BIN_DIR/bootstrap" ./cmd/bootstrap/ 2>/dev/null
go build -o "$BIN_DIR/np4cli" ./cmd/np4cli/ 2>/dev/null
green "构建成功"

# Step 1: 启动 Bootstrap
echo
echo "--- Step 1: 启动 Bootstrap ---"
"$BIN_DIR/bootstrap" start --port $BOOTSTRAP_PORT --web $WEB_PORT &
BOOTSTRAP_PID=$!
sleep 2

if kill -0 "$BOOTSTRAP_PID" 2>/dev/null; then
    green "Bootstrap 启动成功"
else
    red "Bootstrap 启动失败"
    exit 1
fi

BOOTSTRAP_MULTIADDR=$("$BIN_DIR/bootstrap" id --port $BOOTSTRAP_PORT 2>&1 | grep "/ip4/127.0.0.1" | head -1 | awk '{print $1}')
green "Bootstrap multiaddr: $BOOTSTRAP_MULTIADDR"

# Step 2: 获取节点 A 的 Peer ID
echo
echo "--- Step 2: 创建节点 A ---"
NODE_A_ID=$("$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" id 2>&1 | grep "Peer ID:" | awk '{print $3}')
green "节点 A Peer ID: $NODE_A_ID"

NODE_A_MULTIADDR=$("$BIN_DIR/np4cli" --port $NODE_A_PORT id 2>&1 | grep "/ip4/127.0.0.1" | head -1 | awk '{print $1}')
green "节点 A multiaddr: $NODE_A_MULTIADDR"

# Step 3: 获取节点 B 的 Peer ID
echo
echo "--- Step 3: 创建节点 B ---"
NODE_B_ID=$("$BIN_DIR/np4cli" --port $NODE_B_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" id 2>&1 | grep "Peer ID:" | awk '{print $3}')
green "节点 B Peer ID: $NODE_B_ID"

NODE_B_MULTIADDR=$("$BIN_DIR/np4cli" --port $NODE_B_PORT id 2>&1 | grep "/ip4/127.0.0.1" | head -1 | awk '{print $1}')
green "节点 B multiaddr: $NODE_B_MULTIADDR"

# Step 4: 节点 A 连接节点 B
echo
echo "--- Step 4: A 连接 B ---"
OUTPUT=$("$BIN_DIR/np4cli" --port $NODE_A_PORT connect "$NODE_B_MULTIADDR" 2>&1)
if echo "$OUTPUT" | grep -q "Connected"; then
    green "A -> B 连接成功"
    PASS=$((PASS+1))
else
    red "A -> B 连接失败: $OUTPUT"
    FAIL=$((FAIL+1))
fi

# Step 5: 节点 B 连接节点 A
echo
echo "--- Step 5: B 连接 A ---"
OUTPUT=$("$BIN_DIR/np4cli" --port $NODE_B_PORT connect "$NODE_A_MULTIADDR" 2>&1)
if echo "$OUTPUT" | grep -q "Connected"; then
    green "B -> A 连接成功"
    PASS=$((PASS+1))
else
    red "B -> A 连接失败: $OUTPUT"
    FAIL=$((FAIL+1))
fi

# Step 6: A 发送消息给 B
echo
echo "--- Step 6: A 发消息给 B ---"
OUTPUT=$("$BIN_DIR/np4cli" --port $NODE_A_PORT send "$NODE_B_ID" "你好B，我是A" 2>&1)
if echo "$OUTPUT" | grep -q "sent\|Sent"; then
    green "A -> B 消息发送成功"
    PASS=$((PASS+1))
else
    red "A -> B 发送失败: $OUTPUT"
    FAIL=$((FAIL+1))
fi

# Step 7: B 发送消息给 A
echo
echo "--- Step 7: B 发消息给 A ---"
OUTPUT=$("$BIN_DIR/np4cli" --port $NODE_B_PORT send "$NODE_A_ID" "你好A，我是B" 2>&1)
if echo "$OUTPUT" | grep -q "sent\|Sent"; then
    green "B -> A 消息发送成功"
    PASS=$((PASS+1))
else
    red "B -> A 发送失败: $OUTPUT"
    FAIL=$((FAIL+1))
fi

# Step 8: 检查 API 中的 peers
echo
echo "--- Step 8: 检查 Bootstrap API ---"
PEERS_COUNT=$(curl -s "http://localhost:$WEB_PORT/api/peers" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null)
if [ "$PEERS_COUNT" -gt 0 ] 2>/dev/null; then
    green "Bootstrap 已连接 $PEERS_COUNT 个 peer"
    PASS=$((PASS+1))
else
    red "Bootstrap 无 peer 连接"
    FAIL=$((FAIL+1))
fi

# 结果
echo
echo "========================="
echo "通过: $PASS  失败: $FAIL"
[ $FAIL -eq 0 ] && green "全部通过!" || red "有失败用例"
