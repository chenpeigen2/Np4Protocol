#!/bin/bash
# 多节点网状通信测试：3个节点互相通信
set +e

BOOTSTRAP_PORT=4600
WEB_PORT=8600
NODE_A_PORT=4601
NODE_B_PORT=4602
NODE_C_PORT=4603
BIN_DIR="$(cd "$(dirname "$0")/.." && pwd)/bin"
TEST_IDENTITY="/tmp/np4_test_boot_$(basename $0 .sh)_$$"
NODE_A_IDENTITY="/tmp/np4_test_multiA_$(basename $0 .sh)_$$"
NODE_B_IDENTITY="/tmp/np4_test_multiB_$(basename $0 .sh)_$$"
NODE_C_IDENTITY="/tmp/np4_test_multiC_$(basename $0 .sh)_$$"
PASS=0
FAIL=0

green() { echo -e "\033[32m✓ $1\033[0m"; }
red()   { echo -e "\033[31m✗ $1\033[0m"; }

cleanup() {
    for port in $BOOTSTRAP_PORT $NODE_A_PORT $NODE_B_PORT $NODE_C_PORT; do
        lsof -ti :$port 2>/dev/null | xargs kill -9 2>/dev/null
    done
    rm -f /tmp/np4_multi_*.log /tmp/np4_multi_*.fifo "$TEST_IDENTITY" /tmp/np4_boot_$$.log "$NODE_A_IDENTITY" "$NODE_B_IDENTITY" "$NODE_C_IDENTITY"
}
trap cleanup EXIT

echo "=== 多节点网状通信测试 ==="
echo

# 构建
go build -o "$BIN_DIR/bootstrap" ./cmd/bootstrap/ 2>/dev/null
go build -o "$BIN_DIR/np4cli" ./cmd/np4cli/ 2>/dev/null
green "构建成功"

# 清理旧进程
for port in $BOOTSTRAP_PORT $NODE_A_PORT $NODE_B_PORT $NODE_C_PORT; do
    lsof -ti :$port 2>/dev/null | xargs kill -9 2>/dev/null
done
sleep 1

# 启动 Bootstrap
"$BIN_DIR/bootstrap" start --port $BOOTSTRAP_PORT --web $WEB_PORT --identity "$TEST_IDENTITY" > /tmp/np4_boot_$$.log 2>&1 &
sleep 2
BOOTSTRAP_MULTIADDR=$("$BIN_DIR/bootstrap" id --port $BOOTSTRAP_PORT --identity "$TEST_IDENTITY" 2>&1 | grep "/ip4/127.0.0.1" | head -1 | awk '{print $NF}')
green "Bootstrap: $BOOTSTRAP_MULTIADDR"

# 启动 3 个节点
start_node() {
    local port=$1 name=$2 ident=$3
    rm -f /tmp/np4_multi_${name}.fifo /tmp/np4_multi_${name}.log
    mkfifo /tmp/np4_multi_${name}.fifo
    (tail -f /dev/null | "$BIN_DIR/np4cli" --port $port --bootstrap "$BOOTSTRAP_MULTIADDR" --identity "$ident" chat > /tmp/np4_multi_${name}.fifo 2>&1) &
    cat /tmp/np4_multi_${name}.fifo > /tmp/np4_multi_${name}.log &
    sleep 3
}

echo
echo "--- 启动 3 个节点 ---"
start_node $NODE_A_PORT "a" "$NODE_A_IDENTITY"
start_node $NODE_B_PORT "b" "$NODE_B_IDENTITY"
start_node $NODE_C_PORT "c" "$NODE_C_IDENTITY"

NODE_A_ID=$(grep "Peer ID:" /tmp/np4_multi_a.log | awk '{print $3}')
NODE_A_ADDR=$(grep "/ip4/127.0.0.1" /tmp/np4_multi_a.log | head -1 | awk '{print $1}')
NODE_B_ID=$(grep "Peer ID:" /tmp/np4_multi_b.log | awk '{print $3}')
NODE_B_ADDR=$(grep "/ip4/127.0.0.1" /tmp/np4_multi_b.log | head -1 | awk '{print $1}')
NODE_C_ID=$(grep "Peer ID:" /tmp/np4_multi_c.log | awk '{print $3}')
NODE_C_ADDR=$(grep "/ip4/127.0.0.1" /tmp/np4_multi_c.log | head -1 | awk '{print $1}')

green "A: $NODE_A_ID"
green "B: $NODE_B_ID"
green "C: $NODE_C_ID"

