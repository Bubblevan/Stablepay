# StablePay 微服务联调指南

## 1. 服务架构概览

```
┌─────────────────────────────────────────────────────────────────┐
│                        API Gateway                              │
│                      (Hertz :8080)                              │
└────────────────────────────┬────────────────────────────────────┘
                             │ HTTP
        ┌────────────────────┼────────────────────┐
        │                    │                    │
        ▼                    ▼                    ▼
┌───────────────┐   ┌───────────────┐   ┌───────────────┐
│  DID Service  │   │Payment Service│   │Verification   │
│  (:8081 RPC)  │   │(:8888 RPC)    │   │Service        │
└───────┬───────┘   └───────┬───────┘   │(:8085 RPC)    │
        │                   │           └───────────────┘
        │                   │
        │           ┌───────┴───────┐
        │           │Blockchain     │
        │           │Adapter        │
        │           │(:8083 RPC)    │
        │           └───────────────┘
        │                   │
        └───────────┐       │
                    ▼       ▼
            ┌───────────────┐
            │ Query Service │
            │ (:8084 RPC)   │
            └───────────────┘
```

## 2. 端口分配

| 服务 | HTTP端口 | RPC端口 | 说明 |
|------|---------|--------|------|
| API Gateway | 8080 | - | 统一入口 |
| DID Service | - | 8081 | 身份管理 |
| Payment Service | 8082 | 8888 | 支付核心 |
| Blockchain Adapter | - | 8083 | 链上操作 |
| Query Service | - | 8084 | 数据查询 |
| Verification Service | - | 8085 | 购买验证 |

## 3. 服务依赖关系

```
DID Service: 无依赖 (基础服务)
    ↓
Blockchain Adapter: 依赖 Solana RPC
    ↓
Payment Service: 依赖 DID Service + Blockchain Adapter
    ↓
Verification Service: 依赖 MQ + 数据库
    ↓
Query Service: 依赖 数据库
    ↓
API Gateway: 依赖所有下游服务
```

## 4. 联调步骤

### 4.1 启动基础设施

确保以下服务已运行：
- MySQL (localhost:3306)
- Redis (localhost:6379)
- RocketMQ (localhost:9876)

### 4.2 一键启动所有服务

```bash
cd scripts
./start-services.sh
```

### 4.3 验证服务启动

```bash
# 查看进程
ps aux | grep -E "(did-service|payment|blockchain|gateway)"

# 查看日志
tail -f logs/*.log
```

### 4.4 运行联调测试

```bash
./integration-test.sh
```

## 5. 手动联调测试

### 5.1 测试 DID 创建

```bash
curl -X POST http://localhost:8080/api/v1/did \
  -H "Content-Type: application/json" \
  -d '{
    "user_type": 1,
    "metadata": {"env": "integration"}
  }'
```

### 5.2 测试 DID 查询

```bash
curl http://localhost:8080/api/v1/did/did:solana:xxx \
  -H "X-Agent-DID: did:solana:xxx"
```

### 5.3 测试支付

```bash
curl -X POST http://localhost:8080/api/v1/pay \
  -H "Content-Type: application/json" \
  -H "X-Agent-DID: did:solana:xxx" \
  -d '{
    "skill_did": "did:solana:skill123",
    "amount_minor": 1000000,
    "currency": 1
  }'
```

### 5.4 测试购买验证

```bash
curl "http://localhost:8080/api/v1/verify?agent_did=did:solana:xxx&skill_did=did:solana:skill123" \
  -H "X-API-Key: stablepay-dev-key"
```

## 6. 配置检查清单

### 6.1 DID Service (`did-service/conf/dev.yaml`)
- [ ] 端口设置为 8081
- [ ] 数据库配置正确（或内存模式）

### 6.2 Payment Service (`payment-service/config/config.yaml`)
- [ ] HTTP端口改为 8082（避免与Gateway冲突）
- [ ] RPC端口保持 8888
- [ ] did_service地址指向 localhost:8081
- [ ] blockchain_adapter地址指向 localhost:8083

### 6.3 Blockchain Adapter (`blockchain-adapter/conf/dev.yaml`)
- [ ] 端口设置为 8083（避免与Payment冲突）
- [ ] Solana RPC配置正确

### 6.4 API Gateway (`api-gateway/configs/config.yaml`)
- [ ] downstream地址配置正确
- [ ] 路由配置正确

## 7. 常见问题

### 7.1 端口冲突
```bash
# 查找占用端口的进程
lsof -i :8080

# 终止进程
kill -9 <PID>
```

### 7.2 服务间连接失败
- 检查下游服务是否已启动
- 检查配置文件中的地址是否正确
- 检查防火墙设置

### 7.3 数据库连接失败
- 确认MySQL已启动
- 检查DSN配置
- 确认数据库已创建

## 8. 停止服务

```bash
./stop-services.sh
```

## 9. 联调检查点

- [ ] 6个服务全部启动成功
- [ ] API Gateway 健康检查通过
- [ ] DID 创建流程正常
- [ ] DID 查询流程正常
- [ ] 支付流程正常
- [ ] 购买验证流程正常
- [ ] 余额查询流程正常
