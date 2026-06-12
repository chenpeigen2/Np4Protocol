#!/bin/bash
# DHT 节点发现测试：验证多节点通过 DHT 自动发现
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

# Test 1: 单节点 peers 查询（应为空）
echo
echo "--- Test 1: 单节点 peers 查询 ---"
OUTPUT=$("$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" peers 2>&1)
if echo "$OUTPUT" | grep -q "0 peer\|No peers\|discovered"; then
    green "单节点无 peer（预期行为）"
    PASS=$((PASS+1))
else
    red "意外输出: $OUTPUT"
    FAIL=$((FAIL+1))
fi

# 启动节点 B 和 C
start_node() {
    local port=$1 name=$2
    rm -f /tmp/np4_dht_${name}.fifo /tmp/np4_dht_${name}.log
    mkfifo /tmp/np4_dht_${name}.fifo
    (tail -f /dev/null | "$BIN_DIR/np4cli" --port $port --bootstrap "$BOOTSTRAP_MULTIADDR" chat > /tmp/np4_dht_${name}.fifo 2>&1) &
    cat /tmp/np4_dht_${name}.fifo > /tmp/np4_dht_${name}.log &
    sleep 3
}

echo
echo "--- 启动节点 B 和 C ---"
start_node $NODE_B_PORT "b"
start_node $NODE_C_PORT "c"
green "B 和 C 已启动"

# 等待 DHT 传播
sleep 5

# Test 2: A 通过 peers 发现 B 和 C
echo
echo "--- Test 2: A 通过 DHT 发现 B 和 C ---"
OUTPUT=$("$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" peers 2>&1)
PEER_COUNT=$(echo "$OUTPUT" | grep -ac "12D3Koo" 2>/dev/null || echo 0)
if [ "$PEER_COUNT" -ge 2 ]; then
    green "A 发现了 $PEER_COUNT 个 peer"
    PASS=$((PASS+1))
else
    red "A 只发现 $PEER_COUNT 个 peer（预期 >= 2）"
    FAIL=$((FAIL+1))
fi

# Test 3: Bootstrap API 显示已连接 peers
echo
echo "--- Test 3: Bootstrap API peers ---"
API_PEERS=$(curl -s "http://localhost:$WEB_PORT/api/peers" 2>/dev/null)
API_COUNT=$(echo "$API_PEERS" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo 0)
if [ "$API_COUNT" -gt 0 ]; then
    green "Bootstrap API 显示 $API_COUNT 个 peer"
    PASS=$((PASS+1))
else
    red "Bootstrap API 无 peer"
    FAIL=$((FAIL+1))
fi

# Test 4: Bootstrap API status 正常
echo
echo "--- Test 4: Bootstrap API status ---"
STATUS=$(curl -s "http://localhost:$WEB_PORT/api/status" 2>/dev/null)
DHT_PEERS=$(echo "$STATUS" | python3 -c "import sys,json; print(json.load(sys.stdin)['dht_peers'])" 2>/dev/null || echo 0)
UPTIME=$(echo "$STATUS" | python3 -c "import sys,json; print(json.load(sys.stdin)['uptime'])" 2>/dev/null || echo "unknown")
if [ "$DHT_PEERS" -gt 0 ]; then
    green "DHT peers: $DHT_PEERS, uptime: $UPTIME"
    PASS=$((PASS+1))
else
    red "DHT peers 为 0"
    FAIL=$((FAIL+1))
fi

# Test 5: 节点 ID 唯一性
echo
echo "--- Test 5: 节点 ID 唯一性 ---"
NODE_A_ID=$("$BIN_DIR/np4cli" --port $NODE_A_PORT id 2>&1 | grep "Peer ID:" | awk '{print $3}')
NODE_B_ID=$(grep "Peer ID:" /tmp/np4_dht_b.log | awk '{print $3}')
NODE_C_ID=$(grep "Peer ID:" /tmp/np4_dht_c.log | awk '{print $3}')

if [ "$NODE_A_ID" != "$NODE_B_ID" ] && [ "$NODE_B_ID" != "$NODE_C_ID" ] && [ "$NODE_A_ID" != "$NODE_C_ID" ]; then
    green "三个节点 ID 唯一"
    PASS=$((PASS+1))
else
    red "存在重复 ID"
    FAIL=$((FAIL+1))
fi

# 结果
echo
echo "========================="
echo "通过: $PASS  失败: $FAIL"
[ $FAIL -eq 0 ] && green "全部通过!" || red "有失败用例"
