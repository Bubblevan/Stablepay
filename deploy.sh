#!/bin/bash
# StablePay Merchant Backend - build, push, and deploy to ACK.
# Usage:
#   ./deploy.sh
#   ./deploy.sh v20250612
#   ./deploy.sh v20250612 --dry

set -euo pipefail

REGISTRY="stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev"
IMAGE_NAME="merchant-backend"
NAMESPACE="zheda-agent"
DEPLOYMENT="stablepay-merchant-backend"
CONTAINER="merchant-backend"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
K8S_APP="${REPO_ROOT}/infra-deployment/k8s/ack/apps/merchant-backend.yaml"

if [ -n "${1:-}" ] && [[ "$1" != "--"* ]]; then
    VERSION="$1"
else
    VERSION="v$(date +'%Y%m%d.%H%M%S')"
fi

DRY_RUN=false
if [[ "${1:-}" == "--dry" || "${2:-}" == "--dry" ]]; then
    DRY_RUN=true
fi

FULL_IMAGE="${REGISTRY}/${IMAGE_NAME}"

echo "=== Building StablePay Merchant Backend ==="
echo "Version : ${VERSION}"
echo "Image   : ${FULL_IMAGE}:${VERSION}"
echo "Dry run : ${DRY_RUN}"
echo ""

echo "[1/6] Building Docker image..."
cd "$SCRIPT_DIR"
docker build --platform linux/amd64 \
    -t "${IMAGE_NAME}:${VERSION}" \
    -t "${IMAGE_NAME}:latest" \
    .

echo "[2/6] Tagging image..."
docker tag "${IMAGE_NAME}:${VERSION}" "${FULL_IMAGE}:${VERSION}"
docker tag "${IMAGE_NAME}:${VERSION}" "${FULL_IMAGE}:latest"

if [ "${DRY_RUN}" = true ]; then
    echo ""
    echo "[DRY RUN] Skipping push and deploy."
    echo "Local tags:"
    echo "  ${FULL_IMAGE}:${VERSION}"
    echo "  ${FULL_IMAGE}:latest"
    exit 0
fi

echo "[3/6] Pushing to ACR..."
docker push "${FULL_IMAGE}:${VERSION}"
docker push "${FULL_IMAGE}:latest"

echo "[4/6] Applying ACK Deployment and Service..."
kubectl apply -f "$K8S_APP"

echo "[5/6] Updating Kubernetes deployment image..."
kubectl set image "deployment/${DEPLOYMENT}" \
    "${CONTAINER}=${FULL_IMAGE}:${VERSION}" \
    -n "$NAMESPACE"

echo "[6/6] Waiting for rollout..."
kubectl rollout status "deployment/${DEPLOYMENT}" \
    -n "$NAMESPACE" \
    --timeout=180s

echo ""
echo "=== Build & Deploy Complete ==="
echo "Image: ${FULL_IMAGE}:${VERSION}"
echo ""
echo "Verify:"
echo "  kubectl get pods -n ${NAMESPACE} -l app=${DEPLOYMENT}"
echo "  curl -sS https://ai.wenfu.cn/merchant/healthz"
echo "  curl -sS https://ai.wenfu.cn/merchant/api/v1/products"
echo ""
echo "Rollback (if needed):"
echo "  kubectl rollout undo deployment/${DEPLOYMENT} -n ${NAMESPACE}"
