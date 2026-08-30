# StablePay 最终汇报 PPT 事实交接文档

本文只基于当前 Codex 线程内已检查到的仓库代码、配置、测试产物、部署文件与 `Stablepay.pdf` 页面内容整理，不补写未验证事实。若 PPT 讲稿需要“成绩化表述”，应先补充可复现实验或运行截图后再写入。

已核对的主要证据源：

- `D:\MyLab\StablePay\api-gateway`
- `D:\MyLab\StablePay\did-service`
- `D:\MyLab\StablePay\payment-service`
- `D:\MyLab\StablePay\blockchain-adapter`
- `D:\MyLab\StablePay\verification-service`
- `D:\MyLab\StablePay\query-service`
- `D:\MyLab\StablePay\stablepay-openclaw-plugin`
- `D:\MyLab\StablePay\infra-deployment`
- `D:\MyLab\StablePay\merchant`
- `D:\MyLab\StablePay\stablepayAI-documentation`
- `D:\MyLab\StablePay\value-test`
- `D:\MyLab\StablePay\tmp\Stablepay.pdf`

## 2. 项目模块和目录

### API Gateway

- 职责：统一外部入口，按路由策略转发到 DID、Payment、Verification、Query 等后端服务；负责鉴权抽取、签名验签调用、防重放、限流、就绪检查。
- 入口文件：`api-gateway/cmd/api-gateway/main.go`
- 关键实现：
  - 应用装配：`api-gateway/internal/app/bootstrap.go`
  - 路由与 handler：`api-gateway/internal/interfaces/http/router.go`
  - 鉴权与防重放：`api-gateway/internal/interfaces/http/middleware/auth.go`
  - 限流：`api-gateway/internal/interfaces/http/middleware/rate_limit.go`
  - 路由策略：`api-gateway/internal/domain/route.go`
  - 金额归一化：`api-gateway/internal/application/money.go`
- 当前完成度：核心链路已实现，可支撑 health/ready、payment、verification、query 等转发；配置中实际声明的 route 数是 19 条，不是 PPT 里常写的 21 条，需修正。

### DID Service

- 职责：生成和管理 `did:solana:<pubkey>` 身份，提供查询与签名校验能力，维护 DID 状态。
- 入口文件：`did-service/cmd/server/main.go`
- 关键实现：
  - App service：`did-service/app/did_app_service.go`
  - RPC handler：`did-service/adapter/did_handler.go`
  - 持久化模型：`did-service/repository/did_identity_model.go`
  - 测试：`did-service/app/did_app_service_test.go`
- 当前完成度：创建、查询、验签、状态校验已实现；测试覆盖了时间戳过期、重复 nonce、状态禁用等场景。实现是“轻量 DID 服务”，不是完整 DID Document 解析/解析器体系。

### Payment Service

- 职责：接收支付请求，做请求校验、幂等控制、支付状态流转，调用 DID 与 Blockchain Adapter，成功后发布支付事件。
- 入口文件：`payment-service/cmd/payment-service/main.go`
- 关键实现：
  - HTTP 路由：`payment-service/internal/adapter/http/router/router.go`
  - HTTP handler：`payment-service/internal/adapter/http/handler/payment_handler.go`
  - 应用服务：`payment-service/internal/application/service/payment_service.go`
  - 领域校验：`payment-service/internal/domain/service/validator.go`
  - RocketMQ producer：`payment-service/internal/adapter/mq/producer.go`
  - DID RPC client：`payment-service/internal/adapter/rpc/client.go`
- 当前完成度：主支付链路、幂等、支付事件发布已接入当前主程序；但 `DIDServiceClient.Validate()` 仍是 TODO 占位实现，当前直接返回 `true`，因此 Payment Service 内部“二次验签”实际上尚未真正生效。

### Blockchain Adapter

- 职责：屏蔽 Solana RPC、SPL Token 转账、热钱包补贴、交易状态查询等链上细节，对上游暴露统一 RPC。
- 入口文件：`blockchain-adapter/cmd/server/main.go`
- 关键实现：
  - 转账服务：`blockchain-adapter/app/service/transfer_cmd_service.go`
  - 交易审计日志：`blockchain-adapter/app/service/transfer_tx_audit.go`
  - 配置：`blockchain-adapter/config/config.go`
  - RPC handler：`blockchain-adapter/handler.go`
- 当前完成度：余额查询、交易状态、两种转账模式都已落地；签名与 fee payer 审计日志较完整。是否真正上链成功，仍取决于运行环境中的 RPC、热钱包和链上资产配置。

### Verification Service

- 职责：消费 `payment_events` 写入购买记录；提供购买验证、批量验证、购买证明、X 推文验证、奖励状态查询。
- 入口文件：`verification-service/cmd/server/main.go`
- 关键实现：
  - RPC handler：`verification-service/handler.go`
  - RocketMQ consumer：`verification-service/consumer.go`
  - 奖励发放：`verification-service/reward_payout.go`
  - X API client：`verification-service/x_api.go`
- 当前完成度：核心 RPC 与 MQ 消费链路已实现；`GetPurchaseProof` 里金额、币种、tx hash 的完整回填仍留有 TODO；奖励发放依赖支付服务内部接口和环境变量。

