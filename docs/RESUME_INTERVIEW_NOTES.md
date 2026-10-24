# StablePay｜面试追问口径

## 1. AgentPay 全链路

**问：50 项任务、40 次交付和 Devnet 是同一批吗？**<br>
答：不是。50 项固定任务走隔离 MySQL 与确定性 Mock 支付/交付：50/50 达到预期终态，其中 40/40 可购任务有效交付；报价变化场景要求重新批准。Devnet 是单独的 0.99 USDC 验收交易，不能并入 Mock 分母。

**问：如何避免重复支付？**<br>
答：支付尝试关联 PaymentIntent、幂等键、请求指纹与 Ledger 事实。固定任务审计中没有重复结算；Devnet 只有一笔，不能据此推断生产重复率。

### Bullet 1 源码攻防（按当前 main 核对）

**90 秒介绍：**<br>
StablePay 面向 Agent Commerce。外部 Agent 负责理解用户目标并发起能力搜索/购买请求；Commerce Runtime 把结构化购买契约持久化为 Episode，再按业务状态驱动商户调用、x402 报价解析、买家授权、PaymentIntent、支付确认、权益核验和交付验证。MCP 是 Agent 接入协议，不是推理引擎；资金事实由支付执行面和链上/账本事实确认，Runtime 不以模型输出代替支付结果。正常路径主要由确定性状态机执行，恢复路径可让规则或 LLM 提出受限建议，再经 Guard 和应用逻辑校验。

**问：从外部 Agent 调用到 FULFILLED，源码里经过什么？**<br>
答：Agent 可先调用 `stablepay.search_catalog`，再把结构化 `AcquireCapabilityRequest` 提交给 `stablepay.acquire`。Runtime 创建 Episode 后，Runner 的 `RunEpisode` 和 `ResumeEpisode` 共用 `run()`：按持久化状态执行发现、商户调用、解析 HTTP 402、验证报价与授权、支付/对账、权益和交付验证。成功必须到 `FULFILLED` 并通过配置的 Validator；HTTP 402、支付成功或 HTTP 200 单独都不等于业务完成。源码入口：`commerce-runtime/internal/api/server.go`、`commerce-runtime/internal/application/service.go::CreateEpisode`、`commerce-runtime/internal/runtime/runner.go::run/step`。

**问：LLM 会不会在每一步自由决定买什么、付不付？**<br>
答：当前 Runtime 主执行路径不是通用 ReAct Loop。`runner.go::selectMerchant` 从候选集按确定性规则选择首个候选；`StateRecovering` 才调用 `ExecuteRecoveryDecision` 选择允许的恢复动作。外部 Agent 可以先搜索并在购买请求中给出明确选择，但这不意味着 Runtime 内部每一步都由 LLM 决策。模型提出的恢复建议不是支付事实，也不能绕过 Guard。

**问：`stablepay.approve` 是买家确认吗？**<br>
答：不是。当前 MCP 共 7 个工具；`stablepay.approve` 记录的是已存在的 parent approval，`stablepay.workflow_approve` 则委托给活动子 Episode 的 parent-decision authority。买家确认报价走 `POST /v1/episodes/{id}/buyer-confirmations`，包含 offer ref、quote hash、金额、币种、接受/拒绝及买家签名；应用层会重建当前报价、逐字段核对、验证签名与有效期，然后才记录确认。商品选择、父级审批和买家对具体报价的签名是三种不同事实。源码：`server.go::mcpTools/mcpCall/buyerConfirmation`、`application/buyer_confirmation.go::RecordBuyerConfirmation`、`internal/buyer/confirmation.go::ApprovalMessage`。

**问：报价变化后旧确认还能用吗？**<br>
答：不应使用。Runner 在准备预留支付前按当前报价查找与同一 `OfferRef` 关联的确认，并调用 `ValidateCurrentBuyerOffer`；过期或报价不匹配会拒绝继续或要求重新确认。`PurchaseSelection` 只确定购买对象，不是付款授权。源码：`runner.go` 的 `StateNegotiating` 分支、`application/payment_runtime.go::ReservePaymentIntentForEpisode`。

