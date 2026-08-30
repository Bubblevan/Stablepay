#!/bin/bash
# StablePay Query Service — 构建镜像并部署到 ACK (zheda-agent)
# 用法: ./deploy.sh              # 自动生成时间戳版本
#       ./deploy.sh v20250604.1  # 指定版本

set -euo pipefail

REGISTRY="stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev"
IMAGE_NAME="query-service"
NAMESPACE="zheda-agent"
DEPLOYMENT="stablepay-query-service"
CONTAINER="query-service"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
K8S_APP="${REPO_ROOT}/infra-deployment/k8s/ack/apps/query-service.yaml"

if [ $# -gt 0 ] && [ -n "$1" ]; then
    VERSION="$1"
else
    VERSION="v$(date +'%Y%m%d.%H%M%S')"
fi

FULL_IMAGE="${REGISTRY}/${IMAGE_NAME}:${VERSION}"

echo "=== Building StablePay Query Service ==="
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

echo "[4/5] Applying Deployment (mainnet SOLANA_RPC_ENDPOINT in yaml)..."
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
echo "Verify balance RPC env (expect mainnet-beta proxy):"
echo "  kubectl exec -n ${NAMESPACE} deploy/${DEPLOYMENT} -- sh -c 'env | grep -E \"SOLANA|QUERY_BALANCE\"'"
echo ""
echo "Test on-chain balance (replace wallet):"
echo "  kubectl run balance-probe -n ${NAMESPACE} --restart=Never \\"
echo "    --image=${REGISTRY}/curl:8.5.0 \\"
echo "    --overrides='{\"spec\":{\"imagePullSecrets\":[{\"name\":\"acr-secret\"}]}}' \\"
echo "    --command -- curl -sS -m 30 \\"
echo "    # Query is Kitex-only; use the gateway or a Kitex client for balance queries."
echo "  sleep 5; kubectl logs balance-probe -n ${NAMESPACE}; kubectl delete pod balance-probe -n ${NAMESPACE}"
echo ""
echo "Rollback (if needed):"
echo "  kubectl rollout undo deployment/${DEPLOYMENT} -n ${NAMESPACE}"
