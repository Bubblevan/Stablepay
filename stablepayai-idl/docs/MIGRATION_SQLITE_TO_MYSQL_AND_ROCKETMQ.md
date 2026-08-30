# StablePay 迁移与联调现状说明：SQLite -> MySQL & RocketMQ

更新时间：2026-03-30

本文档不再只是记录“SQLite 改 MySQL”和“RocketMQ 报错怎么修”，而是结合当前微服务仓库代码，说明这次迁移到底带来了什么、哪些链路已经具备联调基础、哪些环节仍然是占位、mock 或需要继续补齐。

---

## 1. 目标与范围

这轮改造的核心目标有两个：

1. 把 `verification-service` 和 `query-service` 从 SQLite 切到 MySQL，避免本地文件数据库在容器化、多服务部署下带来的不一致问题。
2. 把支付后的异步链路稳定下来，即：

`Payment Service -> RocketMQ -> Verification Service -> Query Service`

同时，需要结合 `stablepayai-idl/prd.md` 与 `stablepayai-idl/tech.md`，判断用户侧主链是否真的已经“拉通”。

本次判断基于以下仓库代码：

- `api-gateway`
- `did-service`
- `payment-service`
- `blockchain-adapter`
- `verification-service`
- `query-service`

---

## 2. 一句话结论

截至 2026-03-30，当前仓库状态可以概括为：

- `SQLite -> MySQL` 迁移在 `verification-service` 和 `query-service` 方向上已经落地。
- `Payment -> RocketMQ -> Verification` 这条异步链已经具备真实代码实现，RocketMQ 的 `IP addr error` 也已经有代码级规避和运行态修复。
- 用户侧“首次购买链”已经接近打通到 `Verification`，但还不能说完整按 PRD 拉通，因为 `Query` 仍有演示数据和未补齐的返回问题。
- 用户侧“首次注册链”还不能算打通，因为当前微服务代码里没有真正落地 `X 验证页 / 推文校验 / DID-X 绑定 / 1 USDC 奖励发放 / 奖励入账查询` 这一整段后端闭环。

如果当前阶段明确接受“两处先跳过”：

- 跳过真实 HTTPS 下的 X 验证
- 跳过真实 1 USDC 奖励发放

那么可以做一个“降级版联调演示”，但它不等于 PRD 定义的正式链路已经闭环。

---

## 3. 这次迁移已经完成了什么

### 3.1 Verification / Query：SQLite -> MySQL

从仓库结构和服务代码看，这部分迁移已经完成，方向是正确的：

- `verification-service` 现在以 MySQL 为存储，核心表对齐到 `purchase_records`
- `query-service` 现在以 MySQL 为存储，核心表对齐到 `transaction_records`
- 技术方案里“按服务拆库”的思路与当前代码是一致的

这意味着：

- 购买记录不再依赖本地 SQLite 文件
- 查询服务也不再依赖容器内文件数据库
- 后续联调和多服务 compose 场景会更稳定

### 3.2 Payment -> RocketMQ -> Verification

支付成功后通过 RocketMQ 异步通知 Verification，这条链在代码里已经是实装状态：

- `payment-service` 中存在真正的 MQ Producer
- `verification-service` 中存在真正的 RocketMQ Consumer
- Consumer 订阅的 topic 是 `payment_events`
- Consumer 收到消息后会写入 `purchase_records`

这条链是本轮改造最实质性的收获之一，因为它把“支付成功”和“购买关系可验证”真正拆成了最终一致性的两段。

### 3.3 RocketMQ nameserver 的 `IP addr error`

`verification-service/consumer.go` 已经增加了 nameserver 预处理逻辑，会对 `ROCKETMQ_NAMESERVER` 做解析后再传给 `consumer.WithNameServer(...)`，目的就是规避 RocketMQ Go client 对 `IP:port` 的严格校验。

这部分说明：

- 代码层面已经意识到 Docker 场景下 hostname / DNS 的问题
- 不是单纯改 compose，而是服务代码也做了兼容

运行联调时还要注意：

- topic `payment_events` 必须存在
- broker 返回的地址必须对服务容器可达
- infra 与 services 的 Docker network 必须打通

---

## 4. 按服务看，当前真实实现到哪一步了

