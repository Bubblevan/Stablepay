# Legacy 代码说明

此目录包含 **Phase1-6** 版本的传统分层架构代码，已归档保存供参考。

---

## 目录结构

```
legacy/
├── cmd/server/main.go              # 旧启动入口
├── main.go                         # 根目录旧入口
├── handler.go                      # 根目录 handler
├── internal/
│   ├── solana/                     # 区块链客户端 (Phase3)
│   │   ├── client.go
│   │   ├── transaction.go
│   │   ├── hotwallet.go
│   │   └── spl_token.go
│   ├── service/                    # 业务服务 (Phase5)
│   │   ├── transfer_service.go
│   │   ├── balance_service.go
│   │   └── tx_status_service.go
│   └── handler/                    # RPC Handler (Phase6)
│       ├── handler.go
│       ├── transfer.go
│       ├── balance.go
│       └── tx_status.go
└── data-access-layer/              # 数据层 (Phase4)
    └── db/
        ├── init.go
        └── gas_subsidy.go
```

---

## 说明

### 这些代码为什么被归档？

1. **架构演进**: 项目已从传统分层架构迁移到 COLA v5 架构
2. **新架构位置**: 新代码位于 `blockchain-adapter-*/` 目录
3. **参考用途**: 保留原有实现供对比和学习

### 新旧架构对比

| 维度 | Legacy (Phase1-6) | 新架构 (COLA v5) |
|------|-------------------|------------------|
| 架构 | 传统三层 | COLA 五层 |
| 目录 | `internal/` | `blockchain-adapter-*/` |
| 领域模型 | 贫血模型 | 充血模型 |
| 依赖关系 | 上层依赖下层 | Domain 核心，依赖倒置 |
| 可测试性 | 难 mock | 易 mock Gateway |

### 如需恢复使用

```bash
# 1. 从 legacy 恢复
cd blockchain-adapter
cp -r legacy/internal .
cp -r legacy/data-access-layer .
cp -r legacy/cmd .
cp legacy/handler.go .
cp legacy/main.go .

# 2. 修改 go.mod 模块路径
# 3. 更新 import 路径
```

---

## 相关文档

- [COLA_ARCHITECTURE.md](../doc/COLA_ARCHITECTURE.md) - 新架构设计
- [ARCHITECTURE_EVOLUTION.md](../doc/ARCHITECTURE_EVOLUTION.md) - 演进历程

---

*归档时间: 2026-03-15*  
*对应版本: Phase1-6*
