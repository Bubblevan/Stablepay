#!/bin/bash
# StablePay Payment Service - build image and deploy to ACK (zheda-agent)
#
# Usage: ./deploy.sh              # auto-generate timestamp version
#        ./deploy.sh v20260712.1  # use explicit version

set -euo pipefail

REGISTRY="stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev"
IMAGE_NAME="payment-service"
NAMESPACE="zheda-agent"
DEPLOYMENT="stablepay-payment-service"
CONTAINER="payment-service"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
K8S_APP="${REPO_ROOT}/infra-deployment/k8s/ack/apps/payment-service.yaml"

if [ $# -gt 0 ] && [ -n "$1" ]; then
    VERSION="$1"
else
    VERSION="v$(date +'%Y%m%d.%H%M%S')"
fi

FULL_IMAGE="${REGISTRY}/${IMAGE_NAME}:${VERSION}"

echo "=== Building StablePay Payment Service ==="
echo "Version: ${VERSION}"
echo ""

echo "[1/5] Building Docker image..."
cd "$REPO_ROOT"
docker build -f "${SCRIPT_DIR}/Dockerfile" -t "${IMAGE_NAME}:${VERSION}" .

echo "[2/5] Tagging image..."
docker tag "${IMAGE_NAME}:${VERSION}" "${REGISTRY}/${IMAGE_NAME}:${VERSION}"
docker tag "${IMAGE_NAME}:${VERSION}" "${REGISTRY}/${IMAGE_NAME}:latest"

echo "[3/5] Pushing to ACR..."
docker push "${REGISTRY}/${IMAGE_NAME}:${VERSION}"
docker push "${REGISTRY}/${IMAGE_NAME}:latest"

echo "[4/5] Applying ACK app manifest (payment-config ConfigMap mount stays authoritative)..."
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
echo "Verify runtime config source:"
echo "  kubectl exec -n ${NAMESPACE} deploy/${DEPLOYMENT} -- sh -c 'printenv | grep CONFIG_PATH; ls -l /app/config; sed -n \"1,80p\" /app/config/docker.yaml'"
echo ""
echo "Verify service health:"
echo "  kubectl exec -n ${NAMESPACE} deploy/${DEPLOYMENT} -- wget -qO- http://127.0.0.1:8082/health"
echo ""
echo "Rollback (if needed):"
echo "  kubectl rollout undo deployment/${DEPLOYMENT} -n ${NAMESPACE}"
