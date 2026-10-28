# PAYATHON 2026：要求核验与 MVP 交付

核验日期：2026-10-28（Asia/Shanghai）。目标分支：`contest/payathon-2026`。

补充复核：[两场比赛赛制与QMuse要求](RULES-CHECK-2026-10-28.md)。QMuse不是已核到的必选工具；自有HTTPS是当前实施路线。[公网沙箱与回调](PUBLIC-SANDBOX.md)已在新服务器配置，用户已完成一次¥0.01沙箱付款，通知验签与服务端报告交付已核实。录屏按[演示操作顺序](PAYATHON-DEMO-SCRIPT.md)进行。

## 官方来源与确定事项

| 来源 | 核验结果 | 对交付的影响 |
| --- | --- | --- |
| [赛事官网](https://aipay.alipay.com/community/events/payathon-2026) | 报名截止 12-01 23:59:59，作品提交截止 12-09 23:59:59；个人或 2–5 人团队。每赛道最多 10 件独立作品，同件作品不能跨赛道重复投稿。 | 聚焦一件作品；赛道确定前先验证接入。 |
| 同上 | Agent Pay 探索服务发现、支付、交付；形态不限。评审关注产品、支付、商业化与反馈。 | 演示完整链路，分别标明模拟与真实证据。 |
| [详细活动细则](https://render.alipay.com/p/yuyan/180022670000190962/index.html) | 链接可打开，但本次读取未得到正文。 | 详细资格、权利条款与评分细项尚未核验；没有编造权重。 |
| [办公端商家接入](https://aipay.alipay.com/docs/agent-pay/skillpay.html) | PC 路线基于电脑网站支付；商家下单 Skill 创建订单，随后通过支付宝支付 Skill 推进用户确认。 | Go 商家支付适配采用 `alipay.trade.page.pay`，未来接官方 Skill；不能把普通收银台直接称为完整 Agent Pay 接入。 |
| [移动端文档](https://aipay.alipay.com/docs/agent-pay/mobile-agent-pay.html) | 需签约智能体代理支付；使用商家预下单与客户端 SDK；文档明确暂不支持沙箱。 | 本 MVP 不冒用移动端接口来实现 Web 沙箱。 |
| [电脑网站支付接口](https://aipay.alipay.com/docs/vibe-pay/ai-web-app-payment-qianyi/api-list/alipay-trade-page-pay.html) | `out_trade_no`、`total_amount`、`subject`、`FAST_INSTANT_TRADE_PAY` 由服务端签名。 | 金额只来自报价快照，以 CNY 分计量。 |
| [官方 Node SDK 参考](https://github.com/alipay/alipay-sdk-nodejs-all) | SDK 提供页面签名、通知验签与查询响应验签参考。 | 本仓库使用 Go RSA2 适配实现相同 OpenAPI 边界；不声称这是官方 Go SDK。 |

参赛中心需登录，本次未独立读到实际投稿字段。HTTPS 可体验地址、截图、交易演示和复现文档列为交付计划，不能把它们全部当作已经核实的强制字段。

## 当前作品切口

StablePay 采购任务助手：用户提交活动清单，助手发现合适的核验服务，固定报价，由用户明确确认服务费，再经 payment-service 验证付款，交付带事实引用的报告与回执。

供给是代码实际执行的 StablePay 自营预算核算、采购联合预审服务；没有虚构外部商户或实时商品价格。示例采购资料是合成案例。服务费预算与物资采购预算分别管理。

## P0 判断

- **本地闭环：GO。** 可运行服务发现、报价、用户确认、幂等创建 CNY 订单、模拟结算、确定性交付和下载回执。
- **支付宝沙箱交易与报告交付：已通过一次实际交易验证。** 新域名正常HTTPS可达；用户手动支付CNY 1分后，服务器记录 `alipay-rsa2-notify`、`PAID` 和 `DELIVERED`，支付与业务订单绑定一致。报告已持久化；用户端下载仍需手动保存。此项不证明正式资金交易、压力测试或完整AgentPay接入。
- **正式 Agent Pay 投稿的真实交易证据：尚不具备。** 用户目前只有个人支付宝账号，尚未验证商户、应用权限、官方支付 Skill、真实数字供给交易与履约凭证。不能宣称已接通正式支付宝支付。

沙箱应用与凭据已配置，并完成一次用户付款。下一阶段按所选赛道核验正式商户资格、产品权限和相应接入方式。签约、授权和付款确认由用户在官方流程中完成。

## 可交付证据与剩余工作

已包含启动/停止脚本、法币接口契约、测试、跨服务 HTTP 验收、可下载报告和订单事件。验证结果见 `docs/competitions/SHARED-MVP.md`。

已完成公网HTTPS部署和一次官方沙箱交易。尚需：用户端保存下载回执、材料录制、公开AI体验、真实用户反馈、详细细则复核，以及按所选赛道完成正式支付接入；选择AgentPay路线时仍需官方支付工具联调。当前没有真实付费用户、收入、正式支付成功率等商业化数据。
