# StablePay｜BOSS 直聘项目经历

**稳付科技｜Agent 开发实习生**　2026.01 – 2026.07<br>
**项目：StablePay｜Agent 自主购物与支付执行**<br>
**技术栈：** Go、CloudWeGo（Hertz / Kitex / Thrift）、MCP、MySQL、Redis、RocketMQ、Kubernetes、x402、Solana

面向 AI Agent 自主购买数字商品与服务，参与构建覆盖商品发现、交易审批、支付结算、验单交付与故障恢复的服务链路；以下分别标明实习期项目工作与后续个人延伸验证。

- **AgentPay 全链路｜后续个人延伸：** 复用 Commerce Runtime 串联商品发现、报价复核、买家签名确认、PaymentIntent/Ledger、验单与数字交付；隔离 Mock E2E 中 **40/40** 可购任务有效交付、重复结算 **0**；另以独立模式完成 **1 笔 0.99 USDC Solana Devnet** 支付与交付验收。
- **商品发现与动态决策｜后续个人延伸：** 在离线评测器中实现 BM25 + 标题覆盖/有序 bigram 混合排序；在公开 ESCI **500-query 锁定候选集**上，Exact Recall@5 macro **86.96%→88.51%**，NDCG@5 **0.89069→0.90518**。
- **Agent 交易编排与执行控制｜后续个人延伸：** 基于持久化交易状态、幂等键和补偿/恢复路径编排下单、支付及履约；在隔离 MySQL 的 **300 次本地 Mock 购买**中完成 **287 单**，**29/29** 次预设瞬态履约故障恢复，未观察到重复订单、支付、结算或履约。
- **Go 微服务与分布式支付｜后续个人延伸：** 基于 CloudWeGo、DID 验签、RocketMQ 与 MySQL 实现支付授权、结算、验单及崩溃恢复；隔离 MySQL 的 **160 次恢复试验全部通过**，重复支付事实、预算漂移及 orphan/stuck Episode 均为 **0**，TTR P50/P95 **0.621/28.807 秒**。
- **云原生部署与性能｜实习期 + 后续个人延伸：** 实习期间参与将微服务部署至阿里云 ACK；后续在单台 Docker Desktop 上搭建 **6 节点 Minikube** 并完成 Devnet 验收；另以 Docker k6 对本机 Runtime（确定性依赖）进行负载测试，**200 VU** 下测得 **1,319 HTTP req/s、72.6 completed Episodes/s**，60 秒测量窗错误率 **0%**。
- **端到端评测与业务交付｜实习期 + 后续个人延伸：** 实习期间参与 Labubu 数字商品 Agent 下单演示；后续补充买家授权与异常边界的 MySQL/API 回归，**9/9 场景通过**。

> 可直接复制以上项目经历；详细样本定义、Mock/Devnet 边界、评测限制与面试复算材料见 [RESUME_EVIDENCE.md](RESUME_EVIDENCE.md) 和 [RESUME_INTERVIEW_NOTES.md](RESUME_INTERVIEW_NOTES.md)。
