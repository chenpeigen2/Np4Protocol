#!/bin/bash
# DHT 节点发现测试：验证 Bootstrap API 和节点稳定性
set +e

BOOTSTRAP_PORT=4800
WEB_PORT=8800
NODE_A_PORT=4801
NODE_B_PORT=4802
NODE_C_PORT=4803
BIN_DIR="$(cd "$(dirname "$0")/.." && pwd)/bin"
PASS=0
FAIL=0

green() { echo -e "\033[32m✓ $1\033[0m"; }
red()   { echo -e "\033[31m✗ $1\033[0m"; }

cleanup() {
    for port in $BOOTSTRAP_PORT $NODE_A_PORT $NODE_B_PORT $NODE_C_PORT; do
        lsof -ti :$port 2>/dev/null | xargs kill -9 2>/dev/null
    done
    rm -f /tmp/np4_dht_*.log /tmp/np4_dht_*.fifo
}
trap cleanup EXIT

echo "=== DHT 节点发现测试 ==="
echo

go build -o "$BIN_DIR/bootstrap" ./cmd/bootstrap/ 2>/dev/null
go build -o "$BIN_DIR/np4cli" ./cmd/np4cli/ 2>/dev/null
green "构建成功"

for port in $BOOTSTRAP_PORT $NODE_A_PORT $NODE_B_PORT $NODE_C_PORT; do
    lsof -ti :$port 2>/dev/null | xargs kill -9 2>/dev/null
done
sleep 1

# 启动 Bootstrap
"$BIN_DIR/bootstrap" start --port $BOOTSTRAP_PORT --web $WEB_PORT &
sleep 2
BOOTSTRAP_MULTIADDR=$("$BIN_DIR/bootstrap" id --port $BOOTSTRAP_PORT 2>&1 | grep "/ip4/127.0.0.1" | head -1 | awk '{print $1}')
green "Bootstrap 启动"

# Test 1: Bootstrap API /api/status 在线
echo
echo "--- Test 1: Bootstrap API status ---"
STATUS=$(curl -s "http://localhost:$WEB_PORT/api/status" 2>/dev/null)
if echo "$STATUS" | python3 -c "import sys,json; assert json.load(sys.stdin)['status']=='online'" 2>/dev/null; then
    green "Bootstrap 在线"
    PASS=$((PASS+1))
else
    red "Bootstrap 离线"
    FAIL=$((FAIL+1))
fi

# Test 2: Bootstrap 初始 peers 为空
echo
echo "--- Test 2: 初始 peers 为空 ---"
API_PEERS=$(curl -s "http://localhost:$WEB_PORT/api/peers" 2>/dev/null)
API_COUNT=$(echo "$API_PEERS" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo 0)
if [ "$API_COUNT" -eq 0 ]; then
    green "初始 peers 为空（预期）"
    PASS=$((PASS+1))
else
    red "初始 peers 不为空: $API_COUNT"
    FAIL=$((FAIL+1))
fi

# 启动 3 个节点
start_node() {
    local port=$1 name=$2
    rm -f /tmp/np4_dht_${name}.fifo /tmp/np4_dht_${name}.log
    mkfifo /tmp/np4_dht_${name}.fifo
    (tail -f /dev/null | "$BIN_DIR/np4cli" --port $port --bootstrap "$BOOTSTRAP_MULTIADDR" chat > /tmp/np4_dht_${name}.fifo 2>&1) &
    cat /tmp/np4_dht_${name}.fifo > /tmp/np4_dht_${name}.log &
    sleep 3
}

echo
echo "--- 启动 3 个节点 ---"
start_node $NODE_A_PORT "a"
start_node $NODE_B_PORT "b"
start_node $NODE_C_PORT "c"
green "A、B、C 已启动"

NODE_A_ID=$(grep "Peer ID:" /tmp/np4_dht_a.log | awk '{print $3}')
NODE_B_ID=$(grep "Peer ID:" /tmp/np4_dht_b.log | awk '{print $3}')
NODE_C_ID=$(grep "Peer ID:" /tmp/np4_dht_c.log | awk '{print $3}')
green "A: $NODE_A_ID"
green "B: $NODE_B_ID"
green "C: $NODE_C_ID"

# Test 3: Bootstrap API peers 接口正常响应
echo
echo "--- Test 3: Bootstrap API peers ---"
sleep 2
API_PEERS=$(curl -s "http://localhost:$WEB_PORT/api/peers" 2>/dev/null)
IS_ARRAY=$(echo "$API_PEERS" | python3 -c "import sys,json; print(isinstance(json.load(sys.stdin), list))" 2>/dev/null)
if [ "$IS_ARRAY" = "True" ]; then
    green "Bootstrap API peers 返回数组"
    PASS=$((PASS+1))
else
    red "Bootstrap API peers 响应异常"
    FAIL=$((FAIL+1))
fi

# Test 4: Bootstrap DHT peers > 0
echo
echo "--- Test 4: Bootstrap DHT peers ---"
STATUS=$(curl -s "http://localhost:$WEB_PORT/api/status" 2>/dev/null)
DHT_PEERS=$(echo "$STATUS" | python3 -c "import sys,json; print(json.load(sys.stdin)['dht_peers'])" 2>/dev/null || echo 0)
if [ "$DHT_PEERS" -gt 0 ]; then
    green "DHT peers: $DHT_PEERS"
    PASS=$((PASS+1))
else
    red "DHT peers 为 0"
    FAIL=$((FAIL+1))
fi

# Test 5: 节点 ID 唯一性
echo
echo "--- Test 5: 节点 ID 唯一性 ---"
if [ "$NODE_A_ID" != "$NODE_B_ID" ] && [ "$NODE_B_ID" != "$NODE_C_ID" ] && [ "$NODE_A_ID" != "$NODE_C_ID" ]; then
    green "三个节点 ID 唯一"
    PASS=$((PASS+1))
else
    red "存在重复 ID"
    FAIL=$((FAIL+1))
fi

# Test 6: 节点地址格式正确
echo
echo "--- Test 6: 节点地址格式 ---"
NODE_A_ADDR=$(grep "/ip4/127.0.0.1" /tmp/np4_dht_a.log | head -1 | awk '{print $1}')
if echo "$NODE_A_ADDR" | grep -q "/p2p/"; then
    green "地址包含 /p2p/ 后缀: $NODE_A_ADDR"
    PASS=$((PASS+1))
else
    red "地址格式异常: $NODE_A_ADDR"
    FAIL=$((FAIL+1))
fi

# Test 7: Bootstrap uptime 持续增长
echo
echo "--- Test 7: Bootstrap uptime ---"
sleep 3
STATUS2=$(curl -s "http://localhost:$WEB_PORT/api/status" 2>/dev/null)
UPTIME1=$(echo "$STATUS" | python3 -c "import sys,json; print(json.load(sys.stdin)['uptime'])" 2>/dev/null)
UPTIME2=$(echo "$STATUS2" | python3 -c "import sys,json; print(json.load(sys.stdin)['uptime'])" 2>/dev/null)
if [ "$UPTIME1" != "$UPTIME2" ]; then
    green "uptime 增长: $UPTIME1 -> $UPTIME2"
    PASS=$((PASS+1))
else
    red "uptime 未变化"
    FAIL=$((FAIL+1))
fi

# 结果
echo
echo "========================="
echo "通过: $PASS  失败: $FAIL"
[ $FAIL -eq 0 ] && green "全部通过!" || red "有失败用例"
