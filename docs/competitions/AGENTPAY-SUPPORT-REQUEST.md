# Agent Pay PC沙箱联调问题（待开发者向官方咨询）

本文件是可复制的排查描述，尚未代用户发送。更新日期：2026-10-29。咨询对象为支付宝Agent Pay PC接入技术支持或比赛官方技术答疑。[官方商户指南](https://aipay.alipay.com/merchant-guide)列出研发者钉钉技术支持群及邮件 `alipay.ai@service.alipay.com`。

## 可直接复制的问题

支付宝Agent Pay技术支持团队：

您好！我们正在参加PAYATHON 2026，采用已有StablePay Harness、采购Runtime及Payment Service接入官方PC Agent Pay。当前仅使用支付宝沙箱，未开启正式资金交易。希望协助确认 `SIGN_VERIFY_FAILED` 的具体签名层及沙箱支持条件。

环境及现象：

- Windows，Agent宿主为Codex；通过官方 `@alipay/agent-payment@1.0.28 install-cli --json` 安装，CLI版本 `alipay-bot-cli 0.4.8`。
- 商家下单Skill调用既有后端创建固定报价订单。服务为StablePay自营“活动预算核算”，沙箱服务费0.01元。
- 钱包已由用户在支付宝官方入口授权，`check-wallet --json` 返回 `code:200 / status:bound`。提交前再次检查为bound。
- 商家提供GET形式的 `alipay.trade.page.pay` 签名URL，网关为 `https://openapi-sandbox.dl.alipaydev.com/gateway.do`，算法RSA2。
- 按[PC商家指南](https://aipay.alipay.com/docs/agent-pay/skillpay.html)调用 `trigger-payment-signal` 成功返回 `paymentLink`，随后原样传给 `submit-payment`，没有打开、重新编码或改写支付别名。
- 2026-10-28 21:01:09（北京时间）新订单首次submit返回 `success:false / errorCode:SIGN_VERIFY_FAILED / errorMsg:签名校验失败`，没有进入用户付款确认。未重复提交。
- 原诊断曾用signal返回的别名调用 `query-payment-status`，得到 `BIZ_ORDER_NOT_FOUND / status:UNKNOWN`。现已对照官方Skill确认，这不能代替submit输出的查询凭证，因此不把该结果作为支付宝订单不存在的证明。商家端未取得已支付证据，没有交付。
- 已离线核对本次别名缓存对应正确订单、0.01元和沙箱网关；原始URL签名能通过本机应用公钥验证。这不能替代开放平台及官方工具后台的公钥核对。
- 同一商家沙箱应用此前已完成一次普通网页收银台付款、RSA2通知验签及报告交付；该记录不作为Agent Pay成功证据。
- 目前安装了官方CLI，并提供自有商家Skill；没有把官方完整Skill安装到宿主。请协助确认其余安装组件或身份配置是否为此路径的必要条件。

排查中已确认并修正的本地偏差：

- 两次历史测试使用32位StablePay业务订单ID作为 `--session-id`，没有使用真实Agent会话UUID。这违反当前[官方cashier-payment.md](https://github.com/alipay/payment-skills/blob/main/alipay-payment-skill/references/cashier-payment.md)的要求；我们此前遗漏了此约定。
- 本机现已确认存在真实Codex会话UUID，改为优先使用宿主 `AIPAY_SESSION_ID`，否则使用当前 `CODEX_THREAD_ID / CODEX_SESSION_ID`，prepare和submit绑定同一会话。没有可靠来源即停止，旧记录不迁移或重提。
- `intent-summary` 已改为“服务内容：xxx，支付金额：¥xx，支付对象：xxx”，从本次确认报价重新提取。
- 已移除submit前强制 `check-wallet`，钱包处理交给官方提交命令；独立doctor不再是支付前置门禁。
- 12项参数及替身测试已通过，未创建第三笔订单或提交修正后的真实支付。不能认定这些偏差一定导致原错误，也尚未证明修复后能通过。
- 只读比较服务器实际运行环境与本机配置，APP_ID、SELLER_ID、私钥推导公钥指纹及支付宝公钥指纹均一致。随后直接在用户已登录的官方沙箱控制台“系统默认密钥 → 公钥模式 → 查看”中独立计算应用公钥SPKI SHA-256，与本机和运行中服务器一致；控制台AppID、商户PID也匹配。未读取私钥字段、修改密钥或切换模式。
- 两笔原始错误JSON中均未见request ID或trace ID字段。现有CLI仍为0.4.8。

请帮助确认：

1. CLI 0.4.8的PC `trigger-payment-signal → submit-payment` 是否支持上述沙箱网关和沙箱应用？如果支持，需要哪些官方明确支持的环境参数、测试钱包或配置？
2. 正式支付宝Agent钱包的bound身份是否能与沙箱商户、沙箱买家配合？是否需要另一个测试钱包、专门环境、商户产品权限或白名单？如果个人开发者不能满足正式商户条件，应使用哪种官方测试方式？
3. `SIGN_VERIFY_FAILED` 由哪个服务、哪一步验证返回？指商家URL、公钥登记、支付短链、宿主身份/会话，还是CLI到后端请求的签名？请协助区分，避免只按商家密钥错误处理。
4. 非UUID或非真实宿主的 `--session-id` 是否可能触发此错误码？Windows/Codex是否支持现有宿主UUID来源，是否还需显式framework参数或其他初始化？
5. 只安装 `install-cli` 与完整 `install` 在请求签名、设备身份及Agent注册方面有何差别？当前Windows/Codex组合是否受支持，是否需要升级CLI或安装特定组件？
6. 如何获取可以提供给官方的脱敏request ID、trace ID或安全诊断？是否可按上述时间定位？

请先确认接入和测试条件，我们会据此修正；不会用反复新建订单的方式重试，也不会自动改成真实资金支付。

感谢协助！

## 提供附加材料时

先提供CLI版本、上述时间、错误码和必要的脱敏命令结构。APP_ID、商户订单号等如确需定位，由开发者通过官方私密工单单独提供。不要公开上传密钥、钱包凭据、Cookie、完整签名收银台URL、原始paymentLink或本机私有诊断目录。

完整工程记录：[两批官方联调结果](AGENTPAY-LIVE-2026-10-28.md)、[10月29日协议修正与只读审计](AGENTPAY-PROTOCOL-AUDIT-2026-10-29.md)。用户已明确选择自行联系官方，本轮不发送邮件或CLI反馈。若之后决定通过官方 `problem-feedback` 提交，按[官方反馈流程](https://github.com/alipay/payment-skills/blob/main/alipay-payment-skill/references/feedback.md)先确认本次反馈内容。
