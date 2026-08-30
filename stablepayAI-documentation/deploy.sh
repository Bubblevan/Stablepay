#!/bin/bash
set -e

# 配置
REGISTRY="stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev"
IMAGE_NAME="stablepay-docs"

# 版本号：优先使用传入参数，否则生成时间戳版本 (v20250603.153534)
if [ -n "$1" ]; then
    VERSION="$1"
else
    VERSION="v$(date +%Y%m%d.%H%M%S)"
fi

echo "=== Building StablePay Documentation ==="
echo "Version: $VERSION"
echo ""

# 登录阿里云 ACR（如果需要）
# docker login --username=xxx stablepay-registry.cn-shanghai.cr.aliyuncs.com

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
kubectl set image deployment/stablepay-docs \
    docs=${REGISTRY}/${IMAGE_NAME}:${VERSION} \
    -n zheda-agent

# 等待 rollout 完成
echo "Waiting for rollout to complete..."
kubectl rollout status deployment/stablepay-docs -n zheda-agent

echo ""
echo "=== Build & Deploy Complete ==="
echo "Image: ${REGISTRY}/${IMAGE_NAME}:${VERSION}"
echo ""
echo "Apply ingress (first time or after route changes):"
echo "  kubectl apply -f ../infra-deployment/k8s/ack/platform/ingress.yaml"
echo ""
echo "Verify deployment:"
echo "  kubectl get pods -n zheda-agent -l app=stablepay-docs"
echo "  curl -sI https://ai.wenfu.cn/docs/ | grep -i cache-control"
echo "  curl -s https://ai.wenfu.cn/docs/ | grep -o '/docs/_next[^\"]*' | head -1"
echo ""
echo "If browser still shows old content: hard refresh (Ctrl+Shift+R) or incognito once."
echo ""
echo "Rollback command (if needed):"
echo "  kubectl rollout undo deployment/stablepay-docs -n zheda-agent"
