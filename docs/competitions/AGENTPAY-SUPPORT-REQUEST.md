# Agent Pay PC沙箱联调问题（待开发者向官方咨询）

本文件是可复制的排查描述，尚未代用户发送。日期：2026-10-28。咨询对象为支付宝Agent Pay PC接入技术支持或比赛官方技术答疑。

## 可直接复制的问题

我们正在参加PAYATHON，采用已有StablePay Harness、采购Runtime及Payment Service接入官方PC Agent Pay流程。当前仅使用支付宝沙箱，未开启正式资金交易。

环境及现象：

- Windows，Agent宿主为Codex；通过官方 `@alipay/agent-payment@1.0.28 install-cli --json` 安装，CLI版本 `alipay-bot-cli 0.4.8`。
- 商家下单Skill调用既有后端创建固定报价订单。服务为StablePay自营“活动预算核算”，沙箱服务费0.01元。
- 钱包已由用户在支付宝官方入口授权，`check-wallet --json` 返回 `code:200 / status:bound`。提交前再次检查为bound。
- 商家提供GET形式的 `alipay.trade.page.pay` 签名URL，网关为 `https://openapi-sandbox.dl.alipaydev.com/gateway.do`，算法RSA2。
- 按[PC商家指南](https://aipay.alipay.com/docs/agent-pay/skillpay.html)调用 `trigger-payment-signal` 成功返回 `paymentLink`，随后原样传给 `submit-payment`。两步使用同一订单范围的 `session-id`，没有打开、重新编码或改写支付别名。
- 2026-10-28 21:01:09（北京时间）新订单首次submit返回 `success:false / errorCode:SIGN_VERIFY_FAILED / errorMsg:签名校验失败`，没有进入用户付款确认。未重复提交。
- 官方 `query-payment-status` 返回 `BIZ_ORDER_NOT_FOUND / status:UNKNOWN / 查询服务未返回支付状态`。商家端未取得已支付证据，没有交付。
- 已离线核对本次别名缓存对应正确订单、0.01元和沙箱网关；原始URL签名能通过本机应用公钥验证。这不能替代开放平台及官方工具后台的公钥核对。
- 同一商家沙箱应用此前已完成一次普通网页收银台付款、RSA2通知验签及报告交付；该记录不作为Agent Pay成功证据。
- 目前安装了官方CLI，并提供自有商家Skill；没有把官方完整Skill安装到宿主。请协助确认其余安装组件或身份配置是否为此路径的必要条件。

请帮助确认：

1. PC路线的 `trigger-payment-signal → submit-payment` 是否支持上述沙箱网关和沙箱应用？如果支持，需要哪些官方明确支持的环境参数、测试钱包或配置？普通已绑定的钱包与沙箱买家应如何配合？
2. 若仅支持生产应用，是否需要已签约电脑网站支付的正式商家/应用、额外Agent Pay权限或白名单？个人开发者应使用哪种官方测试方式？
3. `SIGN_VERIFY_FAILED` 在CLI 0.4.8中指哪一层签名：商家URL、应用登记公钥、宿主身份，还是钱包/官方请求签名？如何取得可供官方定位的脱敏request ID或诊断输出？
4. 官方完整Skill安装与只安装CLI相比，是否会影响这两个命令所需的身份或签名配置？Codex宿主是否有需要单独完成的配置？

请先确认接入和测试条件，我们会据此修正；不会用反复新建订单的方式重试，也不会自动改成真实资金支付。

## 提供附加材料时

先提供CLI版本、上述时间、错误码和必要的脱敏命令结构。APP_ID、商户订单号等如确需定位，由开发者通过官方私密工单单独提供。不要公开上传密钥、钱包凭据、Cookie、完整签名收银台URL、原始paymentLink或本机私有诊断目录。

完整工程记录：[两批官方联调结果](AGENTPAY-LIVE-2026-10-28.md)。
