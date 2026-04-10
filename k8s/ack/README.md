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
