#!/bin/bash
set -e

# 配置
REGISTRY="stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev"
IMAGE_NAME="stablepay-docs"
VERSION=${1:-latest}

echo "=== Building StablePay Documentation ==="

# 登录阿里云 ACR（如果需要）
# docker login --username=xxx stablepay-registry.cn-shanghai.cr.aliyuncs.com

# 构建镜像
echo "Building Docker image..."
docker build -t ${IMAGE_NAME}:${VERSION} .

# 打标签
echo "Tagging image..."
docker tag ${IMAGE_NAME}:${VERSION} ${REGISTRY}/${IMAGE_NAME}:${VERSION}

# 推送到 ACR
echo "Pushing to ACR..."
docker push ${REGISTRY}/${IMAGE_NAME}:${VERSION}

echo "=== Build & Push Complete ==="
echo "Image: ${REGISTRY}/${IMAGE_NAME}:${VERSION}"
echo ""
echo "Next steps:"
echo "1. kubectl apply -f ../infra-deployment/k8s/ack/apps/stablepay-docs.yaml"
echo "2. kubectl apply -f ../infra-deployment/k8s/ack/platform/ingress.yaml"