### Query Service

- 职责：提供余额摘要、交易记录、收益摘要、销售列表，并支持内部账本同步。
- 入口文件：`query-service/main.go`
- 关键实现：
  - RPC handler：`query-service/handler.go`
  - HTTP sync：`query-service/http_server.go`
  - 幂等 upsert：`query-service/sync.go`
  - 链上余额查询：`query-service/chain_balance.go`
  - Sales 查询：`query-service/sales.go`
- 当前完成度：主要查询能力已实现；`MonthlySpentMinor` 字段名与 SQL 实现不一致，当前统计的是历史累计 PURCHASE，不是按月过滤后的消费。

### OpenClaw Plugin

- 职责：将 StablePay 能力注册成 OpenClaw 可调用工具，负责本地钱包、签名、onboarding、支付策略与商家交互。
- 入口文件：
  - Manifest：`stablepay-openclaw-plugin/openclaw.plugin.json`
  - 主逻辑：`stablepay-openclaw-plugin/src/index.ts`
- 关键实现：
  - 工具注册：`stablepay-openclaw-plugin/src/tools/registry.ts`
  - Onboarding：`stablepay-openclaw-plugin/src/onboard.ts`
  - 支付结算：`stablepay-openclaw-plugin/src/pay_settlement.ts`
  - 运行时加密状态：`stablepay-openclaw-plugin/src/runtime.ts`
  - Doctor：`stablepay-openclaw-plugin/src/doctor.ts`
- 当前完成度：OpenClaw 插件是当前最成熟形态；manifest 与 registry 都显示当前工具数为 18 个，不是部分旧文档中的 17 个。

### MCP SDK

- 职责：把同一套工具暴露成 MCP server，供 Claude Code、Codex、Cursor 等通过 stdio 调用。
- 入口文件：`stablepay-openclaw-plugin/src/mcp.ts`
- 关键实现：
  - 使用官方包：`@modelcontextprotocol/sdk`
  - 通过 `buildToolDefinitions()` 复用同一份工具定义：`stablepay-openclaw-plugin/src/tools/registry.ts`
- 当前完成度：代码内已实现 MCP server 形态，不再是“完全没有代码”；但它仍与 OpenClaw 插件共仓，尚未抽成独立 SDK 产品。`src/mcp.ts` 的注释和版本字符串存在滞后，不能直接当作 PPT 文案事实。

### NPX CLI

- 职责：给开发者或非宿主环境提供命令行入口，支持 doctor、onboard、status、balance、sales、pay、merchant、mcp 等命令。
- 入口文件：`stablepay-openclaw-plugin/src/cli.ts`
- 关键实现：
  - 包声明：`stablepay-openclaw-plugin/package.json`
  - 二进制名：`bin.stablepay = ./dist/cli.cjs`
- 当前完成度：CLI 实际已存在且支持交互式 onboarding；但产品文档里仍有“未来形态”的旧说法，讲稿里应以代码现状为准，并说明独立产品化仍在打磨。

### K8s / ACK / ACR / ALB / RocketMQ / MySQL / Redis

- 职责：承载容器部署、镜像仓库、入口流量、消息队列与状态存储。
- 入口文件与配置位置：
  - ACK 总览：`infra-deployment/k8s/ack/README.md`
  - Ingress：`infra-deployment/k8s/ack/platform/ingress.yaml`
  - RocketMQ topic init job：`infra-deployment/k8s/ack/infra/rocketmq-topic-init-job.yaml`
  - 各服务 deployment / service：`infra-deployment/k8s/ack/apps/*`
  - API Gateway 配置中的 Redis/MySQL/服务地址：`api-gateway/configs/config.yaml`
  - Payment 配置中的 MySQL/Redis/RocketMQ：`payment-service/config/config.yaml`
- 当前完成度：ACK/ALB/ACR/Ingress/YAML 均在仓库中；RocketMQ、MySQL、Redis 作为依赖均有接线配置。需要注意：已检查到的 topic init job 当前只初始化 `payment_events`，不能在 PPT 中写成“已自动补全 payment_events、capability_issued、payment_retry 等全部 topic”。

## 3. 针对 Stablepay.pdf 的逐页事实映射

### 第1页

- 页面主题：封面页。
- 有代码或运行结果支持的表述：无须代码支撑。
- 需要修正：无。
- 对应证据：`tmp/Stablepay.pdf` 第 1 页文本。

### 第2页

- 页面主题：目录页。
- 有代码或运行结果支持的表述：无须代码支撑。
- 需要修正：目录编号应和最终页序同步核对。
- 对应证据：`tmp/Stablepay.pdf` 第 2 页文本。

### 第3页

- 页面主题：项目背景、跨境支付痛点、AI Agent 无法直接适配传统金融身份体系。
- 有代码或运行结果支持的表述：
  - 项目确实围绕 DID、本地钱包、签名支付、402 challenge 设计，这和“AI Agent 自主支付”方向一致。
- 需要修正：
  - “公司背景”“团队履历”属于业务背景，不在代码仓库内，不能说成“代码已证明”。
