# 我的Kubernetes学习笔记：从Hello World到StablePay微服务部署

## 写在前面

这篇笔记记录了我从完全不懂Kubernetes到能够部署公司微服务集群的完整过程。我不是运维专家，只是一个想搞清楚"容器编排到底在做什么"的开发者。

我的学习路径很简单：**先跑起来一个Hello World，然后看看真实的项目是怎么用的**。

---

## 第一部分：Kubernetes到底是什么？

在我开始折腾之前，我花了很长时间理解这三个概念的区别：

### 1. Kubernetes = 容器编排平台

想象你有10台服务器，每台上面跑了几个Docker容器。现在问题来了：
- 某个容器挂了，谁来重启它？
- 流量大了，怎么自动扩容？
- 新版本上线，怎么做滚动更新不中断服务？

**Kubernetes就是解决这些问题的"操作系统"**，它帮你管理一群服务器上的容器生命周期。

### 2. Minikube = 本地单节点的K8s

我不想一上来就搞个真集群，Minikube让我在自己的笔记本上就能跑一个"阉割版"的Kubernetes。它只有一个节点，但API和概念跟生产环境是一样的。

### 3. kubectl = 遥控器

kubectl是我和Kubernetes集群对话的命令行工具。所有操作——部署应用、查看状态、调试问题——都通过它完成。

**一句话总结**：Kubernetes是编排系统，Minikube是本地练习环境，kubectl是操作工具。

---

## 第二部分：从零搭建第一个MVP

### 第1步：启动Minikube

```bash
# 设置Docker作为驱动（Windows/Mac推荐）
minikube config set driver docker

# 启动集群
minikube start --driver=docker

# 验证状态
minikube status
```

如果一切正常，你会看到：
```
host: Running
kubelet: Running
apiserver: Running
kubeconfig: Configured
```

这说明你的本地K8s集群已经跑起来了。

### 第2步：部署第一个应用

我不急着写YAML，先用kubectl命令行感受一下：

```bash
# 创建一个Deployment（部署）
kubectl create deployment hello-k8s --image=nginx:latest

# 暴露为Service（服务）
kubectl expose deployment hello-k8s --type=NodePort --port=80

# 查看服务地址
minikube service hello-k8s --url
```

访问输出的地址（比如 http://127.0.0.1:57321），你应该能看到Nginx欢迎页。

**这里发生了什么？**

1. **Deployment**：告诉K8s"我要运行一个nginx容器，保持1个副本"
2. **Service**：为这个容器创建一个稳定的内部访问入口
3. **NodePort**：把服务暴露到主机的某个随机端口上

### 第3步：用YAML声明式配置

命令行虽然简单，但实际项目都用YAML文件。我创建了一个 `hello-nginx.yaml`：

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hello-nginx
spec:
  replicas: 2  # 运行2个副本
  selector:
    matchLabels:
      app: hello-nginx
  template:
    metadata:
      labels:
        app: hello-nginx
    spec:
      containers:
        - name: nginx
          image: nginx:latest
          ports:
            - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: hello-nginx
spec:
  selector:
    app: hello-nginx
  ports:
    - port: 80
      targetPort: 80
  type: NodePort
```

然后执行：
```bash
kubectl apply -f hello-nginx.yaml
kubectl get pods
kubectl get services
```

**这一刻我理解了K8s的核心哲学**：我描述"期望状态"（要2个nginx副本），K8s负责让实际状态匹配期望状态。

---

## 第三部分：深入理解核心概念

在继续之前，我必须搞懂这几个概念，否则看项目配置会一脸懵。

### Pod：最小的调度单元

Pod是K8s管理的最小单位，一个Pod可以包含一个或多个容器。Pod内的容器共享网络和存储空间。

**我的理解**：Pod就像是"一台虚拟的小服务器"，容器运行在上面。

```bash
# 查看所有Pod
kubectl get pods -o wide

# 进入Pod内部调试
kubectl exec -it <pod-name> -- /bin/bash

# 查看Pod日志
kubectl logs <pod-name>
```

### Deployment：管理Pod的"Desired State"

Deployment不直接管理容器，它管理的是ReplicaSet，ReplicaSet再管理Pod。

**为什么需要这一层？** 为了实现滚动更新。比如我要从v1升级到v2：
1. Deployment创建一个新的ReplicaSet，运行v2版本
2. 逐步增加v2的Pod数量，减少v1的Pod数量
3. 全程保持服务可用

```bash
# 扩容到3个副本
kubectl scale deployment hello-nginx --replicas=3

