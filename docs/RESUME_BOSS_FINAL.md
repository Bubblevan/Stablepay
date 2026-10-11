# StablePay｜BOSS 直聘简历版

**稳付科技｜Agent 开发实习生**　2026.01 – 2026.07<br>
**项目：StablePay｜AI Agent 自主购物与支付执行平台**<br>
**技术栈：** Go、CloudWeGo（Hertz / Kitex / Thrift）、MCP、MySQL、Redis、RocketMQ、Kubernetes、x402、Solana

**项目介绍：** 面向 AI Agent 自主购买数字商品与服务的需求，构建覆盖商品发现、受控交易、支付授权、结算验单、数字服务交付与故障恢复的 Agent Commerce 链路。

- **AgentPay 全链路：** 通过 Commerce Runtime 完成商品发现、x402 报价复核、买家签名确认、PaymentIntent/Ledger、验单与交付；50 条冻结任务的预期结果正确率 **50/50**，可购任务有效交付 **40/40**，报价变化重新确认 **10/10**，重复结算 **0**，任务 P95 **442 ms**。另独立完成 **1 笔 0.99 USDC Solana Devnet 实付**并验证交付；该笔使用 Runtime 应用层 harness 和本地合成商户，不与 50 条 Mock 任务混算。
- **商品发现与动态决策：** 接入 12 家本地模拟商户、2.4 万条商品，组合 BM25 召回、属性/预算/区域过滤及价格库存复核；在隔离 MySQL 的 **500 条锁定合成购物任务**中全部到达预期终态，覆盖 **50** 次价格变化、**50** 次缺货及 **100** 次过期报价拒绝，SQL 不变量违规 **0**（其中 483 次为 Mock 结算、17 次为 Mock 拒付）。这是交易流程正确性结果，目标商品 Recall@5 **94.2%** 与独立有效方案率目标 **72.6%→91.8% 尚未验证**。
- **Agent 交易编排与执行控制：** 基于持久化交易状态与幂等键实现下单、支付、履约及异常恢复；在隔离 MySQL 的 **300 次本地模拟购买**中完成 **287/300** 单，13 单为确定性 Mock 拒付；**29/29** 次预设瞬态履约故障恢复，未观察到重复订单、支付、结算或履约。
- **Go 微服务与分布式支付：** 基于 CloudWeGo、DID 验签、RocketMQ 与 MySQL 交易事实编排支付授权、结算、验单及恢复；在 **8 个崩溃窗口的 160 次隔离恢复试验中完成 160/160**，重复支付事实、预算漂移、orphan/stuck Episode 均为 **0**，TTR P50/P95 为 **0.621/28.807 秒**。
- **云原生部署与性能：** 实习期间将微服务部署至阿里云 ACK（按本人经历）；另在单台 Docker Desktop 的 **6 节点 Minikube** 部署 12 个 Ready 工作负载。Docker k6 本地 **200 VU** 测得 **1,319 HTTP req/s、72.6 Episodes/s**，HTTP P95 **114 ms**、Episode P95 **3.35 秒**、60 秒测量窗错误率 **0%**；该性能结果使用本地确定性依赖，不代表生产容量。
- **端到端评测与业务交付：** 建立覆盖预算/区域约束、报价和库存变化、买家审批及支付交付的自动化评测；9 个买家授权与异常边界 MySQL/API 场景 **9/9 通过**。结合实习期 Labubu 数字商品下单场景，完成 Agent 工具调用、支付确认至结果回传的业务演示。

## 投递与面试口径

- 50 条 Runtime 固定任务是规则解析 + Mock 支付/交付；只有另列的 1 笔 0.99 USDC 是真实 Devnet 交易，且目标是本地合成商户。不得合并为 51 笔链上交易或真实商家接入。
- 商品质量仍有缺口：当前 GPT-6 Luna 生成挑战集未经人工复核，候选与标签来自同一合成目录，已记录的高方案率/Recall 仅是诊断，按证据规范不列作简历指标；目标商品 Recall@5 `94.2%` 和独立有效方案率 `72.6%→91.8%` 尚无合格 holdout 证据。Recall@20 不能替代目标 Recall@5。
- 另有 500-query 公共 ESCI judged-candidate 对照：当前排序器 Exact Recall@5 为 `88.51% macro / 87.19% micro`，BM25 为 `86.96% / 85.35%`；该候选池结果低于 94.2%，且不覆盖 StablePay 全目录、商品可行性或方案生成，故只作面试补充，不填入 Bullet 2 的目标数字。
- Minikube 的六个节点运行于一台 Docker Desktop 主机；ACK 是单独的实习部署经历，不把本地集群说成六台物理机。
- Labubu 场景依据用户提供的实习经历陈述；仓库可见的 `pay_small_item_v2.json` 与 `merchant_labubu_0_2_clean.json` 只是任务/后端状态模板，没有对应执行回执，不能当作本地或链上运行证据。若面试需要量化结果，应补充实习期可公开的脱敏记录，不补造交易量或转化率。

## 证据索引

- Bullet 1 Mock 矩阵：`.local-run/commerce-runtime/m1-fixed-50/m1-runtime-fixed-50-matrix-20261011T165230.796739600Z/`；Devnet 单次验收：`.local-run/unified-business-e2e-devnet-m11-20261012.json`。
- Bullet 2 商品/库存/价格生命周期：`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T080502.937245500Z-http-30dca910123d98dd/`；自动生成挑战集只作诊断，见 `.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T094422.074741800Z/` 及 `docs/RESUME_EVIDENCE.md`。
- Bullet 2 外部排序诊断（500 个 query-disjoint ESCI queries；仅 judged candidates）：`.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T174854.953674800Z/metrics.json`；包含逐 query BM25/当前排序器 Recall@5 以供复算。
- Bullet 3 沙盒购买回放：`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T085605.852589900Z-purchase-replay-35649dd6f5292547/`。
- Bullet 4 崩溃恢复：`.local-run/resume-benchmark/e4-local-20260923T165522Z-8087/full/`。
- Bullet 5 k6：`.local-run/resume-benchmark/e3-steady-state-final/`；Minikube Devnet 验收：`benchmarks/minikube-six-node-devnet/acceptance-report.md`。
- Bullet 6 的 9 场景回归日志：`.local-run/agent-shopping/integration-evaluations/mysql-http-fresh-consent-20261011T081248Z/go-test.jsonl`；Labubu 实习演示为用户提供的经历事实，现存本地引用仅为 `.local-run/agentpay-source-6d4b751/value-test/agentic/{tasks/samples/pay_small_item_v2.json,fixtures/backend_state/templates/merchant_labubu_0_2_clean.json}` 模板。
