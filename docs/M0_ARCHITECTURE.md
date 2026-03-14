# M0 架构设计文档

## 整体流程

```
┌────────────────────────────────────────────────────────────────┐
│                     x402 Payment Flow                          │
└────────────────────────────────────────────────────────────────┘

Step 1: Initial Request (无支付)
┌─────────────┐                           ┌──────────────┐
│   Client    │──GET /protected ─────────→│    Server    │
│ agent_did   │                           │ (M0)         │
└─────────────┘                           └──────────────┘
                                                 │
                                            [Check DB]
                                            [Not found]
                                                 ↓
                                          ? Return 402
┌─────────────┐                           ┌──────────────┐
│   Client    │←─ 402 + PaymentReq  ──────│    Server    │
│             │                           │              │
└─────────────┘                           └──────────────┘
     │
     │ [Read payment requirement]
     │ [Sign & Create TX]
     │

Step 2: Payment Submission
     │
┌─────────────┐                           ┌──────────────┐
│   Client    │──POST /pay ──────────────→│    Server    │
│ (payment    │  {tx_hash, amount}       │ (M0)         │
│  proof)     │                           │              │
└─────────────┘                           └──────────────┘
                                                 │
                                           [Save to DB]
                                           [Return 200]
                                                 ↓
┌─────────────┐                           ┌──────────────┐
│   Client    │←─ 200 OK ─────────────────│    Server    │
│             │                           │              │
└─────────────┘                           └──────────────┘
     │
     │ [Wait & Retry]
     │

Step 3: Retry Request (已支付)
     │
┌─────────────┐                           ┌──────────────┐
│   Client    │──GET /protected ─────────→│    Server    │
│             │                           │ (M0)         │
└─────────────┘                           └──────────────┘
                                                 │
                                            [Check DB]
                                            [Found!]
                                                 ↓
                                          ? Return 200
┌─────────────┐                           ┌──────────────┐
│   Client    │←─ 200 + Content ──────────│    Server    │
│ (Success!)  │                           │              │
└─────────────┘                           └──────────────┘
```

## 组件设计

### 后端服务 (cmd/server/main.go)

```
Server (Go, http.ListenAndServe)
│
├─ /protected [GET]
│  ├─ 读取请求头 "X-Agent-DID"
│  ├─ 查询 payments 内存表
│  ├─ 已支付? → 200 + content
│  └─ 未支付? → 402 + PaymentRequirement
│
├─ /pay [POST]
│  ├─ 解析 PaymentProof {agent_did, tx_hash, amount}
│  ├─ 保存到 payments 内存表
│  └─ 返回 200 OK
│
├─ /verify [POST]
│  ├─ 查询 payments[agent_did]
│  └─ 返回 {verified: bool}
│
└─ /health [GET]
   └─ 返回 {status: ok}

内存存储 (sync.Map + sync.Mutex)
└─ payments: map[agent_did] → PaymentRecord
   ├─ agent_did (string)
   ├─ skill_did (string)
   ├─ amount (int)
   └─ tx_hash (string)
```

### 客户端逻辑 (cmd/client/main.py)

```
X402Client (Python)
│
├─ access_protected_resource()
│  └─ GET /protected with X-Agent-DID header
│
├─ handle_402_payment(requirement)
│  ├─ 读取支付要求
│  ├─ 生成 mock tx_hash (M0)
│  └─ POST /pay with proof
│
├─ retry_protected_resource()
│  └─ 重新请求 /protected
│
├─ verify_payment()
│  └─ POST /verify 验证状态
│
└─ run()
   └─ 执行完整流程：[1] → [2] → [3] → [4]
```

## 数据模型

### PaymentRequirement (402 返回体)
```json
{
  "recipient": "4zMMUH...",        // 收款钱包 (Solana)
  "amount": 10000,                 // USDC 最小单位
  "currency": "USDC",
  "description": "...",
  "timeout": 300                   // 秒
}
```

### PaymentProof (客户端支付)
```json
{
  "agent_did": "did:agent:test:m0",
  "tx_hash": "5mVz4n...",          // Solana TX hash
  "amount": 10000
}
```

