# StablePay｜面试追问口径

## 1. AgentPay 全链路

**问：50 条任务、40 次交付和 Devnet 是同一组实验吗？**<br>
答：不是。50 条任务走隔离 MySQL 与确定性 Mock 支付/交付，其中 40 条预期可购任务全部完成有效交付；Devnet 是单独的一笔 0.99 USDC 端到端验收，不能合并成 51 笔或真实商户支付。

**问：怎么防止重复支付？**<br>
答：支付尝试绑定 PaymentIntent、幂等键、请求指纹和 Ledger 事实；固定任务审计中没有重复结算。Devnet 只做一笔，不据此推断重复率或生产可靠性。

## 2. 商品发现与动态决策

**问：Recall@5 和 NDCG@5 测的是什么？**<br>
答：它们来自公开 ESCI 的 500 个 query-disjoint、带 Exact 判断的候选商品集；比较 BM25 基线和当前 BM25 + 标题覆盖/有序 bigram 混合排序。macro Exact Recall@5 从 86.96% 到 88.51%，NDCG@5 从 0.89069 到 0.90518。

**问：这是不是 2.4 万商品全目录的购买成功率？**<br>
答：不是。ESCI 只评测给定的 judged candidate pool，不含未标注全目录负例、实时商家库存/报价、预算与配送可行性，也不测购买执行。它是检索排序指标，不能替代 StablePay 全目录商品召回或有效方案率。

**问：有没有另外测自然语言购买方案质量？**<br>
答：有一组 30 条诊断任务（15 可购、5 应拒绝、10 应澄清），同一批 GPT-6 Luna 解析结果配对比较 BM25 与当前混合策略。有效方案为 `7/15 → 14/15`（`+46.7 pp`；7 条仅 treatment 成功、0 条仅 baseline 成功、1 条两者都失败）；全任务正确终态 `27/30`。但这是 AI 编写、未人工双审的小样本，库存/配送来自合成 overlay，不是商家实况；因此只作为面试补充，不列入简历 Bullet 2。简历主数字仍是 query-disjoint ESCI 500-query 排序指标。

## 3. 交易编排

**问：287/300 表示 300 笔真实交易吗？**<br>
答：不是。它是隔离租户下的本地 Mock 购买/恢复回放：287 笔完成，13 笔是预设的确定性 Mock 拒付；包含请求重放、API 重建和瞬态履约故障注入，不连接 Devnet 或真实支付提供方。

## 4. 崩溃恢复与一致性

**问：160/160 覆盖什么？**<br>
答：8 个预设崩溃窗口、每个窗口 20 次隔离恢复试验。恢复后核对 PaymentIntent、结算 Ledger、预算投影及 Episode 终态；重复支付事实、预算漂移、orphan/stuck Episode 为 0。TTR P50/P95 为 0.621/28.807 秒。

## 5. Kubernetes 与性能

**问：六节点是不是六台物理机？**<br>
答：不是。六个 Minikube 节点由同一台 Docker Desktop 主机承载；这是本地多节点 Kubernetes 环境。ACK 部署属于实习期经历，两者不混为一谈。

**问：k6 数字能代表生产容量吗？**<br>
答：不能。Docker k6 在单台 Windows/Docker Desktop 主机上压测本地 Runtime 与确定性依赖；200 VU、60 秒测量窗结果为 1,319 HTTP req/s、72.6 completed Episodes/s、HTTP P95 114.4 ms、Episode P95 3.349 s、错误率 0%。这是单机基准，不是生产 SLO 或多机容量预测。

## 6. 实习期 Labubu 场景与个人延伸

**问：仓库里能复现 Labubu 业务数据吗？**<br>
答：实习期 Labubu Agent 下单演示是本人对实习经历的陈述；仓库当前保留的是场景/后端状态模板，没有可用于宣称订单量、支付成功率或转化率的执行回执。9/9 是后续独立开发中的买家授权与异常边界 MySQL/API 回归，不是 Labubu 交易指标。

## 额外说明

- E1 DeepSeek 结果是 INTERVIEW_ONLY_NEGATIVE_ABLATION，不属于简历主张；真实调用结果低于规则策略。
- 模拟商户目录与 benchmark 库存 overlay 都是合成数据，不是真实商户、实时报价或真实库存。
- 任何口头数字都以 RESUME_EVIDENCE.md 中的运行清单、manifest、metrics、trial 和审计边界为准。
