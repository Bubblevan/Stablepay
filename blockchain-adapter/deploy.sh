#!/bin/bash
# StablePay Blockchain Adapter — 构建镜像并部署到 ACK (zheda-agent)
# 用法: ./deploy.sh              # 自动生成时间戳版本
#       ./deploy.sh v20250604.1  # 指定版本

set -euo pipefail

REGISTRY="stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev"
IMAGE_NAME="blockchain-adapter"
NAMESPACE="zheda-agent"
DEPLOYMENT="stablepay-blockchain-adapter"
CONTAINER="blockchain-adapter"

# 获取脚本所在目录（兼容符号链接）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
K8S_CONFIG="${REPO_ROOT}/infra-deployment/k8s/ack/config/blockchain-adapter-config.yaml"
K8S_APP="${REPO_ROOT}/infra-deployment/k8s/ack/apps/blockchain-adapter.yaml"

if [ $# -gt 0 ] && [ -n "$1" ]; then
    VERSION="$1"
else
    VERSION="v$(date +'%Y%m%d.%H%M%S')"
fi

FULL_IMAGE="${REGISTRY}/${IMAGE_NAME}:${VERSION}"

echo "=== Building StablePay Blockchain Adapter ==="
echo "Version: ${VERSION}"
echo ""

echo "[1/6] Building Docker image..."
cd "$SCRIPT_DIR"
docker build -t "${IMAGE_NAME}:${VERSION}" .

echo "[2/6] Tagging image..."
docker tag "${IMAGE_NAME}:${VERSION}" "${REGISTRY}/${IMAGE_NAME}:${VERSION}"
docker tag "${IMAGE_NAME}:${VERSION}" "${REGISTRY}/${IMAGE_NAME}:latest"

echo "[3/6] Pushing to ACR..."
docker push "${REGISTRY}/${IMAGE_NAME}:${VERSION}"
docker push "${REGISTRY}/${IMAGE_NAME}:latest"

echo "[4/6] Applying mainnet ConfigMap and Deployment env..."
kubectl apply -f "$K8S_CONFIG"
kubectl apply -f "$K8S_APP"

echo "[5/6] Updating Kubernetes deployment image..."
kubectl set image "deployment/${DEPLOYMENT}" \
    "${CONTAINER}=${FULL_IMAGE}" \
    -n "$NAMESPACE"

echo "[6/6] Waiting for rollout..."
kubectl rollout status "deployment/${DEPLOYMENT}" -n "$NAMESPACE"

echo ""
echo "=== Build & Deploy Complete ==="
echo "Image: ${FULL_IMAGE}"
echo ""
echo "Verify Solana network (expect mainnet-beta; alpine 无 grep，用 cat):"
echo "  kubectl exec -n ${NAMESPACE} deploy/${DEPLOYMENT} -- cat /app/config/cker.yaml"
echo "  kubectl exec -n ${NAMESPACE} deploy/stablepay-query-service -- env | grep SOLANA"
echo "  ./scripts/verify-balance.sh did:solana:YOUR_WALLET"
echo ""
echo "Rollback (if needed):"
echo "  kubectl rollout undo deployment/${DEPLOYMENT} -n ${NAMESPACE}"