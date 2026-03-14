# StablePay M0 - x402 支付流程最小化实现

## 概述

M0 版本实现了 **x402 HTTP 402 Payment Required** 的完整闭环：

1. **Client** → 请求受保护资源
2. **Server** → 返回 402 + 支付要求
3. **Client** → 支付并重试（带支付证明）
4. **Server** → 验证 + 返回 200

## 项目结构

```
demo1/
├── cmd/
│   ├── server/
│   │   └── main.go          # 后端服务
│   └── client/
│       └── main.py          # Python CLI 客户端
├── go.mod
├── Makefile                 # 快速命令
└── README_M0.md
```

## 快速开始

### 1. 启动后端服务

**Windows (推荐)：**
```powershell
cd cmd/server
go run main.go
```

**Linux/Mac (如果装了 make)：**
```bash
make server
```

输出：
```
StablePay M0 Server listening on :8080
Skill DID: did:skill:stablepay:v1
```

### 2. 运行客户端（新终端）

```bash
python3 cmd/client/main.py
```

### 完整流程示例

```
[1] 请求受保护资源: http://localhost:8080/protected
    Agent DID: did:agent:test:m0
    ? 收到 402，需要支付

[2] 收到 402 Payment Required
    支付要求: {
      "recipient": "4zMMUHCXxY...",
      "amount": 10000,
      "currency": "USDC",
      "description": "Payment required for StablePay Skill access",
      "timeout": 300
    }

[3] 执行支付 (M0 模拟)
    Mock TX Hash: 5mVz4n7kL2pQwR9xJ8tY3uA6bC1dE5fG7hI9jK0lM2nO3pQ4rStU5vW
    ? 支付已记录
    响应: {"status": "payment_recorded", "message": "Payment recorded successfully..."}

[4] 重试请求受保护资源...

[结果] 最终状态码: 200
    ? 成功获取受保护资源!
    响应: {
      "status": "ok",
      "data": "This is paid premium skill content. Agent: did:agent:test:m0",
      "paid_at": {...}
    }

? x402 流程完成
```

## API 文档

### 1. GET /protected
请求受保护的资源

**请求头：**
```
X-Agent-DID: did:agent:test:m0
```

**响应 402（未支付）：**
```json
{
  "recipient": "4zMMUHCXxYNbtjS7MBvVh8G7zVqKjn6fKgcR88VVFBq",
  "amount": 10000,
  "currency": "USDC",
  "description": "Payment required for StablePay Skill access",
  "timeout": 300
}
```

**响应 200（已支付）：**
```json
{
  "status": "ok",
  "data": "This is paid premium skill content...",
  "paid_at": {
    "agent_did": "did:agent:test:m0",
    "skill_did": "did:skill:stablepay:v1",
    "amount": 10000,
    "tx_hash": "5mVz4n..."
  }
}
```

### 2. POST /pay
提交支付证明

**请求体：**
```json
{
  "agent_did": "did:agent:test:m0",
  "tx_hash": "5mVz4n7kL2pQwR9xJ8tY3uA6bC1dE5fG7hI9jK0lM2nO3pQ4rStU5vW",
  "amount": 10000
}
```

**响应 200：**
```json
{
  "status": "payment_recorded",
  "message": "Payment recorded successfully. Please retry the request."
}
```

### 3. POST /verify
验证支付状态

**请求体：**
```json
{
  "agent_did": "did:agent:test:m0",
  "skill_did": "did:skill:stablepay:v1"
}
```

**响应：**
```json
{
  "verified": true,
  "message": "Agent has valid payment"
}
```

## 核心特点

### ? M0 已实现
- [x] HTTP 402 标准流程
- [x] 简单的内存存储
- [x] 支付要求生成
- [x] 支付验证逻辑
- [x] CLI 客户端完整演示

### ? 后续 M1+ 的建议方向

| 阶段 | 任务 | 描述 |
|-----|------|------|
| M1 | 真实 Solana 集成 | 调用 Solana RPC 验证交易 |
| M1 | 数据库落库 | 持久化支付记录 |
| M1+ | OpenClaw Skill 包装 | 转换成 SKILL.md + 本地配置 |
| M1+ | 本地密钥管理 | 支持本地加密存储私钥 |
| M2 | CloudWeGo 微服务化 | 引入 Kitex/Hertz 高性能框架 |
| M2 | X 账号绑定验证 | 前端验证 + 后端接口 |
| M2 | 多租户支持 | 支持不同的 Skill 定价 |

## 配置说明

编辑 `cmd/server/main.go` 中的常量：

```go
SKILL_DID       = "did:skill:stablepay:v1"      // Skill 唯一标识
PAYMENT_WALLET  = "4zMMUHCXxY..."               // 收款钱包地址
PAYMENT_AMOUNT  = 10000                         // USDC 最小单位 (0.01 USDC)
PAYMENT_TIMEOUT = 300                           // 支付超时 (秒)
```

## 测试场景

### 场景 1：首次访问（无支付）
```bash
curl -H "X-Agent-DID: did:agent:test:m0" http://localhost:8080/protected
# 预期: 402 + 支付要求
```

### 场景 2：支付后访问
```bash
# 1. 支付
curl -X POST http://localhost:8080/pay \
  -H "Content-Type: application/json" \
  -d '{
    "agent_did": "did:agent:test:m0",
    "tx_hash": "mock_tx_123",
    "amount": 10000
  }'

# 2. 重新访问
curl -H "X-Agent-DID: did:agent:test:m0" http://localhost:8080/protected
# 预期: 200 + 内容
```

### 场景 3：验证支付
```bash
curl -X POST http://localhost:8080/verify \
  -H "Content-Type: application/json" \
  -d '{
    "agent_did": "did:agent:test:m0",
    "skill_did": "did:skill:stablepay:v1"
  }'
# 预期: {"verified": true, ...}
```

## 性能测试

```bash
# 健康检查
make build
./bin/server &
ab -n 100 -c 10 http://localhost:8080/health

# 结果: ~1000+ req/s (M0 简单实现)
```

## 安全性说明（M0 → M1）

### M0 当前限制
- ? 支付未真实验证（mock tx_hash）
- ? 内存存储（进程重启丢失）
- ? 无私钥本地存储
- ? 无交易签名验证

### M1 建议改进
- 调用 Solana RPC `getTransaction()` 验证交易
- PostgreSQL 存储支付记录
- 使用本地加密库存储私钥
- 实现 Ed25519 签名验证

## 开发指南

### 修改支付金额
编辑 `cmd/server/main.go` → `PAYMENT_AMOUNT`

### 修改后端端口
编辑 `cmd/server/main.go` → main() 中的 `port := "8080"`

### 修改客户端 Agent DID
```bash
python3 cmd/client/main.py --agent-did "did:agent:custom:123"
```

### 指向自定义服务器
```bash
python3 cmd/client/main.py --server "http://api.example.com"
```

## 故障排除

| 问题 | 原因 | 解决方案 |
|-----|------|--------|
| `connection refused` | 后端未启动 | `make server` |
| `402 Never resolves` | 支付未写入 | 检查 `/pay` 响应 |
| `Import not found` | 依赖缺失 | `go mod download` |
| `Python 3 not found` | 环境问题 | `python3 --version` |

## 参考资源

- [x402 HTTP 402 Payment Required RFC](https://tools.ietf.org/html/rfc7231#section-6.5.2)
- [Solana x402 实现参考](https://docs.solana.com/)
- [OpenClaw Skills 文档](https://docs.openclaw.ai/)

## 许可证

MIT
