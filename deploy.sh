#!/bin/bash
set -e

# 配置
REGISTRY="stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev"
IMAGE_NAME="stablepay-frontend"

# 版本号：优先使用传入参数，否则生成时间戳版本 (v20250101.120000)
if [ -n "$1" ]; then
    VERSION="$1"
else
    VERSION="v$(date +%Y%m%d.%H%M%S)"
fi

echo "=== Building StablePay Frontend ==="
echo "Version: $VERSION"
echo ""

# 构建镜像
echo "[1/4] Building Docker image..."
docker build -t ${IMAGE_NAME}:${VERSION} .

# 打标签
echo "[2/4] Tagging image..."
docker tag ${IMAGE_NAME}:${VERSION} ${REGISTRY}/${IMAGE_NAME}:${VERSION}
docker tag ${IMAGE_NAME}:${VERSION} ${REGISTRY}/${IMAGE_NAME}:latest

# 推送到 ACR
echo "[3/4] Pushing to ACR..."
docker push ${REGISTRY}/${IMAGE_NAME}:${VERSION}
docker push ${REGISTRY}/${IMAGE_NAME}:latest

echo "[4/4] Updating Kubernetes deployment..."
# 使用 kubectl set image 更新指定版本
kubectl set image deployment/stablepay-frontend \
    frontend=${REGISTRY}/${IMAGE_NAME}:${VERSION} \
    -n zheda-agent

# 等待 rollout 完成
echo "Waiting for rollout to complete..."
kubectl rollout status deployment/stablepay-frontend -n zheda-agent

echo ""
echo "=== Build & Deploy Complete ==="
echo "Image: ${REGISTRY}/${IMAGE_NAME}:${VERSION}"
echo ""
echo "Verify (compare JS hash in index.html with cluster):"
echo "  curl -sI https://ai.wenfu.cn/ | grep -i cache-control"
echo "  curl -s https://ai.wenfu.cn/ | grep -o '/assets/[^\"]*' | head -1"
echo "  kubectl exec -n zheda-agent deploy/stablepay-frontend -- grep -o '/assets/[^\"]*' /usr/share/nginx/html/index.html | head -1"
echo ""
echo "If browser still shows old UI: hard refresh (Ctrl+Shift+R) or incognito once."
echo ""
echo "Rollback command (if needed):"
echo "  kubectl rollout undo deployment/stablepay-frontend -n zheda-agent"