# 滚动更新镜像
kubectl set image deployment/hello-k8s nginx=nginx:1.25

# 查看 rollout 状态
kubectl rollout status deployment/hello-k8s

# 回滚到上一版本
kubectl rollout undo deployment/hello-k8s
```

### Service：稳定的网络入口

Pod的IP是动态变化的（重启后会变），Service提供了一个稳定的DNS名称和ClusterIP。

Service有几种类型：
- **ClusterIP**：只在集群内部可访问（默认）
- **NodePort**：暴露到主机的某个端口
- **LoadBalancer**：在云厂商环境创建负载均衡器
- **ExternalName**：映射到外部域名

```bash
# 集群内部访问
http://hello-nginx.default.svc.cluster.local:80
```

### Namespace：资源隔离

就像文件夹组织文件，Namespace用来隔离不同的项目或环境。

```bash
# 创建namespace
kubectl create namespace stablepay-dev

# 在指定namespace中创建资源
kubectl apply -f app.yaml -n stablepay-dev

# 查看某个namespace的Pod
kubectl get pods -n stablepay-dev
```

### ConfigMap & Secret：配置管理

**ConfigMap**存储非敏感配置，比如数据库连接地址、日志级别。

**Secret**存储敏感信息，比如密码、API Key（虽然只是base64编码，但比直接写在YAML里强）。

```bash
# 从文件创建ConfigMap
kubectl create configmap app-config --from-file=config.yaml

# 创建Secret
kubectl create secret generic db-secret \
  --from-literal=username=admin \
  --from-literal=password=secret123
