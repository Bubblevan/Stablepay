# 面试指南

## 如何向面试官介绍这个项目

本文档帮助你用"面试官能听懂"的方式讲解整个项目。

---

## 一、电梯演讲（30 秒）

> "这是一个 AI Agent 的电商平台——不是给人用的，是给 AI 用的。
>
> 我们搭建了一个商户后端（Go + CloudWeGo），让 AI Agent 可以：
> 1. 浏览商品列表
> 2. 看到喜欢的商品后，用自己的 Solana 钱包支付 USDC
> 3. 购买成功后，获得商品内容
>
> 整个流程基于 x402 标准，用 HTTP 402 状态码来自动触发支付。"

---

## 二、项目背景（2 分钟）

### 为什么会有这个项目？

StablePay 是一个让 AI Agent 拥有"数字钱包"的平台。

之前实现了一个 demo 级的后端（showmethemoney-skill，Node.js），只支持单个付费商品。

现在需要把它升级为**生产级的商户平台**，支持：
- 多个商品管理
- 标准化的 x402 支付协议
- 可扩展的架构
- 能与现有 K8s 基础设施集成

### 为什么选 Go + CloudWeGo？

- **性能**：Go 的并发模型和 CloudWeGo 的高性能是生产级保证
- **与现有基础设施一致**：现有微服务（query-service、payment-service 等）都是 CloudWeGo
- **长期维护**：Go 比 Node.js 更适合长时间运行的后端服务

### 为什么选 COLA 架构？

- **可测试性**：Domain 层是纯 Go 结构体，可以零依赖测试
- **可替换性**：数据库从 SQLite 换到 MySQL 只需改 Infrastructure 层
- **团队协作**：四层职责清晰，多人协作互不干扰

---

## 三、技术亮点（3-5 分钟）

### 亮点 1：x402 标准 + HTTP 402

这是整个项目最大的技术亮点。

**传统支付流程**（给人用）：
```
用户点击"购买" → 跳转到支付页面 → 输入密码 → 重定向回来
```
需要 Session、Cookie、重定向、前端交互。

**x402 支付流程**（给 AI 用）：
```
Agent 请求商品 → 收到 402 → 解析 Payment-Required header → 调用支付 API → 重试请求
```
纯 HTTP 协议，无需前端交互，AI Agent 天然适配。

**为什么好？**
```go
// x402 402 响应示例
HTTP/1.1 402 Payment Required
Payment-Required: <base64 JSON>

{
  "accepts": [{
    "scheme": "exact",
    "network": "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp",
    "maxAmountRequired": "2000000",  // 2.00 USDC = 2000000 minor units
    "payTo": "2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR",
    "asset": "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"  // USDC mint
  }]
}
```

**对于面试官的提问，可以这样回答**：

Q: "为什么不用传统的 OAuth2 + Payment Gateway？"
A: "因为用户是 AI Agent。Agent 没有浏览器，不能重定向到支付页面。
我们用了 HTTP 402 + x402 标准，Agent 看到 402 就知道要付款，
然后通过 StablePay Gateway 直接上链支付，不需要任何人类交互。"

### 亮点 2：COLA 四层架构

**面试官可能会问**："为什么用 COLA？你们的项目规模用 MVC 不够吗？"

**回答思路**：
1. "我们的业务虽然现在简单，但架构是为未来设计的"
2. "举例：如果要从 SQLite 迁移到 MySQL，只需要改 Infrastructure 层"
3. "Domain 层可以独立测试，不依赖数据库、不依赖 HTTP 框架"
4. "在面试中可以展示对 Clean Architecture 的理解"

```go
// Domain 层的 Product 实体是纯业务代码
func (p *Product) IsPurchasable() bool {
    return p.Status == ProductStatusActive && p.Price != "" && p.SkillDid != ""
}
// 这行代码可以 100% 覆盖测试，不需要 mock 任何东西
```

### 亮点 3：DID 去中心化身份

**面试官问**："你们的用户登录怎么做的？"

**回答**：
"我们没有传统的用户名密码登录。每个 Agent 有自己的 Solana 钱包，
通过 OWS (Open Wallet Standard) 管理私钥。

用户标识是 DID (Decentralized Identifier)，格式是 `did:solana:<pubkey>`。
这个 DID 就是用户的身份，不需要注册、不需要密码。

商户后端收到请求时，根据 DID 去 StablePay Gateway 验证支付状态，
整个过程是去中心化的、无状态的。"

---

## 四、可能的面试问题

### 架构相关

**Q: 四层架构和三层架构的区别？**
A: 三层架构通常指 Controller/Service/DAO，COLA 多了 Domain 层。
Domain 层把"业务逻辑"和"应用编排"分开了，让核心业务更清晰。

**Q: 怎么保证层与层之间的依赖方向正确？**
A: 通过 Go 的 package 导入规则。Domain 层不导入 Adapter 和 Infrastructure 包。
Infrastructure 层实现了 Domain 层定义的接口。这在编译期就保证了依赖方向。

**Q: 为什么不用微服务拆分？**
A: 当前业务规模（商户 + 商品管理）单个服务就够。
如果后续需要，COLA 架构的优势就在这里——Domain 层可以直接抽取为独立的微服务，
Adapter 层变成 gRPC 或消息队列的适配器。

### 技术细节

**Q: 为什么用 SQLite 不用 MySQL？**
A: 第一阶段为了开发效率。SQLite 是文件型数据库，无需单独部署。
目前是用内存模拟，后续会切换到 SQLite，再后续可以迁移到 MySQL（现有基础设施中有 MySQL）。

**Q: x402 和直接 POST /api/v1/pay 有什么区别？**
A: x402 是标准协议，可以在不预先知道价格的情况下完成支付。
402 响应中包含了所有必要信息：收款地址、金额、资产类型等。
客户端（AI Agent）读取这些信息后调用支付网关，无需硬编码支付参数。

### 项目经历

**Q: 你在项目中负责什么？**
A: 我是整个商户后端的技术负责人，从零搭建了这个项目。
包括：技术选型（Go + CloudWeGo）、架构设计（COLA 四层）、
x402 支付协议实现、与 OpenClaw 插件的数据流设计。

---

## 五、项目亮点总结（一句话一个）

1. **AI-to-AI 电商** — 不是给人用的电商，是给 AI Agent 用的
2. **x402 标准** — 用 HTTP 402 让 AI 自动完成支付
3. **COLA 架构** — 企业级 Go 后端的最佳实践
4. **Go + CloudWeGo** — 字节跳动级的高性能框架
5. **Solana USDC** — 链上结算，去中心化支付
6. **SQLite → MySQL 演进** — 架构设计考虑到了未来扩容
7. **与 K8s 原生集成** — ACR + ACK 部署链路
