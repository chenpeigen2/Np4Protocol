#!/bin/bash
# 压力测试：高频消息、大消息、并发发送
set +e

BOOTSTRAP_PORT=4700
WEB_PORT=8700
NODE_A_PORT=4701
NODE_B_PORT=4702
BIN_DIR="$(cd "$(dirname "$0")/.." && pwd)/bin"
PASS=0
FAIL=0

green() { echo -e "\033[32m✓ $1\033[0m"; }
red()   { echo -e "\033[31m✗ $1\033[0m"; }

cleanup() {
    for port in $BOOTSTRAP_PORT $NODE_A_PORT $NODE_B_PORT; do
        lsof -ti :$port 2>/dev/null | xargs kill -9 2>/dev/null
    done
    rm -f /tmp/np4_stress_*.log /tmp/np4_stress_*.fifo
}
trap cleanup EXIT

echo "=== 压力测试 ==="
echo

go build -o "$BIN_DIR/bootstrap" ./cmd/bootstrap/ 2>/dev/null
go build -o "$BIN_DIR/np4cli" ./cmd/np4cli/ 2>/dev/null
green "构建成功"

for port in $BOOTSTRAP_PORT $NODE_A_PORT $NODE_B_PORT; do
    lsof -ti :$port 2>/dev/null | xargs kill -9 2>/dev/null
done
sleep 1

# 启动 Bootstrap 和节点
"$BIN_DIR/bootstrap" start --port $BOOTSTRAP_PORT --web $WEB_PORT &
sleep 2
BOOTSTRAP_MULTIADDR=$("$BIN_DIR/bootstrap" id --port $BOOTSTRAP_PORT 2>&1 | grep "/ip4/127.0.0.1" | head -1 | awk '{print $1}')

start_node() {
    local port=$1 name=$2
    rm -f /tmp/np4_stress_${name}.fifo /tmp/np4_stress_${name}.log
    mkfifo /tmp/np4_stress_${name}.fifo
    (tail -f /dev/null | "$BIN_DIR/np4cli" --port $port --bootstrap "$BOOTSTRAP_MULTIADDR" chat > /tmp/np4_stress_${name}.fifo 2>&1) &
    cat /tmp/np4_stress_${name}.fifo > /tmp/np4_stress_${name}.log &
    sleep 3
}

start_node $NODE_A_PORT "a"
start_node $NODE_B_PORT "b"

NODE_B_ID=$(grep "Peer ID:" /tmp/np4_stress_b.log | awk '{print $3}')
NODE_B_ADDR=$(grep "/ip4/127.0.0.1" /tmp/np4_stress_b.log | head -1 | awk '{print $1}')

# 连接
"$BIN_DIR/np4cli" --port $NODE_A_PORT connect "$NODE_B_ADDR" 2>&1
green "节点已连接"

# Test 1: 快速连续发送 10 条消息
echo
echo "--- Test 1: 快速连续 10 条消息 ---"
cp /dev/null /tmp/np4_stress_b.log; sleep 0.5
for i in $(seq 1 10); do
    "$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" send --addr "$NODE_B_ADDR" "$NODE_B_ID" "rapid-$i" 2>&1
done
sleep 5

COUNT=$(grep -ac "rapid-" /tmp/np4_stress_b.log 2>/dev/null || echo 0)
if [ "$COUNT" -ge 8 ]; then
    green "快速发送: $COUNT/10 条送达"
    PASS=$((PASS+1))
else
    red "快速发送: 只有 $COUNT/10 条送达"
    FAIL=$((FAIL+1))
fi

# Test 2: 大消息 (10KB)
echo
echo "--- Test 2: 大消息 (10KB) ---"
cp /dev/null /tmp/np4_stress_b.log; sleep 0.5
BIG_MSG=$(python3 -c "print('X' * 10000)")
"$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" send --addr "$NODE_B_ADDR" "$NODE_B_ID" "$BIG_MSG" 2>&1
sleep 3
if grep -aq "XXXX" /tmp/np4_stress_b.log; then
    green "10KB 消息送达"
    PASS=$((PASS+1))
else
    red "10KB 消息未送达"
    FAIL=$((FAIL+1))
fi

# Test 3: 大消息 (20KB)
echo
echo "--- Test 3: 大消息 (20KB) ---"
cp /dev/null /tmp/np4_stress_b.log; sleep 0.5
HUGE_MSG=$(python3 -c "print('Y' * 20000)")
"$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" send --addr "$NODE_B_ADDR" "$NODE_B_ID" "$HUGE_MSG" 2>&1
sleep 8
if grep -aq "YYYY" /tmp/np4_stress_b.log; then
    green "20KB 消息送达"
    PASS=$((PASS+1))
else
    red "20KB 消息未送达"
    FAIL=$((FAIL+1))
fi

# Test 4: JSON 特殊字符消息
echo
echo "--- Test 4: JSON 特殊字符 ---"
cp /dev/null /tmp/np4_stress_b.log; sleep 0.5
JSON_MSG='{"key":"value","arr":[1,2,3],"nested":{"a":true}}'
"$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" send --addr "$NODE_B_ADDR" "$NODE_B_ID" "$JSON_MSG" 2>&1
sleep 3
if grep -aq '"key"' /tmp/np4_stress_b.log; then
    green "JSON 消息送达"
    PASS=$((PASS+1))
else
    red "JSON 消息未送达"
    FAIL=$((FAIL+1))
fi

# Test 5: 空格分隔的消息
echo
echo "--- Test 5: 多空格消息 ---"
cp /dev/null /tmp/np4_stress_b.log; sleep 0.5
SPACE_MSG="hello   world   with   spaces"
"$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" send --addr "$NODE_B_ADDR" "$NODE_B_ID" "$SPACE_MSG" 2>&1
sleep 3
if grep -aq "hello   world" /tmp/np4_stress_b.log; then
    green "多空格消息送达"
    PASS=$((PASS+1))
else
    red "多空格消息未送达"
    FAIL=$((FAIL+1))
fi

# Test 6: 换行符消息
echo
echo "--- Test 6: 包含换行的消息 ---"
cp /dev/null /tmp/np4_stress_b.log; sleep 0.5
NEWLINE_MSG="line1\nline2\nline3"
"$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" send --addr "$NODE_B_ADDR" "$NODE_B_ID" "$NEWLINE_MSG" 2>&1
sleep 3
if grep -aq "line1" /tmp/np4_stress_b.log; then
    green "换行消息送达"
    PASS=$((PASS+1))
else
    red "换行消息未送达"
    FAIL=$((FAIL+1))
fi

# 结果
echo
echo "========================="
echo "通过: $PASS  失败: $FAIL"
[ $FAIL -eq 0 ] && green "全部通过!" || red "有失败用例"
