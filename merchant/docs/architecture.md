# 架构设计文档

## StablePay Merchant Server 架构

本文档从架构层面解释这个服务的整体设计，帮助你在面试中清晰表达。

---

## 一、整体架构概览

```
┌─────────────────────────────────────────────────────────┐
│                    Agent (OpenClaw Plugin)               │
│   stablepay_merchant_list_products                      │
│   stablepay_pay_via_gateway                             │
└──────────────────────┬──────────────────────────────────┘
                       │ HTTP (REST + x402)
                       ▼
┌─────────────────────────────────────────────────────────┐
│              Merchant Server (CloudWeGo Hertz)           │
│                                                          │
│  ┌──────────────────────────────────────────────────┐   │
│  │  🟢 Adapter 适配层                                │   │
│  │  ┌────────────┐  ┌────────────┐  ┌───────────┐  │   │
│  │  │  Handler   │  │    DTO     │  │  Router   │  │   │
│  │  │ (请求解析) │  │ (数据传输)  │  │ (路由注册) │  │   │
│  │  └─────┬──────┘  └────────────┘  └───────────┘  │   │
│  └────────┼─────────────────────────────────────────┘   │
│           ▼                                              │
│  ┌──────────────────────────────────────────────────┐   │
│  │  🔵 Application 应用层                            │   │
│  │  ┌─────────────────────────────────────────────┐  │   │
│  │  │  ProductAppService                          │  │   │
│  │  │  + ListProducts()     商品列表 Use Case     │  │   │
│  │  │  + ExecutePurchase()  购买流程 Use Case     │  │   │
│  │  └─────────────────────────────────────────────┘  │   │
│  └────────┼─────────────────────────────────────────┘   │
│           ▼                                              │
│  ┌──────────────────────────────────────────────────┐   │
│  │  🟡 Domain 领域层                                 │   │
│  │  ┌────────────┐  ┌────────────┐  ┌───────────┐  │   │
│  │  │  Entity    │  │ Repository│  │  Domain   │  │   │
│  │  │  (Product) │  │ (接口契约)  │  │  Service  │  │   │
│  │  └────────────┘  └────────────┘  └───────────┘  │   │
│  └────────┼─────────────────────────────────────────┘   │
│           ▼                                              │
│  ┌──────────────────────────────────────────────────┐   │
│  │  🟠 Infrastructure 基础设施层                      │   │
│  │  ┌─────────────────┐  ┌──────────────────────┐  │   │
│  │  │  SQLite / 后续   │  │  StablePay Client   │  │   │
│  │  │  ProductRepoImpl │  │  (Gateway HTTP 调用)  │  │   │
│  │  └─────────────────┘  └──────────────────────┘  │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────┐
│          StablePay API Gateway (ai.wenfu.cn)             │
│  /api/v1/verify    验证购买状态                          │
│  /api/v1/pay       提交支付                              │
│  /api/v1/pay/require  查询支付要求                       │
└─────────────────────────────────────────────────────────┘
```

---

## 二、COLA 四层架构详解

COLA 是阿里巴巴开源的整洁架构（Clean Architecture）实现，核心思想是 **"依赖倒置"**：

### 2.1 适配层 (Adapter) — 🟢

**职责**：
- 处理 HTTP 协议细节
- 解析请求参数（Header / Query / Body）
- 校验输入参数
- 将领域对象序列化为 JSON 响应

**关键文件**：
- `internal/adapter/handler/*.go` — 接口处理器
- `internal/adapter/dto/*.go` — 数据传输对象（DTO）
- `internal/adapter/router.go` — 路由注册

**设计原则**："胖 Handler 是坏的" — Handler 里面没有业务逻辑，只有"翻译"。

### 2.2 应用层 (Application) — 🔵

**职责**：
- **Use Case 编排**：一个业务场景对应一个方法
- 事务管理（ACID）
- DTO 与 Entity 的转换
- 不包含业务规则

**关键文件**：
- `internal/application/service/product_app_service.go`

**设计原则**："Application Service 是薄的" — 它只是调用 Domain Service 和 Repository，不包含业务判断。

### 2.3 领域层 (Domain) — 🟡

**职责**：
- **业务实体**：Product 对象及其方法（`IsPurchasable()`）
- **仓储接口**：Repository 的接口定义（契约）
- **领域服务**：不适合放在实体中的业务逻辑

**关键文件**：
- `internal/domain/entity/product.go` — 商品领域实体
- `internal/domain/repository/product_repo.go` — 仓储接口
- `internal/domain/service/product_domain_service.go` — 领域服务

**设计原则**："Domain 层是心脏" — 它不依赖任何框架、数据库、外部服务，是纯 Go 业务代码。