| 服务 | 当前状态 | 结论 |
| --- | --- | --- |
| API Gateway | 已有路由转发 DID / Payment / Verification / Query | 可作为统一入口，但不包含 X 验证专用链 |
| DID Service | 已实现 DID 创建、钱包生成、签名验证、防重放 | 真实可用 |
| Payment Service | 已实现支付请求、DID 验签、余额检查、调用 Blockchain Adapter、发 MQ 事件 | 主流程基本具备 |
| Blockchain Adapter | 已实现 Solana 余额查询、转账、交易状态查询 | 真实依赖 Solana devnet / 配置 |
| Verification Service | 已实现消费 `payment_events` 并写 `purchase_records`，也可对外验证购买关系 | 主流程可用 |
| Query Service | 已有余额 / 交易 / 收益接口，但余额仍有演示常量，交易返回也未完全组装 | 未完全产品化 |
| X 验证 / 奖励发放 | 在当前微服务代码中未见完整后端实现 | 尚未落地 |

---

## 5. 结合代码的关键判断依据

### 5.1 API Gateway：有统一入口，但没有 X 验证专用后端

Gateway 当前暴露的路由主要是：

- `/api/v1/did`
- `/api/v1/did/verify`
- `/api/v1/pay`
- `/api/v1/pay/:tx_id`
- `/api/v1/pay/history`
- `/api/v1/pay/require`
- `/api/v1/verify`
- `/api/v1/verify/batch`
- `/api/v1/verify/proof`
- `/api/v1/balance`
- `/api/v1/transactions`
- `/api/v1/revenue`

这些路由说明：

- 购买链相关入口是存在的
- Query 查询入口也是存在的
- 但没有看到 PRD 中那种专门用于 X / Twitter 绑定与奖励领取的后端接口

换句话说，Gateway 能串起“购买验证”，但串不起“X 验证领奖”。

### 5.2 DID Service：创建 DID / 钱包、签名验证都是真实现

`did-service/app/did_app_service.go` 里可以明确看到：

- `CreateDID()` 调用了 `solana.NewWallet()`
- DID 采用 `did:solana:{publicKey}` 形式
- 私钥入库前做了加密
- `VerifySignature()` 做了 DID 存在性校验
- `VerifySignature()` 做了时间窗校验
- `VerifySignature()` 做了 nonce 防重放
- 最终执行的是 Ed25519 签名验证

这意味着：

- “创建钱包 / 生成 DID”
- “本地私钥签名后由后端验签”

这两块基础能力是真正成立的，不是 mock。

### 5.3 Payment Service：主购买流程已落地，但 402 响应仍偏占位

`payment-service/internal/application/service/payment_service.go` 里能看到真实的支付流程：

- 校验金额
- 做幂等处理
- 校验 nonce
- 通过 DID Service 做签名验证
- 通过 Blockchain Adapter 查询余额
- 持久化 payment 记录
- 发起链上转账
- 轮询交易状态
- 发布支付成功事件

但是 `GetPaymentRequirement()` 目前仍是偏占位实现，返回内容里：

- `SkillName` 仍是空字符串
- `Price` 仍是 `"0"`
- `Currency` 固定是 `"USDC"`
- `Endpoint` 固定是 `"/api/v1/pay"`

这说明：

- “收到 402 并引导去支付”这个接口形状已经有了
- 但还没有真正做成 PRD 里那种面向具体 skill 资源、可直接支撑前端/Agent 决策的完整协议

### 5.4 Verification Service：购买异步确认链已具备真实闭环

`verification-service/consumer.go` 和 `verification-service/handler.go` 对应的是当前最完整的一段：

- Consumer 订阅 `payment_events`
- 收到消息后解析支付成功事件
- 写入或更新 `purchase_records`
- 提供购买校验、批量校验、购买证明查询接口

这说明：

- Payment 成功后，购买关系最终能沉淀下来
- Gateway 的 `/api/v1/verify`、`/api/v1/verify/batch`、`/api/v1/verify/proof` 有真实后端可接

就“支付后最终一致性”这件事而言，当前代码已经比最初方案更接近生产结构。

### 5.5 Query Service：接口有了，但用户视角的数据还不可信

`query-service/handler.go` 暴露出两个非常关键的问题：

1. 余额还是演示数据

代码里直接写了：

- `resp.BalanceMinor = 10000 * 1000000`
- `resp.MonthlyLimitMinor = 50000 * 1000000`