```

### Ingress：HTTP层路由

Service解决了集群内通信，Ingress解决的是"外部HTTP请求如何路由到不同服务"。

比如我想让：
- `ai.wenfu.cn/api/*` → api-gateway服务
- `ai.wenfu.cn/*` → frontend服务

这就需要Ingress来配置。

---

## 第四部分：实战StablePay微服务部署

好了，基础概念搞清楚了，现在看看我们项目的真实配置。

### 4.1 项目架构概览

我们的StablePay系统包含以下服务：
- **api-gateway**：入口网关，处理HTTP请求路由
- **payment-service**：支付核心服务
- **did-service**：DID身份服务
- **blockchain-adapter**：区块链适配器
- **verification-service**：交易验证服务
- **query-service**：查询服务
- **frontend**：前端应用

### 4.2 基础资源配置

先看 `infra-deployment/k8s/base/namespace.yaml`：

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: stablepay-dev
```

这是最基本的——先划个地盘，所有资源都放在这个namespace里。

### 4.3 微服务的Deployment配置

以 `payment-service` 为例，看看 `infra-deployment/k8s/base/services.yaml` 中的配置：

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: stablepay-payment-service
  namespace: stablepay-dev
spec:
  replicas: 1
  selector:
    matchLabels:
      app: stablepay-payment-service
  template:
    metadata:
      labels:
        app: stablepay-payment-service
    spec:
      containers:
        - name: payment-service
          image: stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/payment-service:latest
          imagePullPolicy: IfNotPresent
          env:
            - name: CONFIG_PATH
              value: config/docker.yaml
          ports:
            - containerPort: 8082
              name: http
            - containerPort: 8888
              name: rpc
          volumeMounts:
            - name: payment-config
              mountPath: /app/config/docker.yaml
              subPath: docker.yaml
          readinessProbe:
            tcpSocket:
              port: 8082
            initialDelaySeconds: 10
            periodSeconds: 5
          livenessProbe:
            tcpSocket:
              port: 8082
            initialDelaySeconds: 20
            periodSeconds: 10
      volumes:
        - name: payment-config
          configMap:
            name: payment-config
```

**我学到的关键点**：

1. **双端口设计**：8082是HTTP端口，8888是RPC端口（内部服务间通信）
2. **ConfigMap挂载**：配置文件不打包在镜像里，而是通过volume挂载，实现配置与代码分离
3. **健康检查**：
   - `readinessProbe`：告诉K8s"我可以接收流量了"
   - `livenessProbe`：告诉K8s"我还活着，别重启我"

再看对应的Service配置：

```yaml
apiVersion: v1
kind: Service
metadata:
  name: stablepay-payment-service
  namespace: stablepay-dev
spec:
  selector:
    app: stablepay-payment-service
  ports:
    - name: http
      port: 8082
      targetPort: 8082
    - name: rpc
      port: 8888
      targetPort: 8888
```

Service通过 `selector` 找到对应的Pod，把流量转发过去。

### 4.4 生产环境ACK配置

在 `infra-deployment/k8s/ack/apps/` 目录下，是阿里云ACK（容器服务Kubernetes版）的配置。

以 `api-gateway.yaml` 为例：

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: stablepay-api-gateway
  namespace: zheda-agent
spec:
  replicas: 1
  selector:
    matchLabels:
      app: stablepay-api-gateway
  template:
    metadata:
      labels:
        app: stablepay-api-gateway
    spec:
      imagePullSecrets:
        - name: acr-secret  # 阿里云容器镜像仓库的拉取密钥
      containers:
        - name: api-gateway
          image: stablepay-registry.cn-shanghai.cr.aliyuncs.com/stablepay-dev/api-gateway:latest
          imagePullPolicy: IfNotPresent
          ports:
            - containerPort: 8080
              name: http
---
apiVersion: v1
kind: Service
metadata:
  name: stablepay-api-gateway
  namespace: zheda-agent
spec:
  type: ClusterIP
  selector:
    app: stablepay-api-gateway
  ports:
    - name: http
      port: 8080
      targetPort: 8080
```

**注意几个生产环境的关键点**：

1. **namespace是zheda-agent**：生产环境用不同的namespace
2. **imagePullSecrets**：私有镜像仓库需要配置拉取密钥
3. **ClusterIP类型**：不直接暴露到公网，通过Ingress统一入口

### 4.5 Ingress配置：流量入口

 `infra-deployment/k8s/ack/platform/ingress.yaml` 是整个系统的流量入口：

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: stablepay-ingress
  namespace: zheda-agent
  annotations:
    alb.ingress.kubernetes.io/albconfig.name: "alb"
    alb.ingress.kubernetes.io/backend-protocol: "HTTP"
    alb.ingress.kubernetes.io/listen-ports: '[{"HTTP": 80}, {"HTTPS": 443}]'
    alb.ingress.kubernetes.io/healthcheck-enabled: "true"
    alb.ingress.kubernetes.io/healthcheck-path: "/healthz"
    alb.ingress.kubernetes.io/cors-allow-origin: "*"
    alb.ingress.kubernetes.io/certificate-id: "21374537-cn-hangzhou"
    alb.ingress.kubernetes.io/ssl-redirect: "true"
spec:
  ingressClassName: alb
  rules:
    - host: ai.wenfu.cn
      http:
        paths:
          - path: /api
            pathType: Prefix
            backend:
              service:
                name: stablepay-api-gateway
                port:
                  number: 8080
          - path: /pay
            pathType: Prefix
            backend:
              service:
                name: stablepay-api-gateway
                port:
                  number: 8080
          - path: /
            pathType: Prefix
            backend:
              service:
                name: stablepay-frontend
                port:
                  number: 80
```

**这段配置告诉我**：

1. **ALB（阿里云负载均衡）**：通过 `alb.ingress.kubernetes.io` 注解配置
2. **HTTPS配置**：证书ID、强制HTTP转HTTPS
3. **路径路由**：
   - `/api/*` 和 `/pay/*` 转发到api-gateway
   - `/*` 转发到frontend
4. **健康检查**：配置 `/healthz` 作为健康检查端点

### 4.6 Secret管理敏感信息

看看 `blockchain-adapter` 的配置，它需要访问热钱包：

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: stablepay-blockchain-adapter
  namespace: stablepay-dev
spec:
  template:
    spec:
      containers:
        - name: blockchain-adapter
          volumeMounts:
            - name: hotwallet-secret
              mountPath: /app/conf/hotwallet.json
              subPath: hotwallet.json
      volumes:
        - name: hotwallet-secret
          secret:
            secretName: stablepay-secrets
```

Secret `stablepay-secrets` 可能是这样创建的：

```bash
kubectl create secret generic stablepay-secrets \
  --from-file=hotwallet.json \
  --namespace=stablepay-dev
```

### 4.7 环境变量与Secret引用

`verification-service` 展示了如何从Secret读取数据库密码：

```yaml
env:
  - name: MYSQL_HOST
    value: stablepay-mysql
  - name: MYSQL_PORT
    value: "3306"
  - name: MYSQL_USER
    valueFrom:
      secretKeyRef:
        name: stablepay-secrets
        key: mysql-user
  - name: MYSQL_PASSWORD
    valueFrom:
      secretKeyRef:
        name: stablepay-secrets
        key: mysql-password
```

这种方式比直接把密码写在YAML里安全多了。

---

## 第五部分：常用操作命令备忘

### 查看资源状态

```bash
# 查看所有Pod
kubectl get pods -n zheda-agent

# 查看Pod详情（排查问题时常用）
kubectl describe pod <pod-name> -n zheda-agent

# 查看日志
kubectl logs <pod-name> -n zheda-agent

# 实时跟踪日志
kubectl logs -f <pod-name> -n zheda-agent

# 查看Deployment状态
kubectl get deployments -n zheda-agent

# 查看Service
kubectl get svc -n zheda-agent

# 查看Ingress
kubectl get ingress -n zheda-agent
```

### 调试与排查

```bash
# 进入容器内部
kubectl exec -it <pod-name> -n zheda-agent -- /bin/sh

# 从本地复制文件到Pod
kubectl cp local-file.txt <pod-name>:/path/in/pod -n zheda-agent

# 查看Pod事件（排查启动失败）
kubectl get events -n zheda-agent --sort-by='.lastTimestamp'

# 查看资源使用（需要安装metrics-server）
kubectl top pods -n zheda-agent
```

### 部署与更新

```bash
# 应用配置
kubectl apply -f infra-deployment/k8s/ack/apps/

# 删除资源
kubectl delete -f infra-deployment/k8s/ack/apps/payment-service.yaml

# 重启Deployment（会重新创建Pod）
kubectl rollout restart deployment/stablepay-payment-service -n zheda-agent

# 查看 rollout 历史
kubectl rollout history deployment/stablepay-payment-service -n zheda-agent

# 回滚到指定版本
kubectl rollout undo deployment/stablepay-payment-service -n zheda-agent --to-revision=2
```

### 端口转发（本地调试神器）

```bash
# 把集群里的服务映射到本地端口
kubectl port-forward svc/stablepay-api-gateway 8080:8080 -n zheda-agent

# 现在访问 localhost:8080 就能访问集群里的服务了
```

---

## 第六部分：我踩过的坑

### 1. Pod一直Pending

**原因**：通常是资源不足（CPU/内存），或者镜像拉取失败。

**排查**：
```bash
kubectl describe pod <pod-name> -n zheda-agent
# 看Events部分
```

### 2. 镜像拉取失败（ImagePullBackOff）

**原因**：
- 镜像名称/标签写错了
- 私有仓库没有配置imagePullSecrets
- 网络问题拉取不到

### 3. 服务访问不通

**排查路径**：
1. Pod是否Running且Ready？`kubectl get pods`
2. Service的selector是否匹配Pod的label？
3. 端口是否正确？
4. 如果是Ingress，证书和路径配置是否正确？

### 4. ConfigMap/Secret修改后Pod没更新

ConfigMap和Secret的修改不会自动触发Pod重启。需要手动重启：
```bash
kubectl rollout restart deployment/<name> -n zheda-agent
```

或者使用一些工具（如Reloader）来自动重启。

---

## 写在最后

学习K8s的过程就是不断"从抽象到具体"的过程：

1. **第一阶段**：搞懂Pod、Deployment、Service这些抽象概念
2. **第二阶段**：跑通Hello World，理解YAML的语法
3. **第三阶段**：研究真实项目的配置，看 production-ready 的部署长什么样
4. **第四阶段**：自己排查问题、优化配置

对于我这个后端开发来说，我不需要成为K8s专家，但至少要能：
- 看懂项目的K8s配置
- 排查基本的部署问题
- 知道怎么修改配置、重新部署

希望这篇笔记对你也有帮助。

---

## 参考资源

- [Kubernetes官方文档](https://kubernetes.io/zh-cn/docs/concepts/overview/)
- [Minikube官方文档](https://minikube.sigs.k8s.io/docs/start/)
- [kubectl命令参考](https://kubernetes.io/zh/docs/reference/kubectl/)
