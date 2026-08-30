# StablePay 手动联调指南

按依赖顺序逐步启动服务，逐步验证。

---

## 第一步：基础设施 (MySQL/Redis/RocketMQ)

先打开你的 Docker Desktop：

```bash
cd scripts
docker-compose -f docker-compose.infra.yml up -d
```

### 验证基础设施

```bash
# MySQL (自动创建数据库)
docker-compose -f docker-compose.infra.yml ps mysql
# 手动创建数据库（如果自动初始化失败）:
docker exec -i stablepay-mysql mysql -uroot -proot123 < init-db/01-init.sql
docker exec -i stablepay-mysql mysql -uroot -proot123 < init-db/02-blockchain.sql

# Redis
docker-compose -f docker-compose.infra.yml ps redis
redis-cli -h 127.0.0.1 -p 6379 ping

# RocketMQ
docker-compose -f docker-compose.infra.yml ps rocketmq-nameserver
# 查看控制台: http://localhost:8088
```

---

## 第二步：DID Service (基础服务)

**端口**: 8081
**依赖**: 无 (使用内存存储)

```bash
cd did-service
go run cmd/server/main.go
```

### 验证 DID Service

**注意**: DID Service 是 RPC 服务（Thrift 协议），**不能直接用 curl 访问**。

验证方法：
```bash
# 方式1: 查看进程是否在监听
netstat -an | findstr 8081  # Windows
lsof -i :8081               # Linux/Mac

# 方式2: 查看日志确认启动成功
# 日志应显示: "DID Service starting on 0.0.0.0:8081"

# 方式3: 等 API Gateway 启动后，通过 Gateway 的 HTTP 接口测试
# curl -X POST http://localhost:8080/api/v1/did -d '{"user_type":1}'
```

---

## 第三步：Blockchain Adapter

**端口**: 8083
**依赖**: Solana DevNet + MySQL

### 前置准备：生成热钱包

Blockchain Adapter 需要一个 Solana 热钱包文件：

**注意**: 生成的热钱包仅用于开发联调，不要在生产环境使用！

### 启动

```bash
cd blockchain-adapter
# 使用本地联调配置
go run cmd/server/main.go -config conf/dev.local.yaml
# 或原始配置
go run cmd/server/main.go -config conf/dev.yaml
```

### 验证 Blockchain Adapter

查看日志确认：
- ✅ 数据库初始化完成
- ✅ 热钱包加载成功: `xxx...`
- ✅ Solana RPC 连接成功

---

## 第四步：Query Service

**端口**: 8084
**依赖**: SQLite (本地文件)

```bash
cd Query-service

# 注意：使用 . 编译整个包，不要只写 main.go
go run .

# 或明确指定所有 go 文件
go run *.go
```

### 可能遇到的问题

**问题**: `undefined: InitDB` 或 `undefined: QueryServiceImpl`
**解决**: 必须使用 `go run .` 而不是 `go run main.go`，因为代码分散在多个文件中。

### 验证 Query Service

服务启动后会自动创建 `query.db` 文件。

---

## 第五步：Verification Service

**端口**: 8085
**依赖**: SQLite + RocketMQ

```bash
cd verification
go run main.go
```

### 验证 Verification Service

- 数据库: `test.db`
- MQ消费者: 查看日志确认消费端启动

---

## 第六步：Payment Service

**端口**: HTTP 8082 / RPC 8888
**依赖**: DID Service(8081) + Blockchain Adapter(8083) + MySQL + Redis + RocketMQ

### 前置检查

确保以下服务已启动：
- DID Service: localhost:8081
- Blockchain Adapter: localhost:8083
- MySQL: localhost:3306
- Redis: localhost:6379
- RocketMQ: localhost:9876

### 启动

```bash
cd payment-service

# 使用本地联调配置（已修正端口）
cp config/config.local.yaml config/config.yaml
go run cmd/payment-service/main.go
```

### 验证 Payment Service

```bash
# 检查健康端点
curl http://localhost:8082/health
```

---

## 第七步：API Gateway (最后启动)

**端口**: 8080
**依赖**: 所有下游服务

### 前置检查

确认下游服务地址配置正确 (`configs/config.yaml`):

```yaml
downstream:
  did_service: "localhost:8081"
  payment_service: "localhost:8888"
  verification_service: "localhost:8085"
  query_service: "localhost:8084"
```

### 启动

```bash
cd api-gateway
go run cmd/api-gateway/main.go -config configs/config.yaml
```

### 验证 API Gateway