也就是说，当前 balance 并不是完全由链上余额、奖励入账、购买扣减等真实数据汇总出来的。

2. 交易列表组装不完整

`ListTransactions()` 里虽然构造了 `items`，但当前实现只设置了 `resp.Page.Total`，没有把 `items` 挂回响应对象。

这意味着：

- 即使数据库里查到交易
- API 返回给用户时也可能拿不到真正的交易列表

因此 Query 现在更像“接口骨架 + 部分数据库查询能力”，还不能作为用户侧最终展示结果的可靠依据。

---

## 6. 对照 PRD：A1 首次注册链是否拉通

目标链：

`OpenClaw / 本地 HTTP mock -> API Gateway -> DID Service -> X 验证页 / 验证接口 -> 奖励发放 -> Query`

### 6.1 已经具备的部分

- Gateway 可转发 DID 创建请求
- DID Service 能真实创建 DID 和钱包地址
- 这一步可以返回 `did` 和 `wallet_address`

### 6.2 当前缺失的部分

在当前微服务代码里，没有看到完整实现以下能力：

- `verify?did=...` 对应的真实验证页面后端
- `POST /verify-twitter` 之类的服务实现
- 对 `tweet_url` 的校验逻辑
- DID 与 X 账号绑定记录
- 奖励发放调用链
- 奖励交易记录写入 Query 可读模型
- 查询余额后看到 “1 USDC 已到账” 的真实闭环

### 6.3 结论

`A1 首次注册链` 目前**没有按 PRD 拉通**。

更准确地说：

- “创建 DID / 钱包”已经通了
- “X 验证 + 绑定 + 奖励 + Query 可见”还没有落在当前微服务后端代码里

所以即使 HTTPS / SSL 问题先不看，仅从后端代码现状出发，A1 也还不算闭环。

### 6.4 如果当前阶段接受“跳过逻辑”

如果产品上明确允许本阶段这样降级：

- X 验证先不走真实 HTTPS 页面
- 奖励发放先不走真实链上 / 钱包入账
- Query 先只展示 mock 或占位结果

那么 A1 可以做成“演示版流程”，但应该明确标注为：

`DID 创建已真，X 验证与奖励链为跳过或 mock，不属于正式验收通过`

---

## 7. 对照 PRD：A2 首次购买链是否拉通

目标链：

`OpenClaw / 本地 HTTP mock -> API Gateway -> Payment Service -> DID 验签 -> Blockchain Adapter -> Solana -> RocketMQ -> Verification -> Query`

### 7.1 当前已经成立的部分

从代码现状看，下面这些节点都已经有真实实现：

- Gateway -> Payment
- Payment -> DID 验签
- Payment -> Blockchain Adapter
- Blockchain Adapter -> Solana
- Payment -> RocketMQ
- Verification -> 消费 `payment_events`
- Verification -> 写 `purchase_records`
- Gateway -> Verification 查询购买关系

这说明从“支付发起”到“购买关系最终验证”这一段，已经具备真实主链能力。

### 7.2 当前还不够完整的部分

#### 1. Query 端用户视图未闭环

虽然 Query 有：

- `/api/v1/balance`
- `/api/v1/transactions`
- `/api/v1/revenue`

但当前实现仍存在：

- balance 是演示常量
- transactions 返回组装不完整
- 仓库里没有看到明确的交易可读模型写入链路

因此“支付成功后，用户查询余额减少 / 查询交易记录增加”这部分还不能视为完全可靠。

#### 2. 402 支付协商仍是骨架

PRD 要求的是：

- 第一次访问付费资源返回 402
- 带上金额、skill_did、currency
- Agent 根据本地阈值决定自动购买或二次确认

当前代码里 `GET /api/v1/pay/require` 是存在的，但返回内容还比较骨架化，还不足以证明完整的资源级 402 协议已经落地。

#### 3. 限额配置未在当前微服务里看到完整实现

PRD 里提到用户可配置自动购买阈值 / 月度限额，但在当前微服务代码里没有看到明确的：

- 限额配置接口
- 限额持久化
- Payment 在服务端按限额拒绝或放行的完整逻辑

当前更像是：

- Agent 或客户端侧未来应承担这部分决策
- Query 响应里保留了 `monthly_limit` 相关字段
- 但后端产品化链路还没有真正闭环

### 7.3 结论

`A2 首次购买链` 可以说：

