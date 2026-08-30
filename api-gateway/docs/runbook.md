# 运行手册

## 1. 环境要求

- Go 1.21+
- 可选 Redis（限流/防重放分布式模式）

## 2. 安装依赖

```bash
go mod tidy
```

## 3. 启动服务

```bash
go run ./cmd/api-gateway -config configs/config.yaml
```

## 4. 关键配置

- `server.address`：监听地址
- `security.allowed_api_keys`：API Key 白名单
- `security.allowed_timestamp_skew_sec`：签名时间窗
- `security.require_nonce`：是否强制 nonce
- `redis.enabled`：是否启用 Redis
- `routes`：路由策略、鉴权模式、限流阈值

## 5. 验证

```bash
curl http://127.0.0.1:8080/healthz
curl "http://127.0.0.1:8080/pay?skill=did:solana:dev1&price=5.00"
```
