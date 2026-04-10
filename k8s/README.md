# StablePay Kubernetes 全量部署目录

本目录用于一次性部署 StablePay 的全部基础设施与业务服务，适用于本地 Minikube 与后续迁移 ACK。

## 目录结构

- `base/namespace.yaml`：命名空间
- `base/secrets.yaml`：数据库与钱包敏感信息（请改成真实值）
- `base/configmaps.yaml`：服务配置文件与 RocketMQ broker 配置
- `base/infra.yaml`：MySQL/Redis/RocketMQ
- `base/mysql-init-job.yaml`：初始化业务库（did/payment/query/verification）
- `base/services.yaml`：did/payment/blockchain/query/verification/api-gateway
- `base/kustomization.yaml`：统一编排入口
- `ack/kustomization.yaml`：ACK 覆盖层入口
- `ack/patch-imagepullsecrets.yaml`：ACK 镜像拉取密钥补丁

## 使用说明（全量）

1. 构建并准备镜像（Minikube 本地镜像或推送到 ACR）。
2. 替换 `base/secrets.yaml` 内密码和钱包密钥。
3. 应用 `base` 清单。
4. 等待 MySQL 就绪后执行初始化 Job。
5. 通过 `api-gateway` 的 NodePort `30080` 访问。

## 镜像名（默认）

- `stablepay-did-service:latest`
- `stablepay-payment-service:latest`
- `stablepay-blockchain-adapter:latest`
- `stablepay-verification-service:latest`
- `stablepay-query-service:latest`
- `stablepay-api-gateway:latest`

迁移 ACK 时，建议改成 ACR 完整镜像地址，并使用 `k8s/ack` 覆盖层。
