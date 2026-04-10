# ACK 部署说明（基于 kustomize）

## 你需要先准备

- ACK 集群访问权限（可用 `kubectl get nodes`）
- ACR 推送与拉取权限
- 命名空间与本目录一致：`stablepay-dev`
- 已创建镜像拉取密钥：`acr-secret`

## 本目录做了什么

- `patch-imagepullsecrets.yaml`：所有业务 Deployment 注入 `acr-secret`
- `patch-storageclass.yaml`：MySQL/Redis PVC 指定 `alicloud-disk-essd`
- `patch-gateway-service-clusterip.yaml`：将网关 Service 改为 `ClusterIP`
- `ingress.yaml`：通过 ALB Ingress 暴露网关服务

## 部署

```bash
kubectl apply -k ./k8s/ack
kubectl -n stablepay-dev get pods
kubectl -n stablepay-dev get ingress
```

## 上线前必须修改

- `ingress.yaml` 的 `host` 改为真实域名
- `patch-storageclass.yaml` 按实际规格调整磁盘类型和容量
- 所有业务镜像 tag 建议改为版本号，不要长期使用 `latest`
