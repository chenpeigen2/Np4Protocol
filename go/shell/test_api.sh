#!/bin/bash
# API 接口详细测试
set -e

PORT=4300
WEB_PORT=8300
BIN_DIR="$(cd "$(dirname "$0")/.." && pwd)/bin"
TEST_IDENTITY="/tmp/np4_test_boot_$(basename $0 .sh)_$$"
PASS=0
FAIL=0

green() { echo -e "\033[32m✓ $1\033[0m"; }
red()   { echo -e "\033[31m✗ $1\033[0m"; }

cleanup() {
    [ -n "$PID" ] && kill "$PID" 2>/dev/null
    wait "$PID" 2>/dev/null
    rm -f "$TEST_IDENTITY" /tmp/np4_boot_$$.log
}
trap cleanup EXIT

echo "=== API 接口测试 ==="
echo

# 启动节点
go build -o "$BIN_DIR/bootstrap" ./cmd/bootstrap/ 2>/dev/null
"$BIN_DIR/bootstrap" start --port $PORT --web $WEB_PORT --identity "$TEST_IDENTITY" > /tmp/np4_boot_$$.log 2>&1 &
PID=$!
sleep 2

# Test 1: GET /api/status 返回 200
echo "--- HTTP 状态码 ---"
CODE=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:$WEB_PORT/api/status")
if [ "$CODE" = "200" ]; then
    green "/api/status 返回 200"
    PASS=$((PASS+1))
else
    red "/api/status 返回 $CODE"
    FAIL=$((FAIL+1))
fi

# Test 2: Content-Type 为 JSON
echo
echo "--- Content-Type ---"
CT=$(curl -s -D - -o /dev/null "http://localhost:$WEB_PORT/api/status" | grep -i content-type | tr -d '\r')
if echo "$CT" | grep -q "application/json"; then
    green "Content-Type: application/json"
    PASS=$((PASS+1))
else
    red "Content-Type 异常: $CT"
    FAIL=$((FAIL+1))
fi

# Test 3: CORS 头
echo
echo "--- CORS ---"
CORS=$(curl -s -D - -o /dev/null "http://localhost:$WEB_PORT/api/status" | grep -i "access-control-allow-origin" | tr -d '\r')
if echo "$CORS" | grep -q "*"; then
    green "CORS: *"
    PASS=$((PASS+1))
else
    red "CORS 缺失"
    FAIL=$((FAIL+1))
fi

# Test 4: status JSON 字段完整性
echo
echo "--- status 字段完整性 ---"
RESP=$(curl -s "http://localhost:$WEB_PORT/api/status")
HAS_PEER_ID=$(echo "$RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); print('peer_id' in d)" 2>/dev/null)
HAS_ADDRS=$(echo "$RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); print('addresses' in d)" 2>/dev/null)
HAS_UPTIME=$(echo "$RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); print('uptime' in d)" 2>/dev/null)
HAS_PEERS=$(echo "$RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); print('dht_peers' in d)" 2>/dev/null)
HAS_STATUS=$(echo "$RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); print('status' in d)" 2>/dev/null)

ALL_OK=true
[ "$HAS_PEER_ID" = "True" ] && green "  peer_id 字段存在" || { red "  peer_id 缺失"; ALL_OK=false; }
[ "$HAS_ADDRS" = "True" ] && green "  addresses 字段存在" || { red "  addresses 缺失"; ALL_OK=false; }
[ "$HAS_UPTIME" = "True" ] && green "  uptime 字段存在" || { red "  uptime 缺失"; ALL_OK=false; }
[ "$HAS_PEERS" = "True" ] && green "  dht_peers 字段存在" || { red "  dht_peers 缺失"; ALL_OK=false; }
[ "$HAS_STATUS" = "True" ] && green "  status 字段存在" || { red "  status 缺失"; ALL_OK=false; }

if $ALL_OK; then
    PASS=$((PASS+1))
else
    FAIL=$((FAIL+1))
fi

# Test 5: peer_id 格式（base58btc）
echo
echo "--- peer_id 格式 ---"
PEER_ID=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['peer_id'])" 2>/dev/null)
if echo "$PEER_ID" | grep -qE "^12D3Koo"; then
    green "peer_id 格式正确 (starts with 12D3Koo)"
    PASS=$((PASS+1))
else
    red "peer_id 格式异常: $PEER_ID"
    FAIL=$((FAIL+1))
fi

# Test 6: addresses 包含 /p2p/ 后缀
echo
echo "--- addresses 格式 ---"
ADDR=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['addresses'][0])" 2>/dev/null)
if echo "$ADDR" | grep -q "/p2p/"; then
    green "address 包含 /p2p/ 后缀: $ADDR"
    PASS=$((PASS+1))
else
    red "address 格式异常: $ADDR"
    FAIL=$((FAIL+1))
fi

# Test 7: uptime 递增
echo
echo "--- uptime 递增 ---"
UPTIME1=$(curl -s "http://localhost:$WEB_PORT/api/status" | python3 -c "import sys,json; print(json.load(sys.stdin)['uptime'])" 2>/dev/null)
sleep 2
UPTIME2=$(curl -s "http://localhost:$WEB_PORT/api/status" | python3 -c "import sys,json; print(json.load(sys.stdin)['uptime'])" 2>/dev/null)
if [ "$UPTIME1" != "$UPTIME2" ]; then
    green "uptime 递增: $UPTIME1 -> $UPTIME2"
    PASS=$((PASS+1))
else
    red "uptime 未变化"
    FAIL=$((FAIL+1))
fi

# Test 8: /api/peers 返回数组
echo
echo "--- /api/peers 格式 ---"
PEERS=$(curl -s "http://localhost:$WEB_PORT/api/peers")
IS_ARRAY=$(echo "$PEERS" | python3 -c "import sys,json; print(isinstance(json.load(sys.stdin), list))" 2>/dev/null)
if [ "$IS_ARRAY" = "True" ]; then
    green "/api/peers 返回数组"
    PASS=$((PASS+1))
else
    red "/api/peers 未返回数组"
    FAIL=$((FAIL+1))
fi

# Test 9: GET / 返回 HTML
echo
echo "--- Dashboard HTML ---"
CODE=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:$WEB_PORT/")
if [ "$CODE" = "200" ]; then
    green "Dashboard 返回 200"
    PASS=$((PASS+1))
else
    red "Dashboard 返回 $CODE"
    FAIL=$((FAIL+1))
fi

# Test 10: JSON 格式化输出
echo
echo "--- JSON 格式化 ---"
PRETTY=$(curl -s "http://localhost:$WEB_PORT/api/status" | python3 -m json.tool 2>/dev/null)
if [ -n "$PRETTY" ]; then
    green "JSON 格式正确"
    PASS=$((PASS+1))
else
    red "JSON 格式错误"
    FAIL=$((FAIL+1))
fi

# 结果
echo
echo "========================="
echo "通过: $PASS  失败: $FAIL"
[ $FAIL -eq 0 ] && green "全部通过!" || red "有失败用例"