- 对应证据：
  - `stablepay-openclaw-plugin/src/runtime.ts`
  - `did-service/app/did_app_service.go`
  - `merchant/README.md`

### 第4页

- 页面主题：从“人类支付”到“Agent 支付”的技术概念切换。
- 有代码或运行结果支持的表述：
  - 本地钱包、DID、签名、402 challenge、链上支付都有对应实现。
- 需要修正：
  - 这页偏概念解释，不宜写成“已经商业验证完成”。
- 对应证据：
  - `stablepay-openclaw-plugin/src/pay_settlement.ts`
  - `did-service/adapter/did_handler.go`
  - `payment-service/internal/application/service/payment_service.go`

### 第5页

- 页面主题：产品定位、ClawHub / Skill 市场切入。
- 有代码或运行结果支持的表述：
  - 插件和商家侧示例后端确实存在，能支持“未购买返回 402，支付后重试”的闭环。
- 需要修正：
  - “主宰 Web3 时代支付逻辑”属于愿景，不是当前事实。
- 对应证据：
  - `merchant/README.md`
  - `merchant/cmd/merchant-server/main.go`
  - `stablepay-openclaw-plugin/src/tools/registry.ts`

### 第6页

- 页面主题：核心技术方案总览。
- 有代码或运行结果支持的表述：
  - API Gateway、DID、Payment、Blockchain、Verification、Query、Plugin、Docs、Merchant、K8s 这些模块在仓库中均存在。
- 需要修正：
  - 若此页写“6 个 Go 微服务 + 官网 + MCP SDK + npx CLI 全部正式上线”，则不严谨。更准确口径是：6 个后端服务代码齐全；OpenClaw 插件最成熟；MCP/CLI 已有代码实现但产品化程度不一。
- 对应证据：
  - 见第 2 节各模块入口文件。

### 第7页

- 页面主题：后端微服务总览。
- 有代码或运行结果支持的表述：
  - 六个核心服务都存在独立目录和入口。
  - 网关到 Payment，再到 DID / Blockchain / Verification / Query 的调用关系能在代码中找到。
- 需要修正：
  - 若图中把 Query 画成消费 `payment_events` 的主路径，需要说明当前“内部账本同步”走的是 `POST /internal/transactions/sync`，Verification 负责 purchase_records，Query 负责查询和 upsert。
- 对应证据：
  - `api-gateway/internal/interfaces/http/router.go`
  - `payment-service/internal/application/service/payment_service.go`
  - `verification-service/consumer.go`
  - `query-service/http_server.go`

### 第8页

- 页面主题：API Gateway。
- 有代码或运行结果支持的表述：
  - 中间件拆成 `AuthExtract` 与 `AuthVerify` 两段。
  - 防重放使用 DID + nonce + timestamp + path + method 指纹。
  - Redis 不可用时支持内存 fallback。
  - 路由策略包含鉴权模式与限流配置。
- 需要修正：
  - 路由总数应改为 19 条，不是 21 条。
- 对应证据：
  - `api-gateway/internal/interfaces/http/middleware/auth.go`
  - `api-gateway/internal/interfaces/http/middleware/rate_limit.go`
  - `api-gateway/internal/app/bootstrap.go`
  - `api-gateway/internal/domain/route.go`
  - `api-gateway/configs/config.yaml`

### 第9页

- 页面主题：DID Service。
- 有代码或运行结果支持的表述：
  - DID 是 `did:solana:<base58(pubkey)>`
  - 支持 active / disabled 状态
  - 验签会检查时间戳与 nonce
- 需要修正：
  - 不宜写成“完整 W3C DID 标准解析器”。当前更接近“基于 W3C DID 命名风格的轻量实现”。
- 对应证据：
  - `did-service/app/did_app_service.go`
  - `did-service/app/did_app_service_test.go`
  - `did-service/repository/did_identity_model.go`

### 第10页

- 页面主题：Payment Service。
- 有代码或运行结果支持的表述：
  - 有幂等键、request hash 对比、状态流转、RocketMQ 事件发布。
  - 主程序路径里 MQ producer 已接入，而不只是日志打印。
- 需要修正：
  - “Payment 调 DID 验签”这句话要加限定：代码接口已接线，但 `payment-service/internal/adapter/rpc/client.go` 当前 `Validate()` 直接返回 true，Payment 内部二次验签尚未真正完成。
  - 若页内仍引用根目录旧版 `handler.go` 中的 `noopEventPublisher` 口径，应改成“旧实现中曾预留 noop publisher；当前主入口已接 RocketMQ producer”。
- 对应证据：
  - `payment-service/cmd/payment-service/main.go`
  - `payment-service/internal/application/service/payment_service.go`
  - `payment-service/internal/adapter/mq/producer.go`
  - `payment-service/internal/adapter/rpc/client.go`
  - `payment-service/handler.go`

### 第11页

- 页面主题：Blockchain Adapter。
- 有代码或运行结果支持的表述：
  - 实现了 buyer partial-signed tx + hot wallet 补 fee payer 的模式。
  - 也支持纯托管热钱包转账模式。
  - 有统一 base64 tx 审计日志。
- 需要修正：
  - 不宜夸大为“所有链已支持”，当前代码聚焦 Solana / SPL Token。
