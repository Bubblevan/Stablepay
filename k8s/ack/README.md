# ACK 发布目录（非 Kustomize）

此目录可直接用于云效 `KubectlApply`（`kubectl apply -f`），按职责拆分：

- `infra/all.yaml`：命名空间、密钥、配置、MySQL/Redis/RocketMQ、DB 初始化 Job
- `apps/did-service.yaml`
- `apps/payment-service.yaml`
- `apps/blockchain-adapter.yaml`
- `apps/verification-service.yaml`
- `apps/query-service.yaml`
- `apps/api-gateway.yaml`
- `platform/ingress.yaml`：ACK ALB Ingress

## 推荐流水线拆分

1. **infra 流水线**
   - `yamlPath`: `k8s/ack/infra`
   - 触发：手工或 `k8s/ack/infra/**` 变更

2. **服务流水线（6条）**
   - did: `yamlPath = k8s/ack/apps/did-service.yaml`
   - payment: `yamlPath = k8s/ack/apps/payment-service.yaml`
   - blockchain-adapter: `yamlPath = k8s/ack/apps/blockchain-adapter.yaml`
   - verification: `yamlPath = k8s/ack/apps/verification-service.yaml`
   - query: `yamlPath = k8s/ack/apps/query-service.yaml`
   - api-gateway: `yamlPath = k8s/ack/apps/api-gateway.yaml`

3. **platform 流水线**
   - `yamlPath`: `k8s/ack/platform`
   - 触发：域名、证书或网关策略变更

## 注意

- **Kubernetes 命名空间**：本目录资源统一为 `zheda-agent`。云效 `KubectlApply` 的 **NAMESPACE** 也必须填 `zheda-agent`，与 YAML 里 `metadata.namespace` 一致。  
  （这与 **ACK 集群名称** 无关；集群由 kubeconfig 选择。镜像地址里的 `stablepay-dev` 是 **ACR 仓库命名空间**，不要改成 `zheda-agent`。）
- `infra/all.yaml` 含敏感信息，建议改为云效密文变量或外部 Secret。
- 执行顺序建议：`infra -> 各服务 -> platform`。

## 基础设施镜像（避免 Docker Hub 超时）

ACK 节点常无法稳定访问 `docker.io`。请在本机（能访问 Docker Hub 的环境）把镜像同步到 ACR `stablepay-dev` 后，再 `apply infra`：

```bash
# RocketMQ（与 all.yaml 中镜像路径一致）
docker pull apache/rocketmq:5.3.2
docker tag apache/rocketmq:5.3.2 stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/rocketmq:5.3.2
docker push stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/rocketmq:5.3.2

# Redis（若尚未有 redis:7-alpine）
docker pull redis:7-alpine
docker tag redis:7-alpine stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/redis:7-alpine
docker push stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/redis:7-alpine
```

`mysql:8.0` 已按你们现有习惯使用 `stablepay-dev/mysql:8.0`。

`infra/all.yaml` 中基础设施 Pod 使用 **`imagePullSecrets: acr-secret`**（与 `apps/*` 一致）。若你环境只用 `aliyun-registry-secret`，请全文替换为对应 Secret 名。

## 强制替换旧 MySQL / Redis（selector 曾为 `app: mysql`）

若集群里已有 **selector 为 `app: mysql` / `app: redis`** 的旧 Deployment，与当前清单 **`app: stablepay-mysql`** 不一致且无法 `apply`，需先删掉旧工作负载再 apply（**删 PVC 会丢库**，请自行确认）：

```bash
kubectl -n zheda-agent delete deployment stablepay-mysql stablepay-redis --ignore-not-found
kubectl -n zheda-agent delete svc stablepay-mysql stablepay-redis --ignore-not-found
# 需要全新数据盘时再考虑：kubectl -n zheda-agent delete pvc mysql-data redis-data
kubectl apply -f k8s/ack/infra/all.yaml
```

## did / query Pod 反复重启

先查日志再判因（常见：连不上 MySQL、库未初始化、配置监听地址等）：

```bash
kubectl -n zheda-agent logs deploy/stablepay-did-service --tail=100
kubectl -n zheda-agent logs deploy/stablepay-query-service --tail=100
```
