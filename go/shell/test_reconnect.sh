#!/bin/bash
# 重连测试：节点断开后重新连接
set +e

BOOTSTRAP_PORT=4900
WEB_PORT=8900
NODE_A_PORT=4901
NODE_B_PORT=4902
BIN_DIR="$(cd "$(dirname "$0")/.." && pwd)/bin"
PASS=0
FAIL=0

green() { echo -e "\033[32m✓ $1\033[0m"; }
red()   { echo -e "\033[31m✗ $1\033[0m"; }

cleanup() {
    for port in $BOOTSTRAP_PORT $NODE_A_PORT $NODE_B_PORT; do
        lsof -ti :$port 2>/dev/null | xargs kill -9 2>/dev/null
    done
    rm -f /tmp/np4_reconnect_*.log /tmp/np4_reconnect_*.fifo
}
trap cleanup EXIT

echo "=== 重连测试 ==="
echo

go build -o "$BIN_DIR/bootstrap" ./cmd/bootstrap/ 2>/dev/null
go build -o "$BIN_DIR/np4cli" ./cmd/np4cli/ 2>/dev/null
green "构建成功"

for port in $BOOTSTRAP_PORT $NODE_A_PORT $NODE_B_PORT; do
    lsof -ti :$port 2>/dev/null | xargs kill -9 2>/dev/null
done
sleep 1

# 启动 Bootstrap
"$BIN_DIR/bootstrap" start --port $BOOTSTRAP_PORT --web $WEB_PORT &
sleep 2
BOOTSTRAP_MULTIADDR=$("$BIN_DIR/bootstrap" id --port $BOOTSTRAP_PORT 2>&1 | grep "/ip4/127.0.0.1" | head -1 | awk '{print $1}')

# 启动节点 B（长期运行）
rm -f /tmp/np4_reconnect_b.fifo /tmp/np4_reconnect_b.log
mkfifo /tmp/np4_reconnect_b.fifo
(tail -f /dev/null | "$BIN_DIR/np4cli" --port $NODE_B_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" chat > /tmp/np4_reconnect_b.fifo 2>&1) &
cat /tmp/np4_reconnect_b.fifo > /tmp/np4_reconnect_b.log &
sleep 3

NODE_B_ID=$(grep "Peer ID:" /tmp/np4_reconnect_b.log | awk '{print $3}')
NODE_B_ADDR=$(grep "/ip4/127.0.0.1" /tmp/np4_reconnect_b.log | head -1 | awk '{print $1}')
green "B: $NODE_B_ID"

# Test 1: 第一次连接和通信
echo
echo "--- Test 1: 第一次连接 ---"
"$BIN_DIR/np4cli" --port $NODE_A_PORT connect "$NODE_B_ADDR" 2>&1
cp /dev/null /tmp/np4_reconnect_b.log; sleep 0.5
"$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" send --addr "$NODE_B_ADDR" "$NODE_B_ID" "msg-1-first" 2>&1
sleep 3
if grep -aq "msg-1-first" /tmp/np4_reconnect_b.log; then
    green "第一次通信成功"
    PASS=$((PASS+1))
else
    red "第一次通信失败"
    FAIL=$((FAIL+1))
fi

# Test 2: 第二次连接（同端口，新节点）
echo
echo "--- Test 2: 第二次连接 ---"
"$BIN_DIR/np4cli" --port $NODE_A_PORT connect "$NODE_B_ADDR" 2>&1
cp /dev/null /tmp/np4_reconnect_b.log; sleep 0.5
"$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" send --addr "$NODE_B_ADDR" "$NODE_B_ID" "msg-2-second" 2>&1
sleep 3
if grep -aq "msg-2-second" /tmp/np4_reconnect_b.log; then
    green "第二次通信成功"
    PASS=$((PASS+1))
else
    red "第二次通信失败"
    FAIL=$((FAIL+1))
fi

# Test 3: 第三次连接
echo
echo "--- Test 3: 第三次连接 ---"
"$BIN_DIR/np4cli" --port $NODE_A_PORT connect "$NODE_B_ADDR" 2>&1
cp /dev/null /tmp/np4_reconnect_b.log; sleep 0.5
"$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" send --addr "$NODE_B_ADDR" "$NODE_B_ID" "msg-3-third" 2>&1
sleep 3
if grep -aq "msg-3-third" /tmp/np4_reconnect_b.log; then
    green "第三次通信成功"
    PASS=$((PASS+1))
else
    red "第三次通信失败"
    FAIL=$((FAIL+1))
fi

# Test 4: 快速连续连接（不等待）
echo
echo "--- Test 4: 快速连续 5 次连接 ---"
ALL_OK=true
for i in $(seq 1 5); do
    "$BIN_DIR/np4cli" --port $NODE_A_PORT connect "$NODE_B_ADDR" 2>&1
done
cp /dev/null /tmp/np4_reconnect_b.log; sleep 0.5
"$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" send --addr "$NODE_B_ADDR" "$NODE_B_ID" "msg-after-rapid" 2>&1
sleep 3
if grep -aq "msg-after-rapid" /tmp/np4_reconnect_b.log; then
    green "快速连接后通信成功"
    PASS=$((PASS+1))
else
    red "快速连接后通信失败"
    FAIL=$((FAIL+1))
fi

# Test 5: Bootstrap 仍然在线
echo
echo "--- Test 5: Bootstrap 稳定性 ---"
STATUS=$(curl -s "http://localhost:$WEB_PORT/api/status" 2>/dev/null)
if echo "$STATUS" | python3 -c "import sys,json; assert json.load(sys.stdin)['status']=='online'" 2>/dev/null; then
    green "Bootstrap 仍然在线"
    PASS=$((PASS+1))
else
    red "Bootstrap 离线"
    FAIL=$((FAIL+1))
fi

# 结果
echo
echo "========================="
echo "通过: $PASS  失败: $FAIL"
[ $FAIL -eq 0 ] && green "全部通过!" || red "有失败用例"
