#!/bin/bash
# DID Service API 测试脚本
# 直接测试 DID Service (端口8081)，无需API Gateway

set -e

DID_SERVICE_URL="http://localhost:8081"
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "========================================"
echo "DID Service API 测试"
echo "========================================"
echo ""

# 检查服务是否运行
check_service() {
    if ! curl -s "${DID_SERVICE_URL}/healthz" > /dev/null 2>&1; then
        echo -e "${RED}❌ DID Service 未启动${NC}"
        echo "请先运行: go run cmd/server/main.go"
        exit 1
    fi
    echo -e "${GREEN}✅ DID Service 运行中${NC}"
}

# 测试健康检查
test_health() {
    echo ""
    echo "1. 测试健康检查..."
    RESPONSE=$(curl -s "${DID_SERVICE_URL}/healthz" || echo "{}")
    echo "响应: $RESPONSE"
}

# 测试创建DID
test_create_did() {
    echo ""
    echo "2. 测试创建DID..."
    echo -e "${YELLOW}⚠️ 注意：当前DID Service使用Kitex RPC协议${NC}"
    echo "需要启动服务后进行手动测试，或使用Kitex客户端"
}

# 主流程
main() {
    check_service
    test_health
    test_create_did
    
    echo ""
    echo "========================================"
    echo -e "${GREEN}测试完成${NC}"
    echo "========================================"
}

main
