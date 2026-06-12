#!/bin/bash
# np4cli 节点功能测试
set -e

BOOTSTRAP_PORT=4200
NODE_A_PORT=4201
NODE_B_PORT=4202
BIN_DIR="$(cd "$(dirname "$0")/.." && pwd)/bin"
PASS=0
FAIL=0

green() { echo -e "\033[32m✓ $1\033[0m"; }
red()   { echo -e "\033[31m✗ $1\033[0m"; }

cleanup() {
    [ -n "$BOOTSTRAP_PID" ] && kill "$BOOTSTRAP_PID" 2>/dev/null
    [ -n "$NODE_A_PID" ] && kill "$NODE_A_PID" 2>/dev/null
    [ -n "$NODE_B_PID" ] && kill "$NODE_B_PID" 2>/dev/null
    wait 2>/dev/null
}
trap cleanup EXIT

echo "=== np4cli 节点测试 ==="
echo

# 构建
echo "--- 构建 ---"
go build -o "$BIN_DIR/np4cli" ./cmd/np4cli/ 2>/dev/null
go build -o "$BIN_DIR/bootstrap" ./cmd/bootstrap/ 2>/dev/null
green "构建成功"

# 启动 bootstrap
echo
echo "--- 启动 Bootstrap ---"
"$BIN_DIR/bootstrap" start --port $BOOTSTRAP_PORT --web 0 &
BOOTSTRAP_PID=$!
sleep 2

BOOTSTRAP_ADDR=$("$BIN_DIR/bootstrap" id --port $BOOTSTRAP_PORT 2>&1 | grep "/ip4/127.0.0.1" | head -1 | awk '{print $1}')
green "Bootstrap 地址: $BOOTSTRAP_ADDR"

# Test 1: np4cli --help
echo
echo "--- np4cli 帮助 ---"
OUTPUT=$("$BIN_DIR/np4cli" --help 2>&1)
if echo "$OUTPUT" | grep -q "np4cli"; then
    green "帮助信息正常"
    PASS=$((PASS+1))
else
    red "帮助信息异常"
    FAIL=$((FAIL+1))
fi

# Test 2: np4cli id
echo
echo "--- np4cli id ---"
OUTPUT=$("$BIN_DIR/np4cli" --port $NODE_A_PORT id 2>&1)
if echo "$OUTPUT" | grep -q "Peer ID:"; then
    green "id 命令正常"
    PASS=$((PASS+1))
else
    red "id 命令异常"
    FAIL=$((FAIL+1))
fi

# Test 3: 启动节点 A
echo
echo "--- 启动节点 A ---"
"$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_ADDR" id > /dev/null 2>&1
green "节点 A 创建成功"

# Test 4: 启动节点 B
echo
echo "--- 启动节点 B ---"
"$BIN_DIR/np4cli" --port $NODE_B_PORT --bootstrap "$BOOTSTRAP_ADDR" id > /dev/null 2>&1
green "节点 B 创建成功"

# Test 5: peers 命令
echo
echo "--- peers 命令 ---"
OUTPUT=$("$BIN_DIR/np4cli" --port $NODE_A_PORT --bootstrap "$BOOTSTRAP_ADDR" peers 2>&1)
if [ $? -eq 0 ]; then
    green "peers 命令执行成功"
    PASS=$((PASS+1))
else
    red "peers 命令失败"
    FAIL=$((FAIL+1))
fi

# Test 6: connect 命令（连接自己测试格式）
echo
echo "--- connect 命令 ---"
NODE_B_ADDR=$("$BIN_DIR/np4cli" --port $NODE_B_PORT id 2>&1 | grep "/ip4/127.0.0.1" | head -1 | awk '{print $1}')
OUTPUT=$("$BIN_DIR/np4cli" --port $NODE_A_PORT connect "$NODE_B_ADDR" 2>&1)
if echo "$OUTPUT" | grep -q "Connected"; then
    green "连接成功"
    PASS=$((PASS+1))
else
    red "连接失败: $OUTPUT"
    FAIL=$((FAIL+1))
fi

# 结果
echo
echo "========================="
echo "通过: $PASS  失败: $FAIL"
[ $FAIL -eq 0 ] && green "全部通过!" || red "有失败用例"
