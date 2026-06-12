#!/bin/bash
# 节点间通信测试：验证消息实际送达
set +e

BOOTSTRAP_PORT=4500
WEB_PORT=8500
NODE_A_PORT=4501
NODE_B_PORT=4502
BIN_DIR="$(cd "$(dirname "$0")/.." && pwd)/bin"
PASS=0
FAIL=0

green() { echo -e "\033[32m✓ $1\033[0m"; }
red()   { echo -e "\033[31m✗ $1\033[0m"; }

cleanup() {
    for port in $BOOTSTRAP_PORT $NODE_A_PORT $NODE_B_PORT; do
        lsof -ti :$port 2>/dev/null | xargs kill -9 2>/dev/null
    done
    rm -f /tmp/np4_chat_b.log /tmp/np4_chat_b.fifo
}
trap cleanup EXIT

echo "=== 节点间通信测试 ==="
echo

# 构建
echo "--- 构建 ---"
go build -o "$BIN_DIR/bootstrap" ./cmd/bootstrap/ 2>/dev/null
go build -o "$BIN_DIR/np4cli" ./cmd/np4cli/ 2>/dev/null
green "构建成功"

# 清理旧进程
for port in $BOOTSTRAP_PORT $NODE_A_PORT $NODE_B_PORT; do
    lsof -ti :$port 2>/dev/null | xargs kill -9 2>/dev/null
done
sleep 1

# 启动 Bootstrap
echo
echo "--- 启动 Bootstrap ---"
"$BIN_DIR/bootstrap" start --port $BOOTSTRAP_PORT --web $WEB_PORT &
BOOTSTRAP_PID=$!
sleep 2
green "Bootstrap 启动成功"

BOOTSTRAP_MULTIADDR=$("$BIN_DIR/bootstrap" id --port $BOOTSTRAP_PORT 2>&1 | grep "/ip4/127.0.0.1" | head -1 | awk '{print $1}')
green "Bootstrap: $BOOTSTRAP_MULTIADDR"

# 启动节点 B（使用 fifo 保持输出流）
echo
echo "--- 启动节点 B (chat 模式) ---"
rm -f /tmp/np4_chat_b.fifo
mkfifo /tmp/np4_chat_b.fifo
(tail -f /dev/null | "$BIN_DIR/np4cli" --port $NODE_B_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" chat > /tmp/np4_chat_b.fifo 2>&1) &
CHAT_B_PID=$!
# 后台读取 fifo 到日志文件
cat /tmp/np4_chat_b.fifo > /tmp/np4_chat_b.log &
CAT_PID=$!
sleep 3

if ! kill -0 "$CHAT_B_PID" 2>/dev/null; then
    red "节点 B 启动失败"
    cat /tmp/np4_chat_b.log
    exit 1
fi
green "节点 B 启动成功"

NODE_B_ID=$(grep "Peer ID:" /tmp/np4_chat_b.log | awk '{print $3}')
NODE_B_MULTIADDR=$(grep "/ip4/127.0.0.1" /tmp/np4_chat_b.log | head -1 | awk '{print $1}')
green "节点 B ID: $NODE_B_ID"
green "节点 B 地址: $NODE_B_MULTIADDR"

if [ -z "$NODE_B_ID" ] || [ -z "$NODE_B_MULTIADDR" ]; then
    red "无法获取节点 B 信息"
    exit 1
fi

send_msg() {
    local msg="$1"
    # 截断日志（只截断文件，不影响 cat 的读取位置）
    cp /dev/null /tmp/np4_chat_b.log
    sleep 0.5
    "$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" send --addr "$NODE_B_MULTIADDR" "$NODE_B_ID" "$msg" 2>&1
    sleep 3
}

# Test 1: 节点 A 连接节点 B
echo
echo "--- Test 1: A 连接 B ---"
OUTPUT=$("$BIN_DIR/np4cli" --port $NODE_A_PORT connect "$NODE_B_MULTIADDR" 2>&1)
if echo "$OUTPUT" | grep -q "Connected"; then
    green "A -> B 连接成功"
    PASS=$((PASS+1))
else
    red "连接失败: $OUTPUT"
    FAIL=$((FAIL+1))
fi

# Test 2: A 发送带时间戳的消息
echo
echo "--- Test 2: A 发送带时间戳消息 ---"
TIMESTAMP=$(date +%H:%M:%S)
send_msg "timestamp-$TIMESTAMP"
if grep -aq "timestamp-$TIMESTAMP" /tmp/np4_chat_b.log 2>/dev/null; then
    green "时间戳消息送达"
    PASS=$((PASS+1))
else
    red "时间戳消息未送达"
    FAIL=$((FAIL+1))
fi

# Test 3: A 发送英文消息
echo
echo "--- Test 3: A 发送英文消息 ---"
send_msg "hello from node A $(date +%H:%M:%S)"
if grep -aq "hello from node A" /tmp/np4_chat_b.log 2>/dev/null; then
    green "英文消息送达"
    PASS=$((PASS+1))
else
    red "英文消息未送达"
    FAIL=$((FAIL+1))
fi

# Test 4: A 发送特殊字符消息
echo
echo "--- Test 4: A 发送特殊字符消息 ---"
send_msg 'special-chars: !@#$%^&*()_+-=[]{}'
if grep -aq "special-chars:" /tmp/np4_chat_b.log 2>/dev/null; then
    green "特殊字符消息送达"
    PASS=$((PASS+1))
else
    red "特殊字符消息未送达"
    FAIL=$((FAIL+1))
fi

# Test 5: A 发送长消息
echo
echo "--- Test 5: A 发送长消息 ---"
send_msg "$(python3 -c "print('A' * 500)")"
if grep -aq "AAAA" /tmp/np4_chat_b.log 2>/dev/null; then
    green "长消息送达 (500 字符)"
    PASS=$((PASS+1))
else
    red "长消息未送达"
    FAIL=$((FAIL+1))
fi

# Test 6: 连续发送多条消息
echo
echo "--- Test 6: 连续发送 3 条消息 ---"
cp /dev/null /tmp/np4_chat_b.log
sleep 0.5
for i in 1 2 3; do
    "$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_MULTIADDR" send --addr "$NODE_B_MULTIADDR" "$NODE_B_ID" "msg-$i" 2>&1
done
sleep 4

COUNT=$(grep -ac "msg-" /tmp/np4_chat_b.log 2>/dev/null || echo 0)
if [ "$COUNT" -ge 3 ]; then
    green "连续 3 条消息全部送达"
    PASS=$((PASS+1))
else
    red "只收到 $COUNT/3 条消息"
    FAIL=$((FAIL+1))
fi

# Test 7: 消息包含 Sender ID
echo
echo "--- Test 7: 消息格式验证 ---"
send_msg "format-test"
if grep -aq "12D3Koo" /tmp/np4_chat_b.log 2>/dev/null; then
    green "消息包含 Peer ID 格式的 Sender ID"
    PASS=$((PASS+1))
else
    red "消息缺少 Sender ID"
    FAIL=$((FAIL+1))
fi

# 结果
echo
echo "========================="
echo "通过: $PASS  失败: $FAIL"
[ $FAIL -eq 0 ] && green "全部通过!" || red "有失败用例"
