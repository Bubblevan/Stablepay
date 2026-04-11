# ACK 发布目录（非 Kustomize）

此目录可直接用于云效 `KubectlApply`（`kubectl apply -f`），按职责拆分：

- `infra/all.yaml`：命名空间、密钥、配置、MySQL/Redis/RocketMQ、DB 初始化 Job
- `apps/did-service.yaml`
- `apps/payment-service.yaml`
- `apps/blockchain-adapter.yaml`
- `apps/verification-service.yaml`
- `apps/query-service.yaml`
- `apps/api-gateway.yaml`
- `apps/stablepay-frontend.yaml`：Vite 静态站点（Nginx）
- `platform/ingress.yaml`：ACK ALB Ingress（`/` → 前端，`/api` `/pay` `/verify` 等 → 网关）

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
   - frontend: `yamlPath = k8s/ack/apps/stablepay-frontend.yaml`（镜像需在 `stablepay-frontend/` 用 Dockerfile 构建并推送到 `stablepay-dev/stablepay-frontend`）

3. **platform 流水线**
   - `yamlPath`: `k8s/ack/platform`
   - 触发：域名、证书或网关策略变更

## 注意

- **Kubernetes 命名空间**：本目录资源统一为 `zheda-agent`。云效 `KubectlApply` 的 **NAMESPACE** 也必须填 `zheda-agent`，与 YAML 里 `metadata.namespace` 一致。  
  （这与 **ACK 集群名称** 无关；集群由 kubeconfig 选择。镜像地址里的 `stablepay-dev` 是 **ACR 仓库命名空间**，不要改成 `zheda-agent`。）
- `infra/all.yaml` 含敏感信息，建议改为云效密文变量或外部 Secret。
- 执行顺序建议：`infra -> 各服务（含前端）-> platform`。

## 域名与 DNS（为什么在云解析里搜不到 `ai.wenfu.cn`）

- **云解析 DNS** 里一般只登记**主域名**（例如 `wenfu.cn`）。`ai` 是**主机记录**，在 `wenfu.cn` 的解析列表里新增一条 **A 记录** 或 **CNAME**，主机记录填 `ai`，不是单独再添加一个叫 `ai.wenfu.cn` 的「域名」。
- 若 `wenfu.cn` **不在当前阿里云账号**（域名在别的注册商），要到**域名注册商控制台**做解析，或在注册商处把 DNS 服务器改到阿里云后再在云解析里配。
- 若曾用 **「动态解析」** 把域名指到 ECS，有时是 ECS 上跑的 **DDNS 客户端** 或脚本在改解析，不一定在云解析控制台当前账号里；可登录域名实际托管处核对 **A/CNAME** 最终指向是 **ECS 公网 IP** 还是 **ALB 域名**。
- 切到 ACK 后：建议把 `ai` 的解析改为 **CNAME 到 Ingress 对应的 ALB 域名**（`kubectl -n zheda-agent get ingress stablepay-ingress` 的 `ADDRESS` / 控制台 ALB 实例域名），再停用 ECS 上的 Nginx。

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