- 对应证据：
  - `blockchain-adapter/app/service/transfer_cmd_service.go`
  - `blockchain-adapter/app/service/transfer_tx_audit.go`
  - `blockchain-adapter/config/config.go`

### 第12页

- 页面主题：Verification Service。
- 有代码或运行结果支持的表述：
  - 订阅 `payment_events`
  - 写入 `purchase_records`
  - 提供 X 验证和奖励状态查询
- 需要修正：
  - `GetPurchaseProof` 目前并未完整回填 amount/currency/txhash，应避免说成“proof 字段全部完善”。
  - 若写“真实 X API 一定调用成功”，也不严谨；代码允许在未配置 key 时走 mock 内容。
- 对应证据：
  - `verification-service/consumer.go`
  - `verification-service/handler.go`
  - `verification-service/x_api.go`

### 第13页

- 页面主题：Query Service。
- 有代码或运行结果支持的表述：
  - 余额、交易、收益、sales 都有接口实现。
  - 支持内部 `/internal/transactions/sync` upsert。
  - 链上余额查询被做成独立逻辑，失败时不阻塞主查询。
- 需要修正：
  - 若写“月消费统计”，需要改成“当前字段名叫 MonthlySpentMinor，但实现还没有加月份过滤”。
- 对应证据：
  - `query-service/handler.go`
  - `query-service/http_server.go`
  - `query-service/sync.go`
  - `query-service/chain_balance.go`

### 第14页

- 页面主题：云端部署总体链路。
- 有代码或运行结果支持的表述：
  - ACK、Ingress、ALB、ACR、RocketMQ、各服务部署 YAML 均存在。
  - `ai.wenfu.cn` Ingress 路由确实指向 gateway、docs、merchant、frontend。
- 需要修正：
  - 若页内写“所有服务都经过同一条 Codeup 流水线已自动化”，需要配合真实流水线截图或配置；当前仓库主要能证明 K8s 部署与镜像发布目标，不直接证明某个 SaaS 流水线一定在线。
- 对应证据：
  - `infra-deployment/k8s/ack/README.md`
  - `infra-deployment/k8s/ack/platform/ingress.yaml`
  - `infra-deployment/k8s/ack/apps/*`

### 第15页

- 页面主题：CI/CD 与运维亮点。
- 有代码或运行结果支持的表述：
  - 存在 topic init job。
  - YAML 层面可以做 rollout / rollback 相关部署控制。
- 需要修正：
  - 目前核实到的 topic init job 只初始化 `payment_events`，不能说“payment_events、capability_issued、payment_retry 等全部 topic 都已自动化初始化”。
  - 若写“全流程无人值守发布效率提升 xx%”，需先有量化记录。
- 对应证据：
  - `infra-deployment/k8s/ack/infra/rocketmq-topic-init-job.yaml`
  - `infra-deployment/k8s/ack/apps/*`

### 第16页

- 页面主题：Agentic Plugin。
- 有代码或运行结果支持的表述：
  - OpenClaw 插件、MCP server、CLI 三种入口代码都存在。
  - 当前工具数是 18。
  - 工具注册、doctor、onboarding、pay、merchant 购买链路均有实现。
- 需要修正：
  - 若页内写“官方 MCP SDK TODO、npx CLI TODO，只有 OpenClaw 上线”，需要更新口径：更准确说法是“OpenClaw 形态最成熟；MCP 与 CLI 已编码实现，但仍处于共仓演进与产品化打磨阶段”。
- 对应证据：
  - `stablepay-openclaw-plugin/openclaw.plugin.json`
  - `stablepay-openclaw-plugin/src/tools/registry.ts`
  - `stablepay-openclaw-plugin/src/mcp.ts`
  - `stablepay-openclaw-plugin/src/cli.ts`

### 第17页

- 页面主题：客户端演示视频占位。
- 有代码或运行结果支持的表述：仓库能支撑录制此视频，但当前 PDF 这一页本身不是事实描述页。
- 需要修正：若没有实际视频，不要在讲稿里暗示“已向外部用户演示过完整线上版本”。
- 对应证据：
  - `stablepay-openclaw-plugin/src/cli.ts`
  - `merchant/README.md`

### 第18页

- 页面主题：MCP Server Tools 工具矩阵。
- 有代码或运行结果支持的表述：
  - 当前工具数确认为 18。
  - runtime / wallet / did / limits / merchant / pay / verify / doctor / onboard 等工具都在 manifest 与 registry 中。
- 需要修正：
  - PDF 中若仍写 17 个，需要改成 18 个。
  - 若只列 9 个“面向用户工具”，建议额外说明其余是运行时、商家、manual、doctor、merchant 支撑工具。
- 对应证据：
  - `stablepay-openclaw-plugin/openclaw.plugin.json`
  - `stablepay-openclaw-plugin/src/tools/registry.ts`

### 第19页

- 页面主题：Onboarding 状态机。
- 有代码或运行结果支持的表述：
  - `local_config -> master_key -> backend -> signing_runtime -> wallet -> did -> x_verification -> reward_claim -> payment_limits -> balance_or_funding`
  - Doctor 只读，不会写文件。
  - session 有持久化与 24h 过期。