### PaymentRecord (内存存储)
```json
{
  "agent_did": "did:agent:test:m0",
  "skill_did": "did:skill:stablepay:v1",
  "amount": 10000,
  "tx_hash": "5mVz4n..."
}
```

## 配置参数

| 参数 | 当前值 | 说明 | M1 改动 |
|-----|-------|------|--------|
| `SKILL_DID` | `did:skill:stablepay:v1` | 技能唯一ID | 支持多个 |
| `PAYMENT_WALLET` | `4zMMUH...` | 收款钱包 | 从环境变量读取 |
| `PAYMENT_AMOUNT` | `10000` | 支付金额 (USDC 最小单位) | 动态定价 |
| `PAYMENT_TIMEOUT` | `300` | 超时时间 (秒) | 可配 |

## 错误处理

### 服务端错误

| 状态码 | 场景 | 处理 |
|-------|------|------|
| 200 | 支付验证通过 | 返回内容 |
| 402 | 未支付 | 返回支付要求 |
| 400 | 请求格式错误 | 返回错误信息 |
| 500 | 服务异常 | 返回异常 |

### 客户端错误

| 错误 | 原因 | 重试策略 |
|-----|------|---------|
| 连接失败 | 服务未启动 | 等待后重试 |
| 402 持续返回 | 支付未生效 | 检查 /pay 响应 |
| 支付超时 | 链上交易慢 | 客户端需等待 |

## 性能指标 (M0)

```
吞吐量:       ~1000 req/s (health check)
延迟:         < 10ms (内存操作)
并发:         有限 (单线程 HTTP)
存储:         O(n) - 支付数量
```

## 安全性考评

### M0 安全性问题

| 问题 | 严重性 | M1 解决方案 |
|-----|-------|-----------|
| 支付未验证 | ? 高 | 调用 Solana RPC |
| 内存存储 | ? 高 | PostgreSQL 落库 |
| 无私钥存储 | ? 高 | 本地加密存储 |
| 无签名验证 | ? 高 | Ed25519 验证 |
| 明文传输 | ? 中 | HTTPS + TLS |
| 无速率限制 | ? 中 | Rate limiter 中间件 |

## 扩展性规划

### M0 → M1
- [ ] 真实 Solana 验证
- [ ] 数据库持久化
- [ ] 本地密钥管理
- [ ] 事务日志

### M1 → M2
- [ ] CloudWeGo 框架迁移
- [ ] OpenClaw Skill 包装
- [ ] X 账号绑定验证
- [ ] 多租户支持

### M2 → M3
- [ ] 链上治理 DAO
- [ ] 动态定价
- [ ] 推荐系统
- [ ] 第三方集成

## 部署架构

### M0 单体部署
```
        localhost:8080
                │
        ┌───────┴────────┐
        │                │
    cmd/server        cmd/client
    (Go HTTP)         (Python)
        │
    内存存储 (payments map)
```

### M1 建议架构
```
    Load Balancer (nginx)
            │
        ┌───┴───┐
        │       │
    Server1  Server2  (Stateless)
        │       │
        └───┬───┘
            │
        PostgreSQL
        (持久化)
            │
        Solana RPC
        (链上验证)
```

## 部署清单

### 本地开发
- [x] Go 1.21+
- [x] Python 3.8+
- [x] `make` 工具
- [x] 依赖：`requests` (Python)

### 生产环境 (M1+)
- [ ] Docker 镜像
- [ ] Kubernetes 部署文件
- [ ] 环境变量配置
- [ ] TLS 证书
- [ ] 数据库迁移脚本
- [ ] 监控 (Prometheus/Grafana)
- [ ] 日志 (ELK stack)

## 参考文档

- RFC 7231: [HTTP 402 Payment Required](https://tools.ietf.org/html/rfc7231#section-6.5.2)
- Solana RPC: [getTransaction](https://docs.solana.com/api/http#gettransaction)
- x402 标准: [Solana 官方指南](https://docs.solana.com/)