**问：支付 RPC 超时，怎么保证 Exactly Once？**<br>
答：我不会声称跨进程或链上端到端严格 Exactly Once。系统用 PaymentIntent 的经济身份/幂等键、持久化状态和 Ledger 事实识别重放；对未知结果应查询/对账已存在 Intent，而不是把超时直接当失败并创建新的经济动作。恢复结果仍以支付服务和链上事实为准。源码：`application/payment_runtime.go`、`internal/decision/payment_guard.go`。

**源码边界，面试时不要说过头：**<br>
当前 MCP `approve` 不能代替签名买家确认；Runtime 默认候选选择不是模型排序；静态 Workflow DAG 不等于已支持并行调度或通用跨步骤退款；Devnet 成功也不证明 Mainnet。当前冻结简历及证据写的是一笔 `0.99 USDC` Devnet 验收，不是 Mainnet。

### SP-B1 外部 Agent E2E 运行前置检查

附件建议的 `真实自然语言 → 外部 Agent Host → MCP → Runtime → 买家签名批准 → Devnet → 验单交付` 是一项新的统一会话验收；现有 50 项 Mock 和单笔 Devnet 证据不能冒充这条统一 Trace。2026-10-15 的实测状态：

- 已用缓存的 MySQL 8.0 镜像启动独立 Docker 容器，数据卷单独隔离，只绑定 loopback `127.0.0.1:13328`；在其中创建 `stablepay_commerce_m1_test`。仓库测试 `TestBuyerConfirmationGatesUnifiedRuntimePaymentFlowMySQL` 通过，覆盖买家确认、报价变更/篡改拒绝、批准前无支付副作用及后续支付/交付门控。该测试使用 Mock 支付适配器，不是 Devnet 交易。
- 买家和卖家 keypair 文件均在位。买家文件通过热钱包 loader 校验；卖家文件是标准 64-byte JSON 数组，曾被旧 loader 拒绝。本轮让 loader 支持该格式并校验 seed 与公钥半部一致，相关单测和卖家钱包校验通过；原始卖家文件未改写。
- `.local-run/simulated-merchants/manifest.json` 的 12 家目录商户仍标记 `purchase_enabled=false`、`payment_ready=false`，不能直接当作可收 Devnet USDC 并交付的商户。当前没有 Runtime 监听 `8090`，也没有启动与该隔离库绑定的完整支付/验单服务栈；仓库默认 `.env` 仍是内存模式和另一套既有 DSN，本轮没有连接或写入那套库。
- `openclaw` 不可执行；Codex CLI 可用，但尚未配置并验证外部 Agent Host 经 MCP 调用此 Runtime。
- 买家/卖家 Devnet SOL 与 USDC 余额尚未核验：沙盒内 RPC 请求失败，浏览器 Explorer 页面显示数据抓取失败，权限审核也超时。失败响应不能解释成余额为零。本轮没有签名、广播或发起新交易。

因此，已得到 MySQL 持久化的 Runtime HTTP 状态机 smoke，但完整的外部 Agent Host + MCP + 可支付商户 + Devnet 统一 Trace 尚未完成。下一阶段应在可访问 Devnet RPC 的环境中先只读核验余额，再启动与独立 MySQL 绑定的依赖栈，并使用一笔明确报价完成验单交付。具体商户、商品、金额、网络和收款地址须绑定在买家签名中；拒绝授权、请求重放和报价变化要作为不产生未授权第二次结算的负例单独验收。

Trace 应自动关联 Agent Session、Episode、候选集、Merchant/SKU、报价哈希、签名确认引用、PaymentIntent、交易签名、验证证据、Artifact 哈希和终态；公开材料只保存必要标识/哈希，禁止写入私钥、API Key、完整签名材料或敏感原始输入输出。

## 2. 商品发现与动态决策

**问：简历里的 12 家、2.4 万商品和排序数字分别说明什么？**<br>
答：12 家模拟商户及 24,000 条商品是本地合成目录，用于商品候选发现、过滤与链路验证，不代表真实商户或线上库存。量化排序结果来自公开 Amazon ESCI 的 500 个 query-disjoint、带 Exact 判断的候选商品集：BM25 基线与当前混合排序配对比较，macro Exact Recall@5 为 86.96%→88.51%，NDCG@5 为 0.89069→0.90518；500 个 query 中 40 个改善、448 个持平、12 个退步。

