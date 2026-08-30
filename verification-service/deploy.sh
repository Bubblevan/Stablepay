#!/bin/bash
# StablePay Verification Service — 构建镜像并部署到 ACK (zheda-agent)
# 含 consumer.go 修复：读取 ROCKETMQ_NAMESERVER（勿再连 Pod 内 127.0.0.1:9876）
#
# 用法: ./deploy.sh              # 自动生成时间戳版本
#       ./deploy.sh v20250604.1  # 指定版本

set -euo pipefail

REGISTRY="stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev"
IMAGE_NAME="verification-service"
NAMESPACE="zheda-agent"
DEPLOYMENT="stablepay-verification-service"
CONTAINER="verification-service"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
K8S_APP="${REPO_ROOT}/infra-deployment/k8s/ack/apps/verification-service.yaml"

if [ $# -gt 0 ] && [ -n "$1" ]; then
    VERSION="$1"
else
    VERSION="v$(date +'%Y%m%d.%H%M%S')"
fi

FULL_IMAGE="${REGISTRY}/${IMAGE_NAME}:${VERSION}"

echo "=== Building StablePay Verification Service ==="
echo "Version: ${VERSION}"
echo ""

echo "[1/5] Building Docker image..."
cd "$SCRIPT_DIR"
docker build -t "${IMAGE_NAME}:${VERSION}" .

echo "[2/5] Tagging image..."
docker tag "${IMAGE_NAME}:${VERSION}" "${REGISTRY}/${IMAGE_NAME}:${VERSION}"
docker tag "${IMAGE_NAME}:${VERSION}" "${REGISTRY}/${IMAGE_NAME}:latest"

echo "[3/5] Pushing to ACR..."
docker push "${REGISTRY}/${IMAGE_NAME}:${VERSION}"
docker push "${REGISTRY}/${IMAGE_NAME}:latest"

echo "[4/5] Applying Deployment (ROCKETMQ_NAMESERVER in yaml)..."
kubectl apply -f "$K8S_APP"

echo "[5/5] Updating Kubernetes deployment image..."
kubectl set image "deployment/${DEPLOYMENT}" \
    "${CONTAINER}=${FULL_IMAGE}" \
    -n "$NAMESPACE"

kubectl rollout status "deployment/${DEPLOYMENT}" -n "$NAMESPACE"

echo ""
echo "=== Build & Deploy Complete ==="
echo "Image: ${FULL_IMAGE}"
echo ""
echo "Verify MQ consumer connected to cluster nameserver:"
echo "  kubectl logs -n ${NAMESPACE} deploy/${DEPLOYMENT} --tail=30 | grep -i rocketmq"
echo ""
echo "After a successful POST /api/v1/pay, expect within ~5s:"
echo "  kubectl logs -n ${NAMESPACE} deploy/${DEPLOYMENT} --since=2m | grep -E '收到支付|购买关系已入库'"
echo ""
echo "Verify purchase API (replace DIDs):"
echo "  curl -sS 'https://ai.wenfu.cn/api/v1/verify?agent_did=did:solana:AGENT&skill_did=did:solana:SKILL' -H 'X-API-Key: stablepay-dev-key' | jq ."
echo ""
echo "Reset dev DB purchase state (optional):"
echo "  ./scripts/reset-mysql-purchase-state.sh"
echo "If AutoMigrate error 1170 on tx_id, run once:"
echo "  kubectl exec -n ${NAMESPACE} deploy/stablepay-mysql -- mysql -ustablepay -pstablepay123 < scripts/fix-verification-db-schema.sql"
echo ""
echo "Rollback (if needed):"
echo "  kubectl rollout undo deployment/${DEPLOYMENT} -n ${NAMESPACE}"