- 需要修正：
  - 若页内还少写 `x_verification` 和 `reward_claim`，应补上当前代码态。
- 对应证据：
  - `stablepay-openclaw-plugin/src/onboard.ts`
  - `stablepay-openclaw-plugin/src/doctor.ts`
  - `stablepay-openclaw-plugin/docs/ONBOARDING_STATE_MACHINE.md`

### 第20页

- 页面主题：Agent 支付端到端链路。
- 有代码或运行结果支持的表述：
  - 商家返回 402 challenge
  - 客户端本地签名
  - 走 gateway / payment / blockchain / verification
  - 购买成功后重试业务请求
- 需要修正：
  - 若讲“Payment 内部也做了真正的 DID 二次验签”，需诚实说明当前这个校验点还是 TODO stub。
- 对应证据：
  - `merchant/README.md`
  - `stablepay-openclaw-plugin/src/pay_settlement.ts`
  - `payment-service/internal/adapter/rpc/client.go`
  - `verification-service/consumer.go`

### 第21页

- 页面主题：插件层安全设计。
- 有代码或运行结果支持的表述：
  - 非成功支付返回 `failed` / `policy_denied` / `manual_confirmation_required`
  - 超阈值支付需要显式确认
  - 本地状态使用 AES-256-GCM 加密
  - debug log 走 stderr
- 需要修正：
  - 若写“完全杜绝所有误支付风险”，表述过满；更准确是“在工具层显式降低 LLM 误判导致的误支付风险”。
- 对应证据：
  - `stablepay-openclaw-plugin/src/pay_settlement.ts`
  - `stablepay-openclaw-plugin/src/index.ts`
  - `stablepay-openclaw-plugin/src/runtime.ts`
  - `stablepay-openclaw-plugin/src/plugin_log.ts`

### 第22页

- 页面主题：项目产出总览、测试用户画像。
- 有代码或运行结果支持的表述：
  - 用户端、开发者端、后端、上云这些产物在仓库里都有代码或配置对应。
- 需要修正：
  - “测试人员 10 人”属于外部事实，仓库代码无法证明，只能作为团队自述。
  - “1 USDC 奖励到账”“余额从 50 变 47/32”若无链上截图或录屏，不应说成已被本线程复核。
- 对应证据：
  - 代码证据见后续各页；人数与余额演示需外部材料。

### 第23页

- 页面主题：用户侧闭环。
- 有代码或运行结果支持的表述：
  - 初始化、建钱包、注册 DID、生成验证链接、查询奖励状态、配置限额、支付购买都有工具入口。
- 需要修正：
  - 这页如果按“完整真实 demo 已全程跑通”来讲，最好配合现场命令输出或录屏；当前文档只能证明“代码实现了这条流程”。
- 对应证据：
  - `stablepay-openclaw-plugin/src/cli.ts`
  - `stablepay-openclaw-plugin/src/tools/registry.ts`
  - `stablepay-openclaw-plugin/src/onboard.ts`

### 第24页

- 页面主题：开发者侧闭环 + Mintlify。
- 有代码或运行结果支持的表述：
  - 商家示例后端存在，且 README 明确写了 402 challenge 流程。
  - Mintlify 文档站存在，`docs.json` 给出导航与 API reference。
- 需要修正：
  - 若页内写“开发者接入已对外大规模使用”，没有仓库证据支持。
- 对应证据：
  - `merchant/README.md`
  - `merchant/cmd/merchant-server/main.go`
  - `stablepayAI-documentation/docs.json`

### 第25页

- 页面主题：后端与云上运行。
- 有代码或运行结果支持的表述：
  - 6 个服务和云侧 YAML 存在。
  - `healthz` / `readyz` 有 k6 测试摘要文件。
- 需要修正：
  - 若页内写“支付链路压测稳定通过”，当前 `value-test/backend/reports/task-02/pay-require-summary.json` 与 `verify-short-summary.json` 不能支撑这个强表述。
- 对应证据：
  - `value-test/backend/reports/task-02/healthz-summary.json`
  - `value-test/backend/reports/task-02/readyz-summary.json`
  - `value-test/backend/reports/task-02/pay-require-summary.json`
  - `value-test/backend/reports/task-02/verify-short-summary.json`

### 第26页

- 页面主题：学到的技术栈与体验总结。
- 有代码或运行结果支持的表述：
  - CloudWeGo、Kitex、Hertz、Thrift、RocketMQ、Redis、MySQL、K8s、Solana、MCP、OpenClaw、Mintlify 都在仓库中有对应使用。
- 需要修正：
  - 这页属于总结页，不要虚构“性能提升 xx%”“效率提升 xx%”。
- 对应证据：
  - 见各仓库依赖、入口和配置文件。

### 第27页

- 页面主题：致谢与展望。
- 有代码或运行结果支持的表述：无须代码支撑。
- 需要修正：
  - 如果展望里写“capability token 已完成”，不准确；当前更像路线图方向。
- 对应证据：
  - `stablepay-openclaw-plugin/src/mcp.ts`
  - 现有仓库未见 capability token 全链路上线证据。

