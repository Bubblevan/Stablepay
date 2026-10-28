# Agent Pay：扩展现有 Harness

日期：2026-10-28。目标为Agent Pay，沿用StablePay现有服务和仓库，不新增Agent运行时、钱包服务或部署进程。

## 官方依据

已重新读取[官方PC商家接入指南](https://aipay.alipay.com/docs/agent-pay/skillpay.html)和[支付宝商家Skill示例](https://github.com/alipay/ai/blob/main/code_example/page-pay-demo-with-agentpay/SKILL/SKILL.md)：商家提供下单Skill，调用GET形式的 `alipay.trade.page.pay`，把收银台交给 `trigger-payment-signal`，取返回JSON中的完整 `paymentLink`，再交给支付宝支付处理流程。指南给出的推进命令为 `submit-payment`。生成短链与提交工具都不是商家已收款的证据。

本机通过官方 `@alipay/agent-payment@1.0.28 install-cli --json` 安装 `alipay-bot` **0.4.8**，实际读取 `--version` 和两个命令的 `--help`，确认支持 `--session-id`、`--json` 及文档中的参数。安装CLI不等于已完成Agent宿主识别、钱包授权、风控组件或沙箱兼容性联调。

## 复用位置

| 现有部分 | 本轮扩展 |
| --- | --- |
| Payment Service `AgentPaymentHarness` | 把原金额上限、商户名单、自动批准阈值策略提取到公开的轻量包 `pkg/agentharness`，USDC/USDT Harness和CNY服务共同调用；原Redis一次性消费及DID验签仍由原Harness执行 |
| Commerce Runtime `PaymentAdapter` | 增加 `AlipayPaymentAdapter` 及按币种路由，CNY进入既有fiat HTTP边界，USDC/USDT进入既有Kitex适配器 |
| Runtime预算、确认、执行与账本 | CNY强制启用现有精确报价买家确认；继续使用 `ReservePaymentIntent`、`RuntimeGuard.CheckPayment`、`AuthorizeAndSubmitPayment`、`ReconcilePayment` 和finance事务 |
| Runtime履约 | 支付服务的已验签订单可证明访问资格；实际交付仍经过原商户调用与validator，付款不自动把Episode改成FULFILLED |
| 现有采购MVP订单 | 增加支付工具调用记录，仍使用原计划、报价、用户确认、支付服务和报告持久化；没有第二套支付真相来源 |
| 现有Agent客户端 | 增加 `agentpay-prepare` 与 `agentpay-submit`，本机调用官方CLI，不在服务器执行外部命令 |
| 商家下单Skill | `showmethemoney-skill/skills/stablepay-procurement/SKILL.md`，串联已有API与官方工具 |

CNY最小单位为分，USDC/USDT沿用原精度。共享策略接收各自已经归一化的整数，绝不进行跨币种单位换算。采购MVP使用浏览器/CLI会话里的明确报价确认；完整Runtime仍使用原DID买家签名，两种确认方式不混称。

## 两个接入入口

### 1. 现有采购MVP：本轮可用于工具联调的商家入口

保持同一CLI会话执行：

```text
node scripts/procurement-agent.mjs environment
node scripts/procurement-agent.mjs catalog
node scripts/procurement-agent.mjs plan --file task.json
node scripts/procurement-agent.mjs quote --file quote-request.json
node scripts/procurement-agent.mjs approve --file approval.json --confirm --idempotency-key <stable-key>
node scripts/procurement-agent.mjs agentpay-prepare --purchase-id <id>
node scripts/procurement-agent.mjs agentpay-submit --purchase-id <id> --confirm
node scripts/procurement-agent.mjs reconcile --purchase-id <id>
node scripts/procurement-agent.mjs receipt --purchase-id <id>
```

`task.json` 沿用原 `plan` 请求：`prompt`、`service_budget` 和采购 `input`。`quote-request.json` 包含上一步返回的 `plan_id` 和目录中的 `product_id`。`approval.json` 包含本次报价的 `quote_id`、`quote_hash`、`accepted:true`。用户没有逐笔确认商品、商户、金额前，不能替用户执行 `approve` 或补 `--confirm`。

默认服务器是本机8093。每条命令追加 `--base https://bubblevan.de5.net --allow-remote` 才调用公网。采购工具接口首次发布于 `20261028-092856-linux-amd64`；当前发布为 `20261028-104227-linux-amd64`，源码 `02a4c65`，增加启动预检与非付款signal恢复。完整Runtime生产composition路径尚未在该小服务器启用。CLI有自己的会话，不能直接拿网页里的订单ID当作CLI会话订单。

客户端先用官方CLI的 `--version` 检查子进程能否启动，再由服务端记录 `STARTED` 后调用工具。`submit`每单只有一次调用机会，超时或进程退出后保留不明状态，不能自动重放。只有尚无任何submit记录、上次signal为UNKNOWN时，操作人排查后可显式执行 `agentpay-prepare --recover-signal` 恢复非付款的链接/安全信号步骤；旧记录保留，总工具尝试数受限。这不允许再次提交支付。客户端只保存官方 `paymentLink` 原始值，并在同一 `purchase_id` 作为官方 `session-id` 的隔离范围内传递；不向用户展示、不打开、不改写。

工具JSON、支付别名和Cookie只放 `.local-run/procurement-agent/`。服务端保存阶段、时间与SHA256摘要，不保存原始工具输出；客户端成功报告显式标为 `CLIENT_REPORTED_SUCCESS`，不具有付款证明效力。只有Payment Service的RSA2通知或验签查询可以使原订单进入PAID并交付。

### 2. 完整Commerce Runtime：接入原交易Harness

原生产composition可选开启：

```text
COMMERCE_RUNTIME_ALIPAY_ENABLED=1
FIAT_PAYMENT_URL=http://127.0.0.1:8092
FIAT_INTERNAL_TOKEN=<与payment-service一致，至少24字符>
COMMERCE_RUNTIME_ALIPAY_MERCHANT_DID=<目录里的确切商户DID>
COMMERCE_RUNTIME_ALIPAY_PAYEE_DID=<该商户收款主体引用>
COMMERCE_RUNTIME_ALIPAY_MERCHANT_NAME=<实际商户名称>
```

这复用原Runtime进程和既有MySQL composition，不是新的精简服务；原composition所需的DID、Kitex、结算和目录配置仍按原方式提供。CNY目录/报价必须与配置的商户、币种和结算环境一致。现有HTTP402报价解析器增加CNY整数分，按币种固定网络 `alipay:sandbox`、资产标记 `CNY`；这只是StablePay内部报价传输扩展，不宣称支付宝官方x402或ACT认证。不会把原USDC目录或链上资产自动改成CNY，交付时也不为CNY订单生成Solana付款签名。

在既有创建Episode、发现/选品、报价、买家签名、预算保留和提交动作之后，原 `POST /v1/episodes/{episode_id}/payment-handoff` 返回已经创建的CNY订单及官方工具所需参数。它受原API鉴权保护，只对当前待付款意图开放，不自动确认买家、不生成新意图、不作为公开匿名接口。工具由Agent宿主调用；之后原Runner继续查询支付并推进履约。

Payment Service增加仅内部可见的 `GET /v1/fiat/requests/{request_id}`，用于恢复“订单创建成功但响应丢失”的窗口。适配器固定使用 `runtime:<intent_id>` 幂等标识，并校验商户、报价、确认引用、币种、金额和订单环境。支付宝订单号存入Runtime的提供方交易标识TxID，支付宝交易号不伪装为链上TxHash。

## 公开演示的容量保护

- 采购API进程：每分钟最多120次写请求，每个会话最多30次；这是单进程保护，不宣称分布式限流。
- 采购快照最多8MiB、计划1000份、报价2000份、订单500份；达到容量拒绝新增，维护者须先归档。
- 未关联订单的报价过期24小时后、无引用计划创建24小时后，在后续事务中清理；已付款和待付款订单、报告、确认与工具记录保留。
- 支付演示文件存储最多4MiB、订单1000份。正式规模继续使用既有数据库实现并按业务归档；本轮不在小服务器新增数据库。

## 当前验收边界与下一步

已有证据：网页沙箱¥0.01付款、RSA2回调、PAID及DELIVERED；官方CLI安装与帮助信息。之后已实际调用官方工具：signal成功，submit返回 `SIGN_VERIFY_FAILED`；首次钱包检查为 `unbound`，用户完成官方授权后已实际查到 `bound / code:200`。原失败请求查询为 `BIZ_ORDER_NOT_FOUND / UNKNOWN`，未获付款确认；本机公钥可验证原始收银台签名，支付别名传递一致。见[实际联调记录](AGENTPAY-LIVE-2026-10-28.md)。没有将工具失败或钱包绑定写成付款成功。

新版本已部署并通过健康检查，原有已付款订单和报告仍在，发布文件与manifest摘要一致。钱包绑定后，已在用户明确确认下创建第二笔¥0.01沙箱订单：signal成功，submit仍返回 `SIGN_VERIFY_FAILED`，没有进入用户付款确认或报告交付。新订单的原始URL也通过本机公钥验证，别名原样传递；官方只读查询仍为UNKNOWN。**官方工具对当前Agent宿主与沙箱的支持尚未证明。** 下一步按[官方排查问题](AGENTPAY-SUPPORT-REQUEST.md)核实支持条件和签名层；不能通过反复新订单规避错误，也不能自动切换正式资金。本轮未新增或执行测试套件；Go编译、Node语法检查与部署健康检查不替代支付、恢复或回归验收。

完整Runtime路径还需要实际CNY商户目录、报价/履约协议和买家签名联调，不能把适配器已编译写成完整Harness链路已通过。公网真实LLM、校园原文抽取与引用部署、真实试用、正式收款资格和投稿材料继续按实际结果完成。QMuse不是本轮工程前置条件。
