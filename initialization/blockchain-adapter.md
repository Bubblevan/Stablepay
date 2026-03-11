我是 StablePay DEMO 的研发人员，目前我已经完成了 StablePay DEMO 的 PRD 设计文档和技术方案文档。

请基于以下 PRD 文档和技术方案文档，产出 StablePay 的单个服务代码仓库：{REPO_NAME}
当前仓库仅对应一个服务：Blockchain Adapter

PRD 文档：{PRD_LINK}
技术方案文档：{TECH_LINK}

说明：
- 如果 larksuite / 飞书链接需要 MCP，请使用 MCP 访问
- 如果任一链接无法访问、内容不一致、或信息缺失，请先停止并明确指出问题，等待我确认后再继续
- 只生成当前这个服务的代码仓库，不要生成整个 StablePay
- 代码注释使用中文
- 必须包含完整单元测试、运行手册、运维手册、模块说明文档、对外接口说明
- 先详细列出技术选型、模块设计、接口设计、依赖关系和目录结构，等我确认后再生成代码

【当前服务】
服务名称：Blockchain Adapter
核心职责：
- Solana 链上交易执行
- Gas 费补贴
- 链上余额查询
- 交易状态查询
- 与 Solana 网络交互
- 为 Payment Service / Query Service 提供区块链能力

【技术要求】
- 技术选型必须与 StablePay DEMO 保持一致，优先遵循技术方案文档
- 使用 Go 语言
- 使用 CloudWeGo 生态
- 该服务优先作为内部 RPC 服务，对外不直接暴露公共 HTTP API（以技术方案为准）
- 仓库结构参考 COLA v5 分层思想
- 要重点考虑：
  - RPC 节点管理
  - 交易构造与提交
  - 交易确认与状态轮询
  - 补贴 Gas 费的流程
  - 失败重试
  - 幂等性
  - 可观测性
  - 链上查询的超时与降级

【必须实现的能力】
1. 执行稳定币转账（按文档中的 Solana / USDC / USDT 方案）
2. 查询钱包余额
3. 查询交易状态 / 交易确认结果
4. 执行或模拟 Gas 费补贴逻辑（以 DEMO 范围为准）
5. 提供给 Payment Service / Query Service 的内部服务接口
6. 单元测试：
   - 交易请求构造
   - 余额查询
   - 状态查询
   - RPC 异常
   - 超时重试
   - 幂等控制

【特别要求】
- 需要明确区分：
  - 业务层支付语义（由 Payment Service 负责）
  - 区块链执行语义（由 Blockchain Adapter 负责）
- Blockchain Adapter 只负责链上执行与查询，不负责支付业务规则判断
- 若 DEMO 阶段不适合直接对真实主网执行，请先提出 Mock / Sandbox / Adapter 抽象方案
- 需要设计统一的链上错误码映射
- 如果技术方案要求支持多链扩展，请在接口设计中预留抽象，但当前只实现 Solana

【输出顺序要求】
第一步：先输出，不要写代码
1. 需求摘要
2. 领域模型设计（Transfer、Balance、TxStatus、GasSubsidy 等）
3. 技术选型清单
4. RPC / 链交互架构设计
5. 内部接口 / RPC 定义设计
6. 错误码与重试设计
7. Mock / 测试策略
8. 运行与部署说明提纲
9. 文档冲突点 / 待确认问题

第二步：在我确认后，再生成完整代码仓库，要求包含：
- 完整 Go 代码
- 配置文件
- RPC / 链交互代码
- Mock 适配层（如适用）
- 单元测试
- README
- 运维手册
- 内部接口说明

检查：
- 只生成 Blockchain Adapter 仓库
- 不要生成其他服务代码
- 文档冲突先列出，不要直接猜