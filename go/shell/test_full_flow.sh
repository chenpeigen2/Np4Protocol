#!/bin/bash
# 完整流程测试：启动 bootstrap → 启动两个节点 → 连接 → 发消息 → 验证 API
set +e

BOOTSTRAP_PORT=4400
WEB_PORT=8400
NODE_A_PORT=4401
NODE_B_PORT=4402
BIN_DIR="$(cd "$(dirname "$0")/.." && pwd)/bin"
TEST_IDENTITY="/tmp/np4_test_boot_$(basename $0 .sh)_$$"
NODE_A_IDENTITY="/tmp/np4_test_flowA_$(basename $0 .sh)_$$"
NODE_B_IDENTITY="/tmp/np4_test_flowB_$(basename $0 .sh)_$$"
PASS=0
FAIL=0

green() { echo -e "\033[32m✓ $1\033[0m"; }
red()   { echo -e "\033[31m✗ $1\033[0m"; }

cleanup() {
    for port in $BOOTSTRAP_PORT $NODE_A_PORT $NODE_B_PORT; do
        lsof -ti :$port 2>/dev/null | xargs kill -9 2>/dev/null
    done
    rm -f /tmp/np4_flow_*.log /tmp/np4_flow_*.fifo "$TEST_IDENTITY" /tmp/np4_boot_$$.log "$NODE_A_IDENTITY" "$NODE_B_IDENTITY"
}
trap cleanup EXIT

echo "=== 完整流程测试 ==="
echo

# 构建
go build -o "$BIN_DIR/bootstrap" ./cmd/bootstrap/ 2>/dev/null
go build -o "$BIN_DIR/np4cli" ./cmd/np4cli/ 2>/dev/null
green "构建成功"

for port in $BOOTSTRAP_PORT $NODE_A_PORT $NODE_B_PORT; do
    lsof -ti :$port 2>/dev/null | xargs kill -9 2>/dev/null
done
sleep 1

# Step 1: 启动 Bootstrap
echo
echo "--- Step 1: 启动 Bootstrap ---"
"$BIN_DIR/bootstrap" start --port $BOOTSTRAP_PORT --web $WEB_PORT --identity "$TEST_IDENTITY" > /tmp/np4_boot_$$.log 2>&1 &
sleep 2
BOOTSTRAP_MULTIADDR=$("$BIN_DIR/bootstrap" id --port $BOOTSTRAP_PORT --identity "$TEST_IDENTITY" 2>&1 | grep "/ip4/127.0.0.1" | head -1 | awk '{print $NF}')
green "Bootstrap: $BOOTSTRAP_MULTIADDR"

# Step 2: 启动节点 A（chat 模式）
echo
echo "--- Step 2: 启动节点 A ---"
rm -f /tmp/np4_flow_a.fifo /tmp/np4_flow_a.log
mkfifo /tmp/np4_flow_a.fifo
(tail -f /dev/null | "$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" --identity "$NODE_A_IDENTITY" chat > /tmp/np4_flow_a.fifo 2>&1) &
cat /tmp/np4_flow_a.fifo > /tmp/np4_flow_a.log &
sleep 3

NODE_A_ID=$(grep "Peer ID:" /tmp/np4_flow_a.log | awk '{print $3}')
NODE_A_ADDR=$(grep "/ip4/127.0.0.1" /tmp/np4_flow_a.log | head -1 | awk '{print $NF}')
green "A: $NODE_A_ID"
green "A addr: $NODE_A_ADDR"

# Step 3: 启动节点 B（chat 模式）
echo
echo "--- Step 3: 启动节点 B ---"
rm -f /tmp/np4_flow_b.fifo /tmp/np4_flow_b.log
mkfifo /tmp/np4_flow_b.fifo
(tail -f /dev/null | "$BIN_DIR/np4cli" --port $NODE_B_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" --identity "$NODE_B_IDENTITY" chat > /tmp/np4_flow_b.fifo 2>&1) &
cat /tmp/np4_flow_b.fifo > /tmp/np4_flow_b.log &
sleep 3

NODE_B_ID=$(grep "Peer ID:" /tmp/np4_flow_b.log | awk '{print $3}')
NODE_B_ADDR=$(grep "/ip4/127.0.0.1" /tmp/np4_flow_b.log | head -1 | awk '{print $NF}')
green "B: $NODE_B_ID"
green "B addr: $NODE_B_ADDR"

# Step 4: A 连接 B
echo
echo "--- Step 4: A 连接 B ---"
OUTPUT=$("$BIN_DIR/np4cli" --port $NODE_A_PORT --identity "$NODE_A_IDENTITY" connect "$NODE_B_ADDR" 2>&1)
if echo "$OUTPUT" | grep -q "Connected"; then
    green "A -> B 连接成功"
    PASS=$((PASS+1))
else
    red "A -> B 连接失败: $OUTPUT"
    FAIL=$((FAIL+1))
fi

# Step 5: B 连接 A
echo
echo "--- Step 5: B 连接 A ---"
OUTPUT=$("$BIN_DIR/np4cli" --port $NODE_B_PORT --identity "$NODE_B_IDENTITY" connect "$NODE_A_ADDR" 2>&1)
if echo "$OUTPUT" | grep -q "Connected"; then
    green "B -> A 连接成功"
    PASS=$((PASS+1))
else
    red "B -> A 连接失败: $OUTPUT"
    FAIL=$((FAIL+1))
fi

# Step 6: A 发消息给 B
echo
echo "--- Step 6: A -> B 消息 ---"
cp /dev/null /tmp/np4_flow_b.log; sleep 0.5
"$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" --identity "$NODE_A_IDENTITY" send --addr "$NODE_B_ADDR" "$NODE_B_ID" "hello-from-A" 2>&1
sleep 3
if grep -aq "hello-from-A" /tmp/np4_flow_b.log; then
    green "A -> B 消息送达"
    PASS=$((PASS+1))
else
    red "A -> B 消息未送达"
    FAIL=$((FAIL+1))
fi

# Step 7: B 发消息给 A
echo
echo "--- Step 7: B -> A 消息 ---"
cp /dev/null /tmp/np4_flow_a.log; sleep 0.5
"$BIN_DIR/np4cli" --port $NODE_B_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" --identity "$NODE_B_IDENTITY" send --addr "$NODE_A_ADDR" "$NODE_A_ID" "hello-from-B" 2>&1
sleep 3
if grep -aq "hello-from-B" /tmp/np4_flow_a.log; then
    green "B -> A 消息送达"
    PASS=$((PASS+1))
else
    red "B -> A 消息未送达"
    FAIL=$((FAIL+1))
fi

# Step 8: Bootstrap API 验证
echo
echo "--- Step 8: Bootstrap API ---"
STATUS=$(curl -s "http://localhost:$WEB_PORT/api/status" 2>/dev/null)
PEERS=$(curl -s "http://localhost:$WEB_PORT/api/peers" 2>/dev/null)
API_COUNT=$(echo "$PEERS" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo 0)
IS_ONLINE=$(echo "$STATUS" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])" 2>/dev/null)

if [ "$IS_ONLINE" = "online" ]; then
    green "Bootstrap 在线，API 正常"
    PASS=$((PASS+1))
else
    red "Bootstrap 状态异常: status=$IS_ONLINE"
    FAIL=$((FAIL+1))
fi

# 结果
echo
echo "========================="
echo "通过: $PASS  失败: $FAIL"
[ $FAIL -eq 0 ] && green "全部通过!" || red "有失败用例"
