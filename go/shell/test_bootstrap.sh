#!/bin/bash
# Bootstrap 节点启动和 API 测试
set -e

PORT=4100
WEB_PORT=8100
BIN_DIR="$(cd "$(dirname "$0")/.." && pwd)/bin"
TEST_IDENTITY="/tmp/np4_test_boot_$(basename $0 .sh)_$$"
PASS=0
FAIL=0

green() { echo -e "\033[32m✓ $1\033[0m"; }
red()   { echo -e "\033[31m✗ $1\033[0m"; }

cleanup() {
    [ -n "$BOOTSTRAP_PID" ] && kill "$BOOTSTRAP_PID" 2>/dev/null
    wait "$BOOTSTRAP_PID" 2>/dev/null
    rm -f "$TEST_IDENTITY" /tmp/np4_boot_$$.log
}
trap cleanup EXIT

echo "=== Bootstrap 节点测试 ==="
echo

# Test 1: 构建
echo "--- 构建 ---"
if go build -o "$BIN_DIR/bootstrap" ./cmd/bootstrap/ 2>/dev/null; then
    green "构建成功"
    PASS=$((PASS+1))
else
    red "构建失败"
    FAIL=$((FAIL+1))
    exit 1
fi

# Test 2: 帮助信息
echo
echo "--- 帮助信息 ---"
OUTPUT=$("$BIN_DIR/bootstrap" --help 2>&1)
if echo "$OUTPUT" | grep -q "np4bootstrap"; then
    green "显示帮助信息"
    PASS=$((PASS+1))
else
    red "帮助信息异常"
    FAIL=$((FAIL+1))
fi

# Test 3: id 命令
echo
echo "--- id 命令 ---"
OUTPUT=$("$BIN_DIR/bootstrap" id --port $PORT --identity "$TEST_IDENTITY" 2>&1)
if echo "$OUTPUT" | grep -q "Peer ID:" && echo "$OUTPUT" | grep -q "Multiaddr:"; then
    green "id 命令输出正确"
    PASS=$((PASS+1))
else
    red "id 命令输出异常"
    FAIL=$((FAIL+1))
fi

# Test 4: 启动节点
echo
echo "--- 启动节点 ---"
"$BIN_DIR/bootstrap" start --port $PORT --web $WEB_PORT --identity "$TEST_IDENTITY" > /tmp/np4_boot_$$.log 2>&1 &
BOOTSTRAP_PID=$!
sleep 2

if kill -0 "$BOOTSTRAP_PID" 2>/dev/null; then
    green "节点启动成功 (PID: $BOOTSTRAP_PID)"
    PASS=$((PASS+1))
else
    red "节点启动失败"
    FAIL=$((FAIL+1))
    exit 1
fi

# Test 5: API /api/status
echo
echo "--- API /api/status ---"
RESP=$(curl -s "http://localhost:$WEB_PORT/api/status")
if echo "$RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); assert d['status']=='online'" 2>/dev/null; then
    green "/api/status 返回 online"
    PASS=$((PASS+1))
else
    red "/api/status 响应异常: $RESP"
    FAIL=$((FAIL+1))
fi

# Test 6: API /api/status 包含 peer_id
echo
echo "--- API peer_id ---"
PEER_ID=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['peer_id'])" 2>/dev/null)
if [ -n "$PEER_ID" ] && [ ${#PEER_ID} -gt 10 ]; then
    green "peer_id: $PEER_ID"
    PASS=$((PASS+1))
else
    red "peer_id 异常"
    FAIL=$((FAIL+1))
fi

# Test 7: API /api/status 包含 addresses
echo
echo "--- API addresses ---"
ADDRS=$(echo "$RESP" | python3 -c "import sys,json; print(len(json.load(sys.stdin)['addresses']))" 2>/dev/null)
if [ "$ADDRS" -gt 0 ] 2>/dev/null; then
    green "addresses 数量: $ADDRS"
    PASS=$((PASS+1))
else
    red "addresses 为空"
    FAIL=$((FAIL+1))
fi

# Test 8: API /api/peers
echo
echo "--- API /api/peers ---"
RESP=$(curl -s "http://localhost:$WEB_PORT/api/peers")
if echo "$RESP" | python3 -c "import sys,json; assert isinstance(json.load(sys.stdin), list)" 2>/dev/null; then
    green "/api/peers 返回列表"
    PASS=$((PASS+1))
else
    red "/api/peers 响应异常"
    FAIL=$((FAIL+1))
fi

# Test 9: Dashboard HTML
echo
echo "--- Dashboard HTML ---"
HTML=$(curl -s "http://localhost:$WEB_PORT/")
if echo "$HTML" | grep -q "Np4Protocol"; then
    green "Dashboard 页面正常"
    PASS=$((PASS+1))
else
    red "Dashboard 页面异常"
    FAIL=$((FAIL+1))
fi

# 结果
echo
echo "========================="
echo "通过: $PASS  失败: $FAIL"
[ $FAIL -eq 0 ] && green "全部通过!" || red "有失败用例"