# Test 1: A 连接 B 和 C
echo
echo "--- Test 1: A 连接 B 和 C ---"
OUT_AB=$("$BIN_DIR/np4cli" --port $NODE_A_PORT --identity "$NODE_A_IDENTITY" connect "$NODE_B_ADDR" 2>&1)
OUT_AC=$("$BIN_DIR/np4cli" --port $NODE_A_PORT --identity "$NODE_A_IDENTITY" connect "$NODE_C_ADDR" 2>&1)
if echo "$OUT_AB" | grep -q "Connected" && echo "$OUT_AC" | grep -q "Connected"; then
    green "A -> B, A -> C 连接成功"
    PASS=$((PASS+1))
else
    red "连接失败"
    FAIL=$((FAIL+1))
fi

# Test 2: B 连接 C
echo
echo "--- Test 2: B 连接 C ---"
OUT_BC=$("$BIN_DIR/np4cli" --port $NODE_B_PORT --identity "$NODE_B_IDENTITY" connect "$NODE_C_ADDR" 2>&1)
if echo "$OUT_BC" | grep -q "Connected"; then
    green "B -> C 连接成功"
    PASS=$((PASS+1))
else
    red "B -> C 连接失败"
    FAIL=$((FAIL+1))
fi

# Test 3: A 发送给 B
echo
echo "--- Test 3: A -> B 消息 ---"
cp /dev/null /tmp/np4_multi_b.log; sleep 0.5
"$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" --identity "$NODE_A_IDENTITY" send --addr "$NODE_B_ADDR" "$NODE_B_ID" "from-A-to-B" 2>&1
sleep 3
if grep -aq "from-A-to-B" /tmp/np4_multi_b.log; then
    green "A -> B 送达"
    PASS=$((PASS+1))
else
    red "A -> B 未送达"
    FAIL=$((FAIL+1))
fi

# Test 4: A 发送给 C
echo
echo "--- Test 4: A -> C 消息 ---"
cp /dev/null /tmp/np4_multi_c.log; sleep 0.5
"$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" --identity "$NODE_A_IDENTITY" send --addr "$NODE_C_ADDR" "$NODE_C_ID" "from-A-to-C" 2>&1
sleep 3
if grep -aq "from-A-to-C" /tmp/np4_multi_c.log; then
    green "A -> C 送达"
    PASS=$((PASS+1))
else
    red "A -> C 未送达"
    FAIL=$((FAIL+1))
fi

# Test 5: B 发送给 A
echo
echo "--- Test 5: B -> A 消息 ---"
cp /dev/null /tmp/np4_multi_a.log; sleep 0.5
"$BIN_DIR/np4cli" --port $NODE_B_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" --identity "$NODE_B_IDENTITY" send --addr "$NODE_A_ADDR" "$NODE_A_ID" "from-B-to-A" 2>&1
sleep 3
if grep -aq "from-B-to-A" /tmp/np4_multi_a.log; then
    green "B -> A 送达"
    PASS=$((PASS+1))
else
    red "B -> A 未送达"
    FAIL=$((FAIL+1))
fi

# Test 6: C 发送给 A
echo
echo "--- Test 6: C -> A 消息 ---"
cp /dev/null /tmp/np4_multi_a.log; sleep 0.5
"$BIN_DIR/np4cli" --port $NODE_C_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" --identity "$NODE_C_IDENTITY" send --addr "$NODE_A_ADDR" "$NODE_A_ID" "from-C-to-A" 2>&1
sleep 3
if grep -aq "from-C-to-A" /tmp/np4_multi_a.log; then
    green "C -> A 送达"
    PASS=$((PASS+1))
else
    red "C -> A 未送达"
    FAIL=$((FAIL+1))
fi

# Test 7: C 发送给 B
echo
echo "--- Test 7: C -> B 消息 ---"
cp /dev/null /tmp/np4_multi_b.log; sleep 0.5
"$BIN_DIR/np4cli" --port $NODE_C_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" --identity "$NODE_C_IDENTITY" send --addr "$NODE_B_ADDR" "$NODE_B_ID" "from-C-to-B" 2>&1
sleep 3
if grep -aq "from-C-to-B" /tmp/np4_multi_b.log; then
    green "C -> B 送达"
    PASS=$((PASS+1))
else
    red "C -> B 未送达"
    FAIL=$((FAIL+1))
fi

# 结果
echo
echo "========================="
echo "通过: $PASS  失败: $FAIL"
[ $FAIL -eq 0 ] && green "全部通过!" || red "有失败用例"
