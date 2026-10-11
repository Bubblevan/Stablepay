# StablePay｜Agent Commerce 平台

**稳付科技｜Agent 开发实习生**　2026.01 – 2026.07<br>
**项目：** Agent 自主购物、支付与数字服务交付<br>
**技术栈：** Go、CloudWeGo（Hertz / Kitex / Thrift）、MCP、MySQL、Redis、RocketMQ、Kubernetes、x402、Solana

实习期间参与 ACK 微服务部署与 Labubu 数字商品下单演示；下列其余 Runtime、支付恢复、评测及本地 Kubernetes/性能验证为后续独立开发成果（截至 2026.10）。

- **AgentPay 全链路：** 串联自然语言商品选择、Runtime 报价、买家签名授权、x402 Mock 支付、账本核验与数字服务交付；隔离 MySQL 的 50 项固定任务中 **50/50 正确结束、40/40 可购任务完成交付、0 次重复结算**；另独立完成 **1 笔 0.99 USDC Solana Devnet** 支付与交付验收。
- **商品发现与动态决策：** 面向 **12 家模拟商户、2.4 万条商品**构建自然语言选购与跨商家候选筛选链路，覆盖商品属性、预算、地区/库存核验及报价变化后的重新确认；在公开 Amazon ESCI **500-query** 配对排序评测中，Exact Recall@5 macro **86.96%→88.51%**，NDCG@5 **0.89069→0.90518**。
- **Agent 交易编排与执行控制：** 以持久化状态、幂等标识和补偿/恢复路径编排下单、支付及履约；隔离 MySQL 的 **300 次本地 Mock 购买**完成 **287 单**，**29/29** 次预设瞬态履约故障恢复，未观察到重复订单、支付、结算或履约。
- **Go 微服务与分布式支付：** 基于 CloudWeGo、DID 验签、RocketMQ 与 MySQL 实现授权、结算、验单和崩溃恢复；**160/160** 次隔离恢复试验通过，重复支付事实与预算投影漂移均为 **0**，恢复耗时 P95 **28.8 秒**。
- **云原生部署与性能：** 参与 StablePay 微服务 ACK 部署；使用 Docker Desktop 搭建 **6 节点 Minikube** 并完成 Devnet 支付验收。独立 Docker k6 本机测试在 **200 VU** 下达到 **1,319 HTTP req/s、72.6 completed Episodes/s**，60 秒测量窗错误率 **0%**。
- **端到端评测与业务交付：** 参与 Labubu 数字商品 Agent 下单演示；补充买家授权、拒绝越权及异常边界的 MySQL/API 回归，**9/9 场景通过**。

> 简历主张的实验环境、样本口径及 Mock/Devnet 区分见 [RESUME_EVIDENCE.md](RESUME_EVIDENCE.md)；面试复算说明见 [RESUME_INTERVIEW_NOTES.md](RESUME_INTERVIEW_NOTES.md)。