### 2.4 基础设施层 (Infrastructure) — 🟠

**职责**：
- **数据持久化**：Repository 接口的实现（SQLite / MySQL）
- **外部服务**：对 StablePay Gateway 的 HTTP 调用
- 配置加载、日志、监控等

**关键文件**：
- `internal/infrastructure/persistence/sqlite/product_repo_impl.go`
- `internal/infrastructure/client/stablepay_client.go`

**设计原则**："依赖倒置" — Infrastructure 层实现 Domain 层定义的接口，而不是反过来。

---

## 三、为什么要用 COLA 四层架构？

### 优点

| 特性 | 说明 |
|------|------|
| **可测试性** | Domain 层是纯业务逻辑，可以零依赖地进行单元测试 |
| **可维护性** | 每层职责清晰，新人接手可以快速定位代码 |
| **可演进性** | 替换数据库（SQLite→MySQL）只需要改 Infrastructure 一层 |
| **与现有系统兼容** | 原有 Node.js 后端的业务逻辑可以逐段迁移到 Domain 层 |

### 为什么不用简单的 MVC？

传统的 MVC 常见问题是：
- Controller 越来越胖，混入业务逻辑
- Model 变成"贫血模型"（只有 getter/setter，没有业务方法）
- 难以进行单元测试（需要 mock 整个数据库）

COLA 通过四层分离，让代码保持**高内聚、低耦合**。

---

## 四、x402 支付流程（核心亮点）

这是这个项目最具"面试亮点"的技术方案：

### 流程描述

```
Agent (AI)                  Merchant Server            StablePay Gateway
   │                              │                          │
   │ 1. GET /api/v1/products      │                          │
   │◄───────── 商品列表 ──────────│                          │
   │                              │                          │
   │ 2. GET /products/:id/execute?agent_did=X               │
   │◄── 402 Payment Required ────│                          │
   │    (包含 skill_did, price,   │                          │
   │     payTo, asset 等 x402 头) │                          │
   │                              │                          │
   │ 3. 调用 stablepay_pay_via_gateway                      │
   │ ├───────────────────────────│── POST /api/v1/pay ──────►│
   │ │                           │◄──── tx_id, hash ────────│
   │ │                           │                          │
   │ 4. GET /products/:id/execute?agent_did=X               │
   │    (附带 Payment-Signature) │                          │
   │ ├───────────────────────────│── GET /api/v1/verify ───►│
   │ │                           │◄── purchased=true ──────│
   │◄───────── 200 OK + 内容 ────│                          │
```

### 技术要点

1. **HTTP 402**：使用 RFC 标准状态码表示"需要付款"
2. **x402 协议**：在 HTTP Header 中传递支付信息（标准化的 JSON Base64）
3. **无状态设计**：商户后端不保存订单状态，每次请求都去 Gateway 验证
4. **幂等性**：支付是幂等的，Agent 可以安全重试

### 为什么这个设计好？

- **与 AI Agent 原生适配**：Agent 天然会阅读 HTTP 状态码，402 对它来说是明确的"需要付款"信号
- **无需 Session/登录状态**：使用 DID 标识代替用户密码，去中心化
- **安全**：支付在链上完成（Solana USDC），商户后端只做验证

---

## 五、依赖注入与启动流程

```
main.go
  │
  ├── 1. config.Load("")           ← 加载 YAML 配置
  │
  ├── 2. sqlite.NewProductRepo()   ← 创建仓储（Infra）
  │
  ├── 3. client.NewClient(cfg)     ← 创建 Gateway 客户端（Infra）
  │
  ├── 4. domain.NewService(repo)   ← 创建领域服务（Domain）
  │      注: 依赖 Repository 接口，而非具体实现
  │
  ├── 5. application.NewService(   ← 创建应用服务（Application）
  │       repo, domainSvc, cfg)    ← 依赖注入
  │
  ├── 6. adapter.NewRouter(h,      ← 创建路由（Adapter）
  │       handler, healthHandler)  ← AppService 注入 Handler
  │
  └── 7. h.Spin()                  ← 启动 Hertz 服务器
```

依赖方向：main.go → Adapter → Application → Domain ← Infrastructure

---

## 六、现在的状态与后续计划

### 当前状态（第 1 步完成）
- [x] CloudWeGo Hertz 最小框架
- [x] COLA 4 层目录结构
- [x] 健康检查 `/healthz`
- [x] 配置加载
- [x] 种子数据（4 个商品）
- [x] 文档体系

### 后续步骤
- [ ] 第 2 步：SQLite 数据库实现
- [ ] 第 3 步：商品列表 + 购买 API
- [ ] 第 4 步：Skill 指导文档更新
- [ ] 第 5 步：Docker + K8s 部署
