---
name: stablepay-procurement
description: 通过StablePay已有采购Runtime发现服务、获取报价、由用户确认订单，再使用支付宝官方PC Agent Pay工具接管支付，取得服务器验签后的采购报告。用于用户明确要求购买采购核验服务的场景。
---

# StablePay采购服务：商家下单Skill

复用仓库根目录 `scripts/procurement-agent.mjs`，不创建独立Agent、钱包、收银台或报告服务。脚本路径相对StablePay仓库根目录；在其他工作目录使用绝对路径。

## 服务发现与人类确认

1. `node scripts/procurement-agent.mjs environment` 查看环境；Agent Pay工具适配当前只允许 `alipay_sandbox`。
2. `catalog` 查看真实自营服务。使用用户提供的需求和清单生成任务JSON，再通过 `plan --file <task.json>`、`quote --file <quote-request.json>` 获取报价。不能编造资料、价格、审批或商户。
3. 将商品、商户、CNY服务费、沙箱环境和报告范围告知用户。没有针对本次报价的明确确认，不能调用 `approve` 或支付命令。
4. 将服务器返回的 `quote_id`、`quote_hash` 与 `accepted:true` 写入本次确认JSON，然后 `approve --file <approval.json> --confirm --idempotency-key <stable-key>`。不能复用其他报价的确认；重复调用保持同一个幂等键。

远端调用在每条命令后带 `--base https://bubblevan.de5.net --allow-remote`。默认使用本机8093；保持相同origin和脚本会话，网页订单的Cookie不会自动复制给CLI。完整示例见 `docs/competitions/AGENTPAY-HARNESS.md`。

## 官方PC支付接管

在用户的Agent宿主上安装支付宝官方CLI，服务器不安装钱包工具。脚本优先使用 `ALIPAY_BOT_PATH` 指定的绝对路径，Windows也支持官方安装器默认路径。

会话ID优先读取宿主提供的 `AIPAY_SESSION_ID`，否则读取当前Codex的 `CODEX_THREAD_ID` / `CODEX_SESSION_ID`。必须是真实当前会话的UUID；缺失或无效即停止，不能使用采购订单ID、随机UUID或时间戳替代。prepare保存该会话绑定，submit要求同一会话；旧版没有绑定的记录不迁移、不重放。

支付路径不执行 `check-wallet`，由官方 `submit-payment` 自行处理钱包状态。`agentpay-doctor` 仅供用户明确请求的独立诊断，不是支付前置步骤，其返回值不作为支付门禁。生成授权入口或开通真实账户能力仍需用户明确授权。若原生工具在系统沙箱内无法启动或创建BlueShield目录，按执行环境要求申请沙箱外运行，不能绕过系统批准。

```text
node scripts/procurement-agent.mjs agentpay-prepare --purchase-id <purchase_id>
node scripts/procurement-agent.mjs agentpay-submit --purchase-id <purchase_id> --confirm
```

第一条从现有订单读取固定报价、确认引用和签名收银台，由共享Harness策略检查预算与商户，然后调用官方 `trigger-payment-signal --payment-link ... --merchant-info ... --amount ... --session-id ... --json`。

第二条从当前已确认报价重新提取摘要，格式为 `服务内容：活动预算核算，支付金额：¥0.01，支付对象：StablePay 自营工具`，把官方返回的完整原始 `paymentLink` 交给 `submit-payment --payment-link ... --intent-summary ... --session-id ... --json`，由支付宝引导用户付款。业务订单、幂等键和商户订单号与Agent会话分别保存。`--confirm` 必须来自用户对该笔交易的授权，不能由模型为完成任务自行补上。

`paymentLink` 是不透明的工具参数；不显示、不打开、不截断、不改写，也不放入模型上下文或对话。脚本将其和工具原始输出保存在忽略的本机私有目录，服务端只记工具阶段与摘要。工具报告不是支付宝付款证据。

官方CLI已安装不代表Agent宿主、风控组件和钱包已开通，也不证明该流程支持沙箱。若宿主、登录或沙箱兼容性未通过，停止并说明实际错误，不改用生产链接、不替用户开通钱包，不把网页手动沙箱交易当作Agent Pay验收。

## 付款后交付

调用 `reconcile --purchase-id <purchase_id>`，只在服务器状态为 `DELIVERED` 且存在报告时调用 `receipt` 并交付服务器返回的报告。不能从工具成功、短链生成、浏览器跳转、用户一句“付了”推断付款成功。

如果工具超时、网络中断或返回不认识的JSON，服务器保留本次尝试；禁止自动再次 `submit-payment`。先查询原订单、核对官方支付状态并由用户处理。不要自动新建替代订单绕过一次性执行记录。

官方查询只能使用本次submit输出的有效查询凭证（`outShakeNo` 或 `shortUrl` / 支付确认链接），不能拿signal的支付别名、旧链接或独立钱包授权入口代替。失败提交没有生成此类凭证时，保留商家订单状态和错误记录，不盲查支付别名。

预算核算服务只解释金额，联合预审才检查其规则覆盖的规格与证据条件；均不能承诺报销资格或材料真实性。不得把合成演示资料或沙箱款项记为真实用户效果和营收。
