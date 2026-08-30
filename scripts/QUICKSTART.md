# StablePay 快速联调指南

## 前置要求

1. **Go 1.21+** 已安装
2. **Docker & Docker Compose** 已安装
3. **curl** 或类似HTTP客户端

## 快速开始（3步启动）

### 第1步：启动基础设施

```bash
cd scripts
docker-compose -f docker-compose.infra.yml up -d
```

等待约10秒让MySQL/Redis/RocketMQ启动完成。

### 第2步：启动业务服务

```bash
./start-services.sh
```

此脚本会：
- 检查所有端口
- 启动基础设施（如果还没启动）
- 按依赖顺序启动6个微服务
- 显示服务状态

### 第3步：验证服务

```bash
# 检查DID服务健康
curl http://localhost:8080/api/v1/did -X POST \
  -H "Content-Type: application/json" \
  -d '{"user_type":1,"metadata":{"env":"test"}}'

# 或运行完整联调测试
./integration-test.sh
```

## 服务端口

| 服务 | 地址 | 说明 |
|------|------|------|
| API Gateway | http://localhost:8080 | 统一入口 |
| DID Service | rpc://localhost:8081 | 身份管理 |
| Payment Service | http://localhost:8082, rpc://localhost:8888 | 支付核心 |
| Blockchain Adapter | rpc://localhost:8083 | 链上操作 |
| Query Service | rpc://localhost:8084 | 数据查询 |
| Verification Service | rpc://localhost:8085 | 购买验证 |

## 基础设施

| 组件 | 地址 | 账号 |
|------|------|------|
| MySQL | localhost:3306 | root/root123 或 stablepay/stablepay123 |
| Redis | localhost:6379 | 无密码 |
| RocketMQ | localhost:9876 | - |
| MQ Console | http://localhost:8088 | - |

## 常用命令

```bash
# 查看所有服务日志
tail -f logs/*.log

# 查看特定服务日志
tail -f logs/did-service.log

# 停止业务服务
./stop-services.sh

# 停止所有服务（包括基础设施）
./stop-services.sh  # 然后选择 y
# 或
docker-compose -f docker-compose.infra.yml down

# 重启单个服务
./stop-services.sh
./start-services.sh
```

## 问题排查

### 端口被占用
```bash
# 查找占用端口的进程
lsof -i :8080

# 终止进程
kill -9 <PID>
```

### 服务启动失败
```bash
# 查看详细日志
cat logs/did-service.log

# 手动启动服务测试
cd ../did-service
go run cmd/server/main.go
```

### 数据库连接失败
```bash
# 检查MySQL容器状态
docker-compose -f docker-compose.infra.yml ps

# 查看MySQL日志
docker-compose -f docker-compose.infra.yml logs mysql

# 手动连接测试
mysql -h 127.0.0.1 -P 3306 -u stablepay -p
```

### 编译错误 (sonic)
如果出现 `invalid reference to runtime.lastmoduledatap` 错误：
```bash
# 降级go.mod中的版本要求
cd did-service
sed -i 's/go 1.24.0/go 1.21/g' go.mod
go mod tidy
```

## 测试调用链

```bash
# 1. 创建DID
curl -X POST http://localhost:8080/api/v1/did \
  -H "Content-Type: application/json" \
  -d '{"user_type":1}'

# 2. 查询DID
curl http://localhost:8080/api/v1/did/did:solana:xxx

# 3. 验证签名
curl -X POST http://localhost:8080/api/v1/did/verify \
  -H "Content-Type: application/json" \
  -d '{"did":"did:solana:xxx","message":"test","signature":"xxx"}'

# 4. 发起支付
curl -X POST http://localhost:8080/api/v1/pay \
  -H "Content-Type: application/json" \
  -H "X-Agent-DID: did:solana:xxx" \
  -d '{"skill_did":"did:solana:skill","amount_minor":1000000,"currency":1}'

# 5. 验证购买
curl "http://localhost:8080/api/v1/verify?agent_did=did:solana:xxx&skill_did=did:solana:skill"

# 6. 查询余额
curl http://localhost:8080/api/v1/balance \
  -H "X-Agent-DID: did:solana:xxx"
```
