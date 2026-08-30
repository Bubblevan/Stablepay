# Payment Service 运维手册

## 目录

1. [部署指南](#部署指南)
2. [配置说明](#配置说明)
3. [监控告警](#监控告警)
4. [故障处理](#故障处理)
5. [数据维护](#数据维护)

---

## 部署指南

### 环境要求

| 组件 | 版本 | 说明 |
|------|------|------|
| Go | 1.21+ | 运行环境 |
| MySQL | 8.0+ | 交易数据存储 |
| Redis | 7.0+ | 缓存与分布式锁 |
| RocketMQ | 5.0+ | 消息队列 |

### 编译构建

```bash
# 克隆代码
git clone https://github.com/stablepay/payment-service.git
cd payment-service

# 下载依赖
go mod download

# 编译
CGO_ENABLED=0 GOOS=linux go build -o payment-service cmd/payment-service/main.go
```

### Docker 部署

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o payment-service cmd/payment-service/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/payment-service .
COPY --from=builder /app/config ./config
EXPOSE 8080 8888
CMD ["./payment-service"]
```

构建镜像：

```bash
docker build -t stablepay/payment-service:v1.0.0 .
```

### Kubernetes 部署

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payment-service
  namespace: stablepay
spec:
  replicas: 3
  selector:
    matchLabels:
      app: payment-service
  template:
    metadata:
      labels:
        app: payment-service
    spec:
      containers:
      - name: payment-service
        image: stablepay/payment-service:v1.0.0
        ports:
        - containerPort: 8080
        - containerPort: 8888
        env:
        - name: CONFIG_PATH
          value: "/config/config.yaml"
        volumeMounts:
        - name: config
          mountPath: /config
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
      volumes:
      - name: config
        configMap:
          name: payment-service-config
---
apiVersion: v1
kind: Service
metadata:
  name: payment-service
  namespace: stablepay
spec:
  selector:
    app: payment-service
  ports:
  - name: http
    port: 8080
    targetPort: 8080
  - name: rpc
    port: 8888
    targetPort: 8888
```

---

## 配置说明

### 核心配置项

```yaml
# 支付业务配置
payment:
  # 交易超时时间（分钟）- 超过此时间未确认的交易将被标记为超时
  timeout_minutes: 5

  # 最大重试次数 - 链上交易失败后的最大重试次数
  max_retry_count: 3

  # 最大支付金额（USDC）- 服务端硬限制
  max_amount_usdc: "1000.00"

  # 轮询链上状态间隔（秒）
  poll_interval_seconds: 3

  # 最大轮询次数
  max_poll_count: 20

# 安全配置
security:
  # 签名有效期（分钟）- 超过此时间的签名将被拒绝
  signature_ttl_minutes: 5

  # Nonce 缓存时间（分钟）
  nonce_cache_minutes: 10

  # 幂等键有效期（分钟）
  idempotency_key_ttl_minutes: 30

# 限流配置
rate_limit:
  # IP 级别限流（每分钟）
  ip_limit_per_minute: 100

  # DID 级别限流（每分钟）
  did_limit_per_minute: 50

  # 支付接口限流（每分钟）
  payment_limit_per_minute: 10
```

### 环境变量覆盖

支持通过环境变量覆盖配置文件：

```bash
export PAYMENT_SERVICE_DATABASE_MYSQL_HOST=prod-mysql.example.com
export PAYMENT_SERVICE_DATABASE_MYSQL_PASSWORD=secret
export PAYMENT_SERVICE_REDIS_HOST=prod-redis.example.com
```

---

## 监控告警

### 关键指标

| 指标 | 说明 | 告警阈值 |
|------|------|----------|
| http_requests_total | HTTP 请求总数 | - |
| http_request_duration_seconds | HTTP 请求耗时 | P99 > 2s |
| payment_success_rate | 支付成功率 | < 95% |
| payment_processing_time | 支付处理时间 | > 30s |
| blockchain_rpc_errors | 区块链 RPC 错误数 | > 10/分钟 |
| mysql_connections | MySQL 连接数 | > 80% |
| redis_hit_rate | Redis 命中率 | < 90% |

### 日志级别

```yaml
log:
  level: info  # debug, info, warn, error
  format: json
```

### 健康检查

```bash
# HTTP 健康检查
curl http://localhost:8080/health

# 预期响应
{
  "status": "healthy",
  "service": "payment-service"
}
```

---

## 故障处理

### 常见故障

#### 1. 余额不足错误增多

**现象**: 大量支付请求返回 `20001 insufficient balance`

**处理**:
1. 检查区块链网络状态
2. 确认余额查询接口是否正常
3. 检查是否有用户在短时间内多次尝试支付

#### 2. 链上交易超时

**现象**: 支付状态长时间停留在 `PENDING`

**处理**:
1. 检查 Blockchain Adapter 服务状态
2. 检查 Solana 网络拥堵情况
3. 查看支付交易哈希在链上的状态
4. 手动补偿机制：查询链上状态后更新数据库

```sql
-- 查询超时交易
SELECT * FROM payment_transactions
WHERE status = 1  -- PENDING
  AND created_at < DATE_SUB(NOW(), INTERVAL 10 MINUTE);
```

#### 3. 重复支付

**现象**: 同一笔交易被多次执行

**处理**:
1. 检查幂等性键是否正确传递
2. 检查幂等性表是否有异常
3. 手动修复：标记重复交易为失败

#### 4. MQ 消息堆积

**现象**: RocketMQ 消息消费延迟

**处理**:
1. 检查消费者服务状态
2. 查看消息堆积数量
3. 必要时增加消费者实例

### 紧急预案

#### 服务降级

在极端情况下，可以启用服务降级：

```yaml
# 暂停新支付
payment:
  enabled: false
```

#### 数据修复

```sql
-- 手动标记交易为失败
UPDATE payment_transactions
SET status = 4, error_code = 'MANUAL_RECOVER', error_message = '...'
WHERE tx_id = '...';

-- 重新触发支付成功事件
-- 通知下游服务更新购买记录
```

---

## 数据维护

### 定时任务

#### 1. 清理过期幂等性记录

```sql
-- 每天执行
DELETE FROM payment_idempotency_keys
WHERE expires_at < DATE_SUB(NOW(), INTERVAL 1 DAY);
```

#### 2. 归档历史交易

```sql
-- 归档 3 个月前的已完成交易
CREATE TABLE payment_transactions_2024_q1 LIKE payment_transactions;

INSERT INTO payment_transactions_2024_q1
SELECT * FROM payment_transactions
WHERE created_at < '2024-04-01'
  AND status IN (3, 4, 5);  -- COMPLETED, FAILED, CANCELLED

DELETE FROM payment_transactions
WHERE created_at < '2024-04-01'
  AND status IN (3, 4, 5);
```

#### 3. 数据一致性对账

```sql
-- 检查链上成功但数据库未更新的交易
SELECT * FROM payment_transactions
WHERE status = 1  -- PENDING
  AND tx_hash IS NOT NULL
  AND created_at < DATE_SUB(NOW(), INTERVAL 30 MINUTE);
```

### 备份策略

| 数据 | 备份频率 | 保留期限 |
|------|----------|----------|
| MySQL | 每日全量 + 实时 binlog | 30 天 |
| Redis | 每小时 RDB | 7 天 |
| 配置文件 | 每次变更 | 永久 |

---

## 联系方式

- 技术支持: dev@stablepay.co
- 紧急热线: +86-xxx-xxxx-xxxx