### 第28页

- 页面主题：结束页。
- 有代码或运行结果支持的表述：无须代码支撑。
- 需要修正：无。
- 对应证据：`tmp/Stablepay.pdf` 第 28 页文本。

## 6. Agent 客户端真实演示流程

说明：以下内容按当前代码能够支撑的真实链路整理，命令与返回字段来自源代码；本线程没有重新完整实机跑通一遍链上支付，因此凡是“成功到账”“链上确认”的口径都应在现场以截图或录屏补证。

### 6.1 初始化

- 命令：
  - `npx stablepay-agentpay-dev doctor`
  - `npx stablepay-agentpay-dev onboard --interactive`
- 对应工具：
  - `stablepay_doctor`
  - `stablepay_onboard`
- 代码位置：
  - `stablepay-openclaw-plugin/src/cli.ts`
  - `stablepay-openclaw-plugin/src/doctor.ts`
  - `stablepay-openclaw-plugin/src/onboard.ts`
- 预期返回：
  - doctor 返回各检查项状态，如 backend、master_key、wallet、did、x_verification、reward、payment_limits。
  - onboard 返回当前 step、session_id、下一步建议。
- 失败分支：
  - 后端不可达
  - 本地 master key 缺失或不可读
  - session 过期，需要重新开始

### 6.2 钱包

- 命令：
  - `stablepay_create_local_wallet`
  - 或 `stablepay_bind_existing_wallet`
- 对应工具：
  - `stablepay_create_local_wallet`
  - `stablepay_bind_existing_wallet`
- 代码位置：
  - `stablepay-openclaw-plugin/src/tools/registry.ts`
  - `stablepay-openclaw-plugin/src/runtime.ts`
- 预期返回：
  - 钱包地址、公钥信息、本地状态更新。
- 失败分支：
  - 加密状态文件写入失败
  - 提供的钱包私钥格式不合法

### 6.3 DID

- 命令：
  - `stablepay_register_local_did`
- 对应工具：
  - `stablepay_register_local_did`
- 代码位置：
  - `stablepay-openclaw-plugin/src/tools/registry.ts`
  - `did-service/adapter/did_handler.go`
- 预期返回：
  - `did:solana:<pubkey>`
  - 后端登记成功状态
- 失败分支：
  - 未先创建/绑定钱包
  - DID 服务不可用
  - 重复注册或存储失败

### 6.4 X verification

- 命令或动作：
  - `stablepay_generate_verify_link`
  - 浏览器打开验证页
  - 提交 tweet URL
- 对应工具：
  - `stablepay_generate_verify_link`
  - `stablepay_get_verify_status`
- 代码位置：
  - `stablepay-openclaw-plugin/src/tools/registry.ts`
  - `verification-service/handler.go`
  - `verification-service/x_api.go`
- 预期返回：
  - 验证链接
  - 或状态字段：`verified`, `reward_amount_minor`, `reward_tx_id`
- 失败分支：
  - 推文中不含钱包地址
  - 该 DID 已领过奖励
  - 同一 X 账号已绑定其他 DID
  - X API 未配置时只能走 mock 流程

### 6.5 reward

- 命令：
  - `stablepay_get_verify_status`
  - `stablepay_query_balance`
- 对应工具：
  - `stablepay_get_verify_status`
  - `stablepay_query_balance`
- 代码位置：
  - `verification-service/reward_payout.go`
  - `stablepay-openclaw-plugin/src/tools/registry.ts`
- 预期返回：
  - 奖励状态
  - 可能出现 `reward_tx_id`
  - 余额查询结果
- 失败分支：
  - `ALLOW_SYNTHETIC_REWARD` 未打开且 payment service 内部打款接口失败
  - `PAYMENT_SERVICE_API_KEY` 缺失
  - Treasury wallet 未配置

### 6.6 payment limits

- 命令：
  - `stablepay_configure_payment_limits`
- 对应工具：
  - `stablepay_configure_payment_limits`
- 代码位置：
  - `stablepay-openclaw-plugin/src/tools/registry.ts`
  - `stablepay-openclaw-plugin/src/runtime.ts`
- 预期返回：
  - 单笔限额、自动购买阈值等策略持久化成功。
- 失败分支：
  - 参数不合法
  - 本地加密状态写入失败

### 6.7 402 challenge

- 命令：
  - `stablepay_merchant_list_products`
  - `stablepay_merchant_buy_product`
  - 或直接访问示例商家后端 `/execute`
- 对应工具：
  - `stablepay_merchant_list_products`
  - `stablepay_merchant_buy_product`
- 代码位置：
  - `stablepay-openclaw-plugin/src/tools/registry.ts`
  - `merchant/README.md`
  - `merchant/cmd/merchant-server/main.go`
- 预期返回：
  - 未购买时商家后端返回 `HTTP 402 Payment Required`
  - 返回支付所需的 SKU / amount / payee 等信息
- 失败分支：
  - 商家服务不可用
  - 商品不存在
  - 商家未正确返回 402 challenge

### 6.8 本地签名

- 命令：
  - `stablepay_sign_message`
  - 或由 `stablepay_pay_via_gateway` / `stablepay_merchant_buy_product` 内部自动完成
