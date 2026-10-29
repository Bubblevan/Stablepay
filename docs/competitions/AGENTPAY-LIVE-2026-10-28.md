# Agent Pay官方工具实际联调记录

日期：2026-10-28（Asia/Shanghai）。使用既有采购Runtime、Payment Service和商家Skill；所有采购资料为合成数据，服务费上限CNY 1分。

**10月29日补充审计：** 以下是当日历史结果，不能作为当前协议已正确实现的证明。已确认原 `session-id` 使用32位业务订单ID、摘要格式及强制钱包预检存在偏差，现已修正并通过本地隔离测试，见[参数修正与只读审计](AGENTPAY-PROTOCOL-AUDIT-2026-10-29.md)。原诊断查询传入signal别名，并非官方要求的submit查询凭证，所得UNKNOWN不能用于证明订单不存在；不再重复这种查询。没有创建第三笔订单。

## 第一笔订单：钱包开通前

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

首次提交时尚未申请/绑定钱包，没有更改为正式支付，没有把失败工具响应记成PAID。工具失败与未开通钱包均已观察到，但不能据此认定钱包未开通就是 `SIGN_VERIFY_FAILED` 的唯一原因。后续授权和第二笔订单结果如下。

## 签名与参数独立核对

在本机离线读取本次官方别名缓存，对原始收银台URL使用应用公钥核验，不输出密钥、APP_ID、签名URL或支付别名：

- 官方返回的 `paymentLink` 与后续保存/提交的原始字符串一致，SHA256一致。
- 缓存目标为支付宝沙箱网关，方法为 `alipay.trade.page.pay`，算法RSA2。
- 金额为0.01元，商户订单号与本次订单一致。
- 原始收银台请求签名能够通过本机应用公钥验证。

这排除了本次本地传参改写及收银台签名与本机公钥不匹配两类原因。它**不能证明**支付宝官方Agent工具接受该沙箱应用、公钥或运行身份，也不能替代开放平台密钥配置核对。原网页沙箱付款已成功，是另一条已有证据。

## 修正与继续条件

10月28日客户端新增 `agentpay-doctor`，只输出CLI版本和钱包就绪状态；钱包/账号原始返回只留在本机私有目录。当时 `agentpay-submit` 在领取执行记录前检查钱包；10月29日核对官方Skill后已移除此门禁，doctor仅保留独立诊断用途。官方返回结构化错误时，显示经过脱敏的错误码，避免只给笼统“不明”提示；原订单的不明记录保持，不覆盖历史。

开通官方Agent支付能力涉及真实支付宝账户授权，不属于已经同意的沙箱付款。需用户明确同意生成官方授权入口，再由用户查看官方条件、扫码并决定是否授权。即使钱包开通成功，也要继续核实官方PC链路的沙箱兼容性，不能直接改生产网关重试。

本轮用户随后明确同意“生成官方授权入口，我自行确认”。已调用官方 `apply-wallet --agent-name "StablePay Harness" --json`，取得支付宝域名 `u.alipay.cn` 的授权入口和官方生成的二维码。生成入口时仍为 `unbound`，该步骤不代表绑定成功。授权URL和二维码仅留在用户本机私有目录和当前聊天，不纳入公开材料。

### 用户授权后的状态复核

用户随后报告已完成授权。再次执行官方 `check-wallet --json`，实际返回 `code:200`、`status:bound`、`message:已开启支付宝支付功能`。钱包绑定已确认；不把这一结果当作商家收款权限、沙箱兼容性或新订单已付款的证明。

原失败提交的官方只读查询返回 `BIZ_ORDER_NOT_FOUND`、`status:UNKNOWN`、`查询服务未返回支付状态`。商家支付服务主动查单仍未取得付款确认，原订单保持 `AWAITING_PAYMENT`。本机原收银台的过期时间为2026-10-28 18:58:16（北京时间），复核时已过期。保留全部旧记录，不重放原submit、不将原订单伪造为关闭或成功。

