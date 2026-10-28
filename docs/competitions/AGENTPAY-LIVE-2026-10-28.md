# Agent Pay官方工具实际联调记录

日期：2026-10-28（Asia/Shanghai）。使用既有采购Runtime、Payment Service和商家Skill；所有采购资料为合成数据，服务费上限CNY 1分。

## 实际结果

| 步骤 | 观测 |
| --- | --- |
| 环境与目录 | 公网 `alipay_sandbox`，真实资金关闭；发现StablePay自营“活动预算核算”，报价¥0.01 |
| 计划、报价、用户确认与订单 | CLI成功创建一笔待付款订单；不是复用之前网页付款的交付结果 |
| 首次官方工具启动 | Node原生子进程在受限沙箱内报 `spawn EPERM`，首次signal记录为UNKNOWN，未提交付款 |
| 启动检查修正 | 增加CLI版本预检、私有错误记录；沙箱外原生进程启动正常 |
| 同单signal恢复 | 原不明记录保留，显式恢复非付款信号步骤；官方 `trigger-payment-signal` 返回有效 `paymentLink`，接管信息生成成功 |
| 官方提交支付 | `alipay-bot submit-payment` 0.4.8真实返回 `success:false`、`SIGN_VERIFY_FAILED`、`签名校验失败`；只调用一次，没有自动重放 |
| 钱包状态只读检查 | 官方 `check-wallet --json` 返回 `status:unbound`、`neverCreated:true`、`message:未开通`；官方提示需开启支付宝支付能力 |
| 业务订单 | 仍为 `AWAITING_PAYMENT`，没有新报告交付；主动查单未得到付款确认 |

没有申请/绑定钱包，没有更改为正式支付，没有把失败工具响应记成PAID。工具失败与未开通钱包均已观察到，但不能据此认定钱包未开通就是 `SIGN_VERIFY_FAILED` 的唯一原因。

## 签名与参数独立核对

在本机离线读取本次官方别名缓存，对原始收银台URL使用应用公钥核验，不输出密钥、APP_ID、签名URL或支付别名：

- 官方返回的 `paymentLink` 与后续保存/提交的原始字符串一致，SHA256一致。
- 缓存目标为支付宝沙箱网关，方法为 `alipay.trade.page.pay`，算法RSA2。
- 金额为0.01元，商户订单号与本次订单一致。
- 原始收银台请求签名能够通过本机应用公钥验证。

这排除了本次本地传参改写及收银台签名与本机公钥不匹配两类原因。它**不能证明**支付宝官方Agent工具接受该沙箱应用、公钥或运行身份，也不能替代开放平台密钥配置核对。原网页沙箱付款已成功，是另一条已有证据。

## 修正与继续条件

客户端新增 `agentpay-doctor`，只输出CLI版本和钱包就绪状态；钱包/账号原始返回只留在本机私有目录。`agentpay-submit` 在领取执行记录前检查钱包。官方返回结构化错误时，显示经过脱敏的错误码，避免只给笼统“不明”提示；原订单的不明记录保持，不覆盖历史。

开通官方Agent支付能力涉及真实支付宝账户授权，不属于已经同意的沙箱付款。需用户明确同意生成官方授权入口，再由用户查看官方条件、扫码并决定是否授权。即使钱包开通成功，也要继续核实官方PC链路的沙箱兼容性，不能直接改生产网关重试。

本轮用户随后明确同意“生成官方授权入口，我自行确认”。已调用官方 `apply-wallet --agent-name "StablePay Harness" --json`，取得支付宝域名 `u.alipay.cn` 的授权入口和官方生成的二维码。当前仍为 `unbound`，等待用户扫码确认；生成入口不代表绑定成功。授权URL和二维码仅留在用户本机私有目录和当前聊天，不纳入公开材料。

本轮服务器更新为 `20261028-104227-linux-amd64`，源码 `02a4c65`（完整提交在包内manifest），发布包SHA256为 `142631cc8683164df5758255b9e4e6d651ce98ca001e6a5243afb6f463299c82`。该发布包含启动预检及signal恢复接口；之后的本机doctor/钱包预检改动无需在服务器运行官方CLI。

原始工具输出、Cookie和别名仅保存在忽略目录 `.local-run/procurement-agent/`；脱敏签名核对记录为 `.local-run/agentpay-live/run-20261028-01/signature-inspection.json`。公开材料不打包这些私有原始文件。

参考：[官方PC商家接入指南](https://aipay.alipay.com/docs/agent-pay/skillpay.html)、[官方商家Skill示例](https://github.com/alipay/ai/blob/main/code_example/page-pay-demo-with-agentpay/SKILL/SKILL.md)。当前没有从官方文档核到 `SIGN_VERIFY_FAILED` 针对此CLI场景的确切原因，不用推测代替结论。