```bash
# 健康检查
curl http://localhost:8080/health

# 测试 DID 创建
curl -X POST http://localhost:8080/api/v1/did \
  -H "Content-Type: application/json" \
  -d '{"user_type":1,"metadata":{}}'

# 测试 DID 查询
curl http://localhost:8080/api/v1/did/did:solana:xxx

# 测试支付
curl -X POST http://localhost:8080/api/v1/pay \
  -H "Content-Type: application/json" \
  -H "X-Agent-DID: did:solana:test" \
  -d '{
    "skill_did": "did:solana:skill123",
    "amount_minor": 1000000,
    "currency": 1
  }'
```

---

## 分步联调测试

### 阶段1: 测试 DID 链路

```bash
# 1. 创建 DID
curl -X POST http://localhost:8080/api/v1/did \
  -d '{"user_type":1}'

# 2. 记录返回的 did

# 3. 查询 DID
curl http://localhost:8080/api/v1/did/<did>

# 4. 验证签名
curl -X POST http://localhost:8080/api/v1/did/verify \
  -d '{"did":"<did>","message":"test","signature":"xxx","timestamp":"2026-03-20T10:00:00Z"}'
```

### 阶段2: 测试支付链路

```bash
# 需要先用 DID Service 创建两个 DID (付款方和收款方)

# 发起支付
curl -X POST http://localhost:8080/api/v1/pay \
  -H "X-Agent-DID: <agent_did>" \
  -d '{
    "skill_did": "<skill_did>",
    "amount_minor": 1000000,
    "currency": 1
  }'

# 查询支付状态
curl http://localhost:8080/api/v1/pay/<tx_id>

# 查询支付历史
curl "http://localhost:8080/api/v1/pay/history?agent_did=<agent_did>"
```

### 阶段3: 测试验证链路

```bash
# 购买验证
curl "http://localhost:8080/api/v1/verify?agent_did=<agent_did>&skill_did=<skill_did>" \
  -H "X-API-Key: stablepay-dev-key"

# 批量验证
curl -X POST http://localhost:8080/api/v1/verify/batch \
  -H "X-API-Key: stablepay-dev-key" \
  -d '{"agent_did":"<agent_did>","skill_dids":["<skill1>","<skill2>"]}'
```

### 阶段4: 测试查询链路

```bash
# 余额汇总
curl http://localhost:8080/api/v1/balance \
  -H "X-Agent-DID: <agent_did>"

# 交易列表
curl "http://localhost:8080/api/v1/transactions?did=<did>&type=1&limit=10&offset=0" \
  -H "X-Agent-DID: <agent_did>"

# 收入汇总 (Skill DID)
curl "http://localhost:8080/api/v1/revenue?skill_did=<skill_did>" \
  -H "X-Agent-DID: <skill_did>"
```

---

## 常见问题排查

### 端口冲突

```bash
# 查看占用端口的进程
lsof -i :8080
lsof -i :8081
lsof -i :8888

# 终止进程
kill -9 <PID>
```

### 服务连接失败

```bash
# 检查服务是否监听正确地址
netstat -tlnp | grep 8081

# 测试 RPC 连通性
telnet localhost 8081
```

### 数据库连接失败

**问题**: `Unknown database 'xxx'`
**解决**: 手动创建数据库
```bash
docker exec -i stablepay-mysql mysql -uroot -proot123 < scripts/init-db/01-init.sql
docker exec -i stablepay-mysql mysql -uroot -proot123 < scripts/init-db/02-blockchain.sql
```

**问题**: `Access denied for user`
**解决**: 检查配置文件中的数据库密码是否正确
- 正确用户/密码: `root`/`root123` 或 `stablepay`/`stablepay123`

```bash
# 检查 MySQL 容器
docker-compose -f scripts/docker-compose.infra.yml logs mysql

# 手动连接测试
mysql -h 127.0.0.1 -P 3306 -u stablepay -pstablepay123
```

### 热钱包加载失败

**问题**: `failed to read hot wallet file: open conf/hotwallet.json: The system cannot find the file specified`
**解决**:
```bash
cd scripts
go run generate-hotwallet.go > ../blockchain-adapter/conf/hotwallet.json
```

### 查看服务日志

```bash
# DID Service
tail -f did-service/nohup.out

# Payment Service
tail -f payment-service/nohup.out

# API Gateway
tail -f api-gateway/nohup.out
```

---

## 停止服务

按相反顺序停止：

```bash
# 1. 停止 API Gateway (Ctrl+C)

# 2. 停止 Payment Service (Ctrl+C)

# 3. 停止 Verification Service (Ctrl+C)

# 4. 停止 Query Service (Ctrl+C)

# 5. 停止 Blockchain Adapter (Ctrl+C)

# 6. 停止 DID Service (Ctrl+C)

# 7. 停止基础设施
cd scripts
docker-compose -f docker-compose.infra.yml down
```