## 第二笔订单：用户授权后仍复现签名错误

用户明确同意“新建 ¥0.01 沙箱订单”后，执行一次新订单联调。执行前已检查官方钱包 `bound`、服务器 `alipay_sandbox / live_payments_enabled=false`、StablePay自营“活动预算核算”目录和报价均为CNY 1分，报价与确认引用固定。合成采购资料沿用第一笔；新批次脚本拒绝重复运行。

| 步骤 | 第二笔实际结果（北京时间） |
| --- | --- |
| 创建前钱包检查 | CLI `alipay-bot-cli 0.4.8`，`wallet_status:bound`，`ready_for_submit:true` |
| 计划、报价、明确确认 | 成功创建新的待付款订单，没有重放旧submit |
| 官方signal | 21:01:06生成接管信息；服务端记录 `CLIENT_REPORTED_SUCCESS`，仅代表客户端工具报告成功 |
| 官方submit | 21:01:09仅调用一次；返回 `success:false`、`SIGN_VERIFY_FAILED`、`签名校验失败`，未进入用户付款确认 |
| 离线参数和签名核对 | 官方 `paymentLink` 原样保存，摘要一致；别名缓存对应本次商户订单、沙箱网关、page.pay、RSA2、0.01元；原始签名通过本机应用公钥验证 |
| 官方只读查询 | `BIZ_ORDER_NOT_FOUND`、`status:UNKNOWN`、`查询服务未返回支付状态`，未取得付款确认 |
| 商家端复核 | 业务订单读取为 `AWAITING_PAYMENT`，没有报告交付；主动查单请求返回支付服务HTTP 400，未取得已支付证据 |

服务端submit尝试继续保留 `UNKNOWN`。官方工具的拒绝码与付款事实分别记录；不把查询不到状态当作已取消，也不修改为PAID或DELIVERED。新订单没有自动重提或被另一笔订单替代。

第二笔结果证明：钱包未开通不是第一笔签名错误的唯一解释。离线验证只能证明当前URL与本机公钥匹配，无法证明开放平台登记公钥、官方工具后台验证、沙箱权限及宿主身份全部一致。重新读取[官方PC指南](https://aipay.alipay.com/docs/agent-pay/skillpay.html)，仍未找到PC工具链支持沙箱的明确说明，也未找到此CLI错误码的针对性解释。不能把移动端“不支持沙箱”的说明推断为PC端结论。

下一步先向官方核实PC沙箱支持、所需应用/产品权限及错误所处签名层，再按确认结果修正。已整理[可复制的官方排查问题](AGENTPAY-SUPPORT-REQUEST.md)，尚未代用户发送。无需为同一错误继续开新订单、重新授权钱包或切换正式资金。

本轮服务器更新为 `20261028-104227-linux-amd64`，源码 `02a4c65`（完整提交在包内manifest），发布包SHA256为 `142631cc8683164df5758255b9e4e6d651ce98ca001e6a5243afb6f463299c82`。该发布包含启动预检及signal恢复接口；之后的本机doctor/钱包预检改动无需在服务器运行官方CLI。

原始工具输出、Cookie和别名仅保存在忽略目录 `.local-run/procurement-agent/`；两批脱敏签名核对记录分别位于 `.local-run/agentpay-live/run-20261028-01/signature-inspection.json` 和 `run-20261028-02/signature-inspection.json`。第二批另保存钱包预检、计划报价、确认、新订单、官方查询及主动查单错误。公开材料不打包这些私有原始文件。

参考：[官方PC商家接入指南](https://aipay.alipay.com/docs/agent-pay/skillpay.html)、[官方商家Skill示例](https://github.com/alipay/ai/blob/main/code_example/page-pay-demo-with-agentpay/SKILL/SKILL.md)。当前没有从官方文档核到 `SIGN_VERIFY_FAILED` 针对此CLI场景的确切原因，不用推测代替结论。