- 对应工具：
  - `stablepay_sign_message`
  - `stablepay_pay_via_gateway`
- 代码位置：
  - `stablepay-openclaw-plugin/src/pay_settlement.ts`
  - `stablepay-openclaw-plugin/src/runtime.ts`
- 预期返回：
  - 本地对业务消息与网关请求串完成 Ed25519 签名
  - 构造 partial-signed transaction 或支付请求体
- 失败分支：
  - 未配置钱包
  - 超过阈值但未提供 `confirm_over_threshold: true`
  - 命中本地 policy，返回 `policy_denied` 或 `manual_confirmation_required`

### 6.9 链上支付

- 命令：
  - `stablepay_pay_via_gateway`
  - 或 `stablepay_merchant_buy_product`
- 对应工具：
  - `stablepay_pay_via_gateway`
  - `stablepay_merchant_buy_product`
- 代码位置：
  - 客户端：`stablepay-openclaw-plugin/src/pay_settlement.ts`
  - Gateway：`api-gateway/internal/interfaces/http/router.go`
  - Payment：`payment-service/internal/application/service/payment_service.go`
  - Blockchain：`blockchain-adapter/app/service/transfer_cmd_service.go`
- 预期返回：
  - 成功时返回 settled / tx_id / tx_hash / amount 等信息
  - 失败时返回 `failed`、`policy_denied`、`manual_confirmation_required`
- 失败分支：
  - Gateway 鉴权失败
  - Payment 缺少 `X-Idempotency-Key`
  - Blockchain RPC 或热钱包配置失败
  - 余额不足
  - 链上确认超时

### 6.10 verify

- 命令：
  - 商家侧在业务执行前调用 Verification
  - 客户端可用 `stablepay_query_sales` 或重新请求商品执行
- 对应工具 / RPC：
  - `VerifyPurchase`
  - `BatchVerifyPurchase`
  - `GetPurchaseProof`
- 代码位置：
  - `verification-service/consumer.go`
  - `verification-service/handler.go`
- 预期返回：
  - 若消费成功，`purchase_records` 中存在记录，返回 `purchased: true`
- 失败分支：
  - MQ 消费延迟，短时间内仍查不到购买记录
  - Proof 字段不完整

### 6.11 业务请求重试

- 命令：
  - `stablepay_merchant_buy_product` 内部完成重试
  - 或支付成功后重新请求商家 `/execute`
- 对应工具：
  - `stablepay_merchant_buy_product`
- 代码位置：
  - `stablepay-openclaw-plugin/src/tools/registry.ts`
  - `merchant/README.md`
- 预期返回：
  - 第一次拿到 402
  - 支付后第二次业务请求拿到 200 和业务结果
- 失败分支：
  - Verification 尚未落库，业务仍被拒绝
  - 商家侧 verify 接线错误

## 7. 团队分工

以下分工来自用户提供口径，应在最终答辩前由团队成员再确认一次：

- 包博文：API Gateway、Payment Service、OpenClaw plugin、从 ECS 到 K8S
- 贾越：DID、Blockchain、Mintlify、harness
- 杨世博：Verification Service、Query Service、X app、ACK

建议讲稿写法：

- 不把所有成果都说成“我独立完成”
- 对自己负责部分可讲深
- 对他人负责模块讲“协作接口、联调关系、我参与到哪里”

## 8. 答辩风险

### 8.1 老师可能追问的 20 个问题与准确口径

1. 你们这个项目到底解决什么问题？
- 准确回答：解决的是 AI Agent 没有传统银行账户、身份证和人工交互流程时，如何完成“可签名、可验证、可结算”的自主支付问题。当前实现是用本地钱包 + `did:solana` + x402 challenge + Solana USDC 支付闭环来落地这个问题。

2. 为什么不用传统支付，而要用稳定币？
- 准确回答：代码实现上，Agent 本地持有密钥、直接做 Ed25519 签名与链上转账更自然；传统银行卡/KYC/密码体系是为“人”设计的，不适合 Agent 自主完成。这里是技术路径选择，不代表已经完成合规产品化。

3. 为什么选 Solana？
- 准确回答：当前仓库实现聚焦 Solana / SPL Token / USDC，链路里大量代码都直接依赖 Solana 交易结构和 ATA 概念，例如 `blockchain-adapter/app/service/transfer_cmd_service.go`。不是“多链通用引擎”。

4. 为什么需要 DID Service？
- 准确回答：因为系统里需要一个机器可持有、可签名、可校验、可禁用的身份标识。当前 DID Service 负责把本地公钥映射成 `did:solana:<pubkey>` 并提供验签能力。

5. 这是不是完整 W3C DID 实现？
- 准确回答：不是。当前实现借用了 W3C DID 的标识风格，但没有完整 DID Document、verificationMethod 解析、service endpoint 展开和 resolver 体系，更接近一个轻量化 DID 注册与验签服务。

6. 为什么拆成 6 个微服务，不做单体？
- 准确回答：从代码上看，身份、支付、链交互、验证、查询、网关职责边界是清楚的，拆开后更利于独立部署和联调。特别是 DID 作为底层依赖被别的服务调用，但它自己不依赖别人，依赖图更干净。