**问：这是 2.4 万商品全目录召回或购买成功率吗？**<br>
答：不是。ESCI 测的是给定 judged candidate pool 内的排序，不覆盖 StablePay 全目录未标注商品，也不包括实时商家库存、价格、地区可行性、下单或支付。它是公开排序基准指标，不能包装为 StablePay 端到端购买方案率。

**问：为什么没有报告新做的 500 条自然语言购物任务分数？**<br>
答：B2 Natural v1 的 97 条 holdout 确实没有评分；后来我又运行了一套全新的 500 条 v2 合成任务，并用 GPT-6 Luna 做了真实解析。独立 raw-fact oracle 在 350 条 PLAN 任务上测得 BM25 `53/350`、当前策略 `349/350`，但这些 PLAN 任务由同一目录与合成库存 overlay 自动生成、没有独立盲审。更关键的是 baseline 和 treatment 的 Top-5 在 350 条任务上完全相同；所谓 `350/350 Hit@5` 来自商品名直接写进 prompt 且检索先按 item/format 硬路由，不能说明排序提升。因此这组方案率和 Hit@5 只保留为工程诊断，不写成简历质量指标。简历使用独立公开 ESCI 配对排序结果。

**问：Recall@5 macro 只有 20.12%，为什么 Hit@5 却是 100%？**<br>
答：两个统计量的分母不同：每条任务的 exact 相关集合平均有很多同名商品 listing，宏平均 Recall@5 计算 Top-5 命中的相关 listing 数占该任务全部相关 listing 数；Hit@5 只问 Top-5 是否至少含一个相关 listing。这里 350 条任务共有 13,614 个相关 listing、Top-5 共命中 1,723 个（micro Recall@5 12.66%），但每条任务都命中至少一个。由于 prompt 明确含商品名且候选在排序前已经按商品名路由，Hit@5 饱和，不应作为全目录召回或简历指标。

## 3. 交易编排

**问：287/300 是 300 笔真实支付吗？**<br>
答：不是。它是隔离租户下的本地 Mock 购买/恢复回放：287 单完成，13 次为预设确定性 Mock 拒付；29 次注入的瞬态履约故障全部恢复。未连接 Devnet 或真实支付提供方。

## 4. 崩溃恢复与一致性

**问：160/160 覆盖什么？**<br>
答：8 个预设崩溃窗口、每个窗口 20 次隔离恢复试验。恢复后核对 PaymentIntent、结算 Ledger、预算投影及交易终态；重复支付事实、预算漂移和未完成交易均为 0。恢复耗时 P50/P95 为 0.621/28.807 秒。

## 5. Kubernetes 与性能

**问：六节点是六台物理机吗？**<br>
答：不是。六个 Minikube 节点由同一台 Docker Desktop 主机承载，是本地多节点 Kubernetes 环境。ACK 部署属于实习期经历，不与本地 Minikube 混为一谈。

**问：k6 结果能代表生产容量吗？**<br>
答：不能。Docker k6 在单台 Windows/Docker Desktop 主机上压测本地 Runtime 和确定性依赖；200 VU、60 秒测量窗为 1,319 HTTP req/s、72.6 completed Episodes/s、HTTP P95 114.4 ms、Episode P95 3.349 s、错误率 0%。这是单机基准，不是生产 SLO 或多机容量预测。

## 6. Labubu 场景与归属

**问：仓库里能复现 Labubu 业务交易数据吗？**<br>
答：实习期 Labubu Agent 下单演示是本人对实习经历的陈述；仓库保留场景及后端状态模板，没有支持订单量、支付成功率或转化率的执行回执。9/9 是后续独立开发中的买家授权与异常边界 MySQL/API 回归，不是 Labubu 交易指标。

## 额外说明

- DeepSeek 结果标记为 `INTERVIEW_ONLY_NEGATIVE_ABLATION`，不属于简历主张。
- 模拟商户目录和库存 overlay 均为合成数据；真实 Devnet 仅有单笔验收，不与 Mock 实验合并。
- 具体运行清单、散列、统计定义及限制以 [RESUME_EVIDENCE.md](RESUME_EVIDENCE.md) 为准。