- 后端主干链已经**基本打到 Verification**
- 但还**没有完整打到用户可感知的 Query 展示层**

因此严格按 PRD 来说，A2 目前也**不能算完全拉通**。

更准确的表达应该是：

`购买执行链已基本成立，购买查询链仍待补齐`

---

## 8. 当前最接近“可演示”的联调版本

如果目标是先在 A 机器上做一个“最小能跑”的用户侧演示，建议把范围收敛为下面这版：

### 8.1 可真实演示的部分

1. 创建 DID / 钱包
2. 本地构造支付请求并签名
3. Gateway 转发到 Payment
4. Payment 调 DID 验签
5. Payment 调 Blockchain Adapter 发起链上操作
6. Payment 发送 `payment_events`
7. Verification 消费消息并写 `purchase_records`
8. 调 `/api/v1/verify` 验证用户是否已购买 skill

### 8.2 需要明确标注为“当前跳过 / mock”的部分

1. X 验证页面真实 HTTPS 闭环
2. 推文链接校验
3. DID-X 绑定关系
4. 1 USDC 注册奖励发放
5. 奖励到账后的真实余额展示
6. 购买后 Query 侧真实余额扣减与交易历史完整展示
7. 自动购买阈值 / 高价二次确认的后端产品化支持

---

## 9. 对用户侧联调负责人的建议口径

建议把当前状态分成三层来汇报，不要混成一句“主链已打通”。

### 9.1 可以说“已打通”的

- DID 创建
- 支付签名校验
- 支付执行
- RocketMQ 异步通知
- Verification 购买关系写入与校验

### 9.2 可以说“已具备接口骨架，但还未产品化闭环”的

- HTTP 402 支付要求
- Query 的余额 / 交易 / 收益查询

### 9.3 应该明确说“尚未落地”的

- X 验证真实链
- 注册奖励真实发放
- 奖励到账后的余额闭环
- 限额配置正式链

---

## 10. 后续补齐优先级建议

如果目标是尽快把用户侧主链真正做成可验收状态，建议优先级如下：

### P0：先让 Query 变真

优先把 Query 从“接口壳子”补成真正可验收的数据服务：

1. 去掉 `GetBalanceSummary()` 里的演示常量
2. 明确余额来源
3. 补上 `ListTransactions()` 的 `resp.Items`
4. 建立支付成功、奖励发放到 Query 可读模型的写入链路

原因：如果 Query 不真，用户侧几乎无法证明“买到了”“扣款了”“奖励到了”。

### P1：补注册链的后端闭环

至少需要新增：

1. X 验证提交接口
2. DID 与 X 账号绑定存储
3. 验证成功后的奖励发放服务
4. 奖励交易写入 Query

原因：这一步不是单纯卡在 HTTPS / SSL，上游后端本身也还没落地。

### P2：把 402 / 阈值 / 确认购买做成真正产品流程

至少需要明确：

1. skill 元数据从哪里来
2. 价格从哪里来
3. 自动购买阈值放客户端还是服务端
4. 月限额如何存储和校验

---

## 11. RocketMQ 联调注意事项

这轮迁移里，RocketMQ 相关经验可以沉淀为以下几条：

1. `payment_events` topic 需要提前创建
2. `verification-service` 需要能解析并访问 nameserver
3. broker 回传地址必须在服务容器网络内可达
4. infra/services compose 网络必须对齐
5. `verification-service` 日志里不再出现 `new Namesrv failed.: IP addr error` 才算真正恢复

当前代码已经对 nameserver 的 hostname -> IP 解析做了兼容，但网络拓扑和 topic 准备仍然是运行成功的前提。

---

## 12. 最终判断

结合 `prd.md`、`tech.md` 与当前微服务代码，可以给出最终判断：

- 严格按 PRD 定义，当前用户侧主链**还没有完全拉通**
- `A1 首次注册链` 目前未闭环，核心缺口在 `X 验证 + 奖励发放 + Query 入账`
- `A2 首次购买链` 目前已经基本打到 `Verification`，但 `Query` 还不足以支撑最终验收

如果当前项目阶段允许“先跳过 HTTPS X 验证”和“先跳过真实奖励发放”，那么可以做一个可跑演示版；但在文档、汇报和验收口径上，建议明确称之为：

`购买主干链已基本跑通，注册链和查询展示链仍在补齐中`