7. API Gateway 具体做了什么？
- 准确回答：统一路由、抽取身份头、调用 DID 服务验签、防重放、限流、readiness/health 检查、转发下游服务。证据在 `api-gateway/internal/app/bootstrap.go` 和各 middleware 文件。

8. 你们 PPT 里写 21 条路由，真的是 21 条吗？
- 准确回答：按当前 `api-gateway/configs/config.yaml` 实际声明数是 19 条，这里 PPT 需要修正，不能硬讲 21 条。

9. 支付怎么避免重复扣款？
- 准确回答：Payment Service 要求 `X-Idempotency-Key`，内部会把 agent_did + payee + client key 生成幂等键，并缓存 request hash。如果同一 key 对应不同 body，会拒绝；相同 body 则复用已有支付结果。

10. nonce、防重放是怎么做的？
- 准确回答：网关层把 DID、nonce、timestamp、path、method 组合成指纹写 Redis；相同指纹在 TTL 内再次出现就会被拦。DID Service 验签时也要求时间窗口和 nonce。

11. 既然 API Gateway 已经验签了，Payment Service 为什么还调用 DID？
- 准确回答：设计意图是做服务内二次校验，避免有人绕过 gateway 直接打 payment；但当前实现里 `payment-service/internal/adapter/rpc/client.go` 的 `Validate()` 还是 TODO stub，实际返回 true，所以这个“二次验签”现在还没有真正落地。

12. 那这个 TODO 会带来什么影响？
- 准确回答：如果 Payment Service 只暴露在受控内网、所有流量都先经过 Gateway，风险相对可控；但从“纵深防御”角度看，它意味着 Payment 这一层对签名真实性的独立校验还没完成，答辩时必须诚实说明。

13. Payment Service 现在真的会发 RocketMQ 吗？
- 准确回答：按当前主入口 `payment-service/cmd/payment-service/main.go`，是会接真实 MQ producer 的；但仓库里还保留了一个旧版 `payment-service/handler.go`，里面有 `noopEventPublisher`。答辩要说明“当前主程序已接 MQ，旧文件是历史实现遗留”。

14. Verification Service 做的是强一致还是最终一致？
- 准确回答：是最终一致。支付成功后先返回，购买记录通过 `payment_events` 异步写入 `purchase_records`，所以短时间内存在已支付但 verify 还没查到的窗口。

15. Query Service 的月消费统计是真按月吗？
- 准确回答：不是。字段叫 `MonthlySpentMinor`，但当前 SQL 没有日期过滤，统计的是累计 PURCHASE 金额，这是一个需要修正的实现细节。

16. X verification 和 1 USDC 奖励是真实的吗？
- 准确回答：代码链路是真实存在的，`VerifyXTweet`、奖励状态查询、reward payout 都有实现；但是否真实发放成功取决于 X API key、payment service 内部接口、treasury wallet 等运行时环境。未配置时也支持 mock 文本校验。

17. 你们的 AI/Agent 评测做出来了吗？
- 准确回答：harness、trace 框架和任务文件在仓库里都有，但当前 `stablepay-openclaw-plugin/evals/traces/*.trace.json` 里能看到 OpenAI/Kimi 模型调用报错，现有 trace 结果不能拿来当“评测成功”的证据。

18. OpenClaw 插件、MCP SDK、CLI 分别到什么程度？
- 准确回答：OpenClaw 插件最成熟；MCP server 和 CLI 在代码里都已实现，而且和同一套工具注册共用底层逻辑；但它们仍是共仓演化状态，不是完全独立成熟产品。

19. 你们上云部分最确定能证明什么？
- 准确回答：最确定能证明的是 ACK 部署 YAML、Ingress 路由、服务编排、RocketMQ/MySQL/Redis 接线这些“部署资产”存在；如果要证明真实线上运行状态，还需要现场 `kubectl get pods`、`kubectl get ingress`、镜像仓库或域名访问截图。

20. 目前最诚实的“还没做完”有哪些？
- 准确回答：
  - Payment Service 内部 DID 二次验签还是 TODO
  - Query 月消费统计未加月份窗口
  - Verification 的购买 proof 字段未全部补齐
  - Topic init job 当前只核实到 `payment_events`
  - harness trace 当前不能证明评测效果
  - MCP/CLI 代码已实现，但独立产品化与文档口径仍需统一

### 8.2 当前最容易被抓住的风险点

- PPT 里把 19 条路由写成 21 条
- 把 18 个工具写成 17 个
- 把 Payment 内部 DID 二次验签说成“已完成”
- 把 Query 的累计消费说成“月消费”
- 把 topic init job 说成“已初始化全部 topic”
- 把 harness trace 说成“评测成功”
- 把 CLI/MCP 说成“完全没做”，或反过来说成“已独立正式发布”

### 8.3 最稳妥的答辩口径

- 对“代码已实现”的地方讲深、讲细、讲接口与函数名
- 对“跑通过但本线程未复核”的地方说“需要现场截图或录屏补证”
- 对“还没做完”的地方直接承认，并补一句“下一步就差哪几个具体点”

