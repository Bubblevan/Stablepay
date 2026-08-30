# StablePay 微服务部署指南

> 包含 Kitex 代码生成 和 Docker 容器化部署说明

---

## 目录

1. [Kitex 代码生成](#一-kitex-代码生成)
2. [Dockerfile 说明](#二-dockerfile-说明)
3. [部署命令](#三-部署命令)
4. [常见问题](#四-常见问题)

---

## 一、Kitex 代码生成

### 1.1 安装 Kitex 工具

```bash
# 安装 kitex 命令行工具
go install github.com/cloudwego/kitex/tool/cmd/kitex@latest

# 验证安装
kitex -v
```

### 1.2 模块名对照表

每个服务的 `go.mod` 中定义的模块名不同，生成代码时必须匹配：

| 服务 | go.mod 模块名 | Kitex 命令 |
|------|--------------|-----------|
| did-service | `github.com/stablepay/did-service` | `kitex -module github.com/stablepay/did-service ...` |
| payment-service | `github.com/stablepay/payment-service` | `kitex -module github.com/stablepay/payment-service ...` |
| blockchain-adapter | `github.com/stablepay/blockchain-adapter` | `kitex -module github.com/stablepay/blockchain-adapter ...` |
| verification-service | `verification-service` | `kitex -module verification-service ...` |
| Query-service | `query-service` | `kitex -module query-service ...` |

### 1.3 生成代码命令

```bash
# 进入对应服务目录后执行：

# ========== did-service ==========
cd ~/did-service
kitex -module github.com/stablepay/did-service \
      -service did_service \
      ~/stablepay-idl/idl/did-service.thrift

# ========== payment-service ==========
cd ~/payment-service
kitex -module github.com/stablepay/payment-service \
      -service payment_service \
      ~/stablepay-idl/idl/payment-service.thrift

# ========== blockchain-adapter ==========
cd ~/blockchain-adapter
kitex -module github.com/stablepay/blockchain-adapter \
      -service blockchain_adapter \
      ~/stablepay-idl/idl/blockchain-adapter.thrift

# ========== verification-service ==========
cd ~/verification-service
kitex -module verification-service \
      -service verification_service \
      ~/stablepay-idl/idl/verification-service.thrift

# ========== Query-service ==========
cd ~/query-service
kitex -module query-service \
      -service query_service \
      ~/stablepay-idl/idl/query-service.thrift
```

### 1.4 常见错误

**错误 1：模块名不匹配**
```
[ERROR] the module name given by the '-module' option ('did-service')
is not consist with the name defined in go.mod ('github.com/stablepay/did-service')
```
**解决**：使用完整的模块名 `github.com/stablepay/did-service`，不是简写的 `did-service`。

**错误 2：kitex_gen 目录已存在**
```
[ERROR] target generation directory './kitex_gen' already exists
```
**解决**：先删除旧的生成代码
```bash
rm -rf kitex_gen
kitex -module xxx ...
```

---

## 二、Dockerfile 说明

### 2.1 各服务 Dockerfile 位置

| 服务 | Dockerfile 位置 | 端口 | 说明 |
|------|----------------|------|------|
| api-gateway | `api-gateway/Dockerfile` | 8080 | 已有 |
| did-service | `did-service/Dockerfile` | 8081 | 已创建 |
| payment-service | `payment-service/Dockerfile` | 8080/8888 | 已有 |
| verification-service | `verification-service/Dockerfile` | 8085 | 已创建 |
| query-service | `query-service/Dockerfile` | 8084 | 已创建 |
| blockchain-adapter | `blockchain-adapter/Dockerfile` | 8888 | 已创建 |

### 2.2 Dockerfile 结构说明

所有 Dockerfile 都采用 **多阶段构建**（Multi-stage Build）：

```dockerfile
# 第一阶段：构建
FROM golang:1.21-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o service-name .

# 第二阶段：运行（只包含二进制，体积小）
FROM alpine:latest
WORKDIR /app
COPY --from=builder /build/service-name .
EXPOSE 8080
CMD ["./service-name"]
```

**优点**：
- 构建镜像包含完整 Go 环境（大）
- 运行镜像只包含二进制（小，约 20-50MB）
- 更安全（无编译工具）

### 2.3 各服务特殊配置

**SQLite 服务**（verification/query）：
```dockerfile
# 需要数据卷持久化数据库
VOLUME ["/data"]
RUN mkdir -p /data
```

**需要配置文件的服务**（did/payment/api-gateway）：
```dockerfile
# 复制配置目录
COPY --from=builder /build/conf ./conf
# 或
COPY --from=builder /build/configs ./configs
```

**Blockchain-Adapter**（需要热钱包密钥）：
```dockerfile
# 热钱包密钥文件通过 volume 挂载
VOLUME ["/secure"]
```

---

## 三、部署命令

### 3.1 本地构建并推送

```bash
# 1. 进入本地仓库目录
cd D:\MyLab\StablePay

# 2. 提交
git add .
git commit
git push

# 3. 在远程服务器拉取
cd ~
git pull origin main  # 或你的分支
```

### 3.2 远程服务器部署

```bash
# ========== 构建所有服务镜像 ==========

docker build -t stablepay/api-gateway ./api-gateway
docker build -t stablepay/did-service ./did-service
docker build -t stablepay/payment-service ./payment-service
docker build -t stablepay/verification-service ./verification-service
docker build -t stablepay/query-service ./query-service
docker build -t stablepay/blockchain-adapter ./blockchain-adapter

# ========== 查看构建的镜像 ==========
docker images | grep stablepay

# ========== 启动所有服务 ==========

# 创建网络（让容器互通）
docker network create stablepay-network

# 启动 did-service
docker run -d \
  --name did-service \
  --network stablepay-network \
  -p 8081:8081 \
  stablepay/did-service

# 启动 payment-service
docker run -d \
  --name payment-service \
  --network stablepay-network \
  -p 8082:8082 \
  stablepay/payment-service

# 启动 verification-service（挂载数据卷）
docker run -d \
  --name verification-service \
  --network stablepay-network \
  -p 8085:8085 \
  -v verification-data:/data \
  stablepay/verification-service

# 启动 query-service（挂载数据卷）
docker run -d \
  --name query-service \
  --network stablepay-network \
  -p 8084:8084 \
  -v query-data:/data \
  stablepay/query-service

# 启动 blockchain-adapter（挂载配置和热钱包）
docker run -d \
  --name blockchain-adapter \
  --network stablepay-network \
  -p 8888:8888 \
  -v /root/blockchain-adapter/conf:/app/conf \
  -v /secure:/secure \
  stablepay/blockchain-adapter

# 启动 api-gateway
docker run -d \
  --name api-gateway \
  --network stablepay-network \
  -p 8080:8080 \
  -v /root/api-gateway/configs:/app/configs \
  stablepay/api-gateway

# ========== 查看运行状态 ==========
docker ps | grep stablepay

# ========== 查看日志 ==========
docker logs -f verification-service
docker logs -f query-service

# ========== 停止服务 ==========
docker stop verification-service query-service did-service payment-service blockchain-adapter api-gateway

# ========== 删除容器 ==========
docker rm verification-service query-service did-service payment-service blockchain-adapter api-gateway
```

### 3.3 使用 Docker Compose（推荐）

创建 `~/docker-compose.yml`：

```yaml
version: '3.8'

services:
  did-service:
    build: ./did-service
    ports:
      - "8081:8081"
    networks:
      - stablepay

  payment-service:
    build: ./payment-service
    ports:
      - "8082:8082"
    networks:
      - stablepay
    environment:
      - BLOCKCHAIN_ADAPTER_ADDR=blockchain-adapter:8888

  verification-service:
    build: ./verification-service
    ports:
      - "8085:8085"
    volumes:
      - verification-data:/data
    networks:
      - stablepay

  query-service:
    build: ./query-service
    ports:
      - "8084:8084"
    volumes:
      - query-data:/data
    networks:
      - stablepay

  blockchain-adapter:
    build: ./blockchain-adapter
    ports:
      - "8888:8888"
    volumes:
      - ./blockchain-adapter/conf:/app/conf
      - /secure:/secure
    networks:
      - stablepay

  api-gateway:
    build: ./api-gateway
    ports:
      - "8080:8080"
    volumes:
      - ./api-gateway/configs:/app/configs
    networks:
      - stablepay
    depends_on:
      - did-service
      - payment-service
      - verification-service
      - query-service

volumes:
  verification-data:
  query-data:

networks:
  stablepay:
    driver: bridge
```

然后一键启动：
```bash
cd ~
docker-compose up -d --build

# 查看状态
docker-compose ps

# 查看日志
docker-compose logs -f

# 停止所有服务
docker-compose down
```

---

## 四、常见问题

### Q1: 容器启动后马上退出？

**检查日志**：
```bash
docker logs verification-service
```

**常见原因**：
- 缺少配置文件 → 挂载配置目录
- 端口被占用 → 更换宿主机端口 `-p 8086:8085`
- 数据库目录权限 → 检查 volume 挂载

### Q2: 容器之间无法通信？

**确保在同一网络**：
```bash
# 检查网络
docker network inspect stablepay-network

# 容器间使用服务名访问
# 如：verification-service 访问 payment-service
# 地址应为：payment-service:8082
```

### Q3: SQLite 数据丢失？

**必须使用 volume**：
```bash
docker run -v verification-data:/data stablepay/verification-service

# 不要这样（容器删除数据就丢了）
docker run stablepay/verification-service
```

### Q4: 如何更新服务？

```bash
# 1. 拉取最新代码
git pull

# 2. 重新构建
docker build -t stablepay/verification-service ./verification-service

# 3. 重启容器
docker stop verification-service
docker rm verification-service
docker run -d ... stablepay/verification-service

# 或用 docker-compose
docker-compose up -d --build verification-service
```

---

## 快速参考卡

```bash
# 生成代码
kitex -module $(head -1 go.mod | cut -d' ' -f2) -service xxx_service ~/stablepay-idl/idl/xxx.thrift

# 构建镜像
docker build -t stablepay/xxx-service ./xxx-service

# 运行容器
docker run -d -p 8080:8080 --name xxx stablepay/xxx-service

# 查看日志
docker logs -f xxx

# 进入容器
docker exec -it xxx sh
```
