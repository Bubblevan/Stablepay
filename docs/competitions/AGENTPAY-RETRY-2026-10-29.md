# 参数修正后的单笔沙箱复核

日期：2026-10-29（Asia/Shanghai）。在完成[参数修正与控制台公钥核对](AGENTPAY-PROTOCOL-AUDIT-2026-10-29.md)后，用户明确要求“拿到之后重试支付看看”，因此新建一笔相同自营服务、CNY 1分的沙箱订单。不是重放旧订单，也不是自动循环重试。

## 本次条件与实际结果

| 项目 | 观测 |
| --- | --- |
| 客户端源码 | `509be11`，CLI `alipay-bot-cli 0.4.8` |
| 服务与环境 | StablePay自营“活动预算核算”；报价CNY 1分；`alipay_sandbox / live_payments_enabled=false` |
| 用户授权 | 针对同一商户、服务、金额的新一次沙箱尝试；本批次目录已存在即拒绝重复运行 |
| 官方会话 | 真实宿主 `CODEX_THREAD_ID`，合法UUID；prepare保存值与submit时当前会话一致；没有用业务订单ID替代 |
| 意图摘要 | 从确认报价生成“服务内容：活动预算核算，支付金额：¥0.01，支付对象：StablePay 自营工具” |
| 钱包调用顺序 | 未执行check-wallet，官方submit自行处理钱包状态 |
| signal | 成功生成paymentLink；原样保存与传递；服务端记录CLIENT_REPORTED_SUCCESS |
| submit | 14:27:08仅调用一次，实际返回 `success:false / SIGN_VERIFY_FAILED / 签名校验失败` |
| 商家业务状态 | 随后读取仍为 `AWAITING_PAYMENT`，未生成交付报告；submit尝试保留UNKNOWN |

没有使用signal别名补做官方状态查询，没有更换密钥、重新开通钱包、切换生产、升级CLI、安装Skill或自动新建第四笔订单。原两笔失败记录和已有网页沙箱付款交付记录保留。

这次复核表明：在修正会话ID、摘要格式、钱包预检，并确认控制台应用公钥与实际私钥匹配后，CLI 0.4.8仍返回同一错误。因此不能只用这些已知偏差解释失败，也仍不能据此断言PC沙箱不受支持。具体后台验签层、沙箱与钱包组合权限、宿主完整初始化仍待官方确认。

原始工具JSON、会话、Cookie及支付别名留在本机忽略目录，不公开打包。批次记录为 `.local-run/agentpay-live/run-20261029-01/`，实际工具输出仍在原 `.local-run/procurement-agent/` 目录。当前用户选择自行联系官方，未代发邮件或问题反馈；[咨询稿](AGENTPAY-SUPPORT-REQUEST.md)已补充本次结果。
