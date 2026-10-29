# Agent Pay参数修正与只读审计

日期：2026-10-29（Asia/Shanghai）。本篇审计阶段范围为本地协议修正、隔离测试和服务器配置只读核对，当时没有创建订单、signal、submit、重新授权或切换生产支付。10月28日两笔失败记录保持原样。审计完成后用户另行授权一次同额新单尝试，见文末补充。

## 已核实并修正的本地偏差

依据[官方收银台流程](https://github.com/alipay/payment-skills/blob/main/alipay-payment-skill/references/cashier-payment.md)及[官方支付处理Skill](https://github.com/alipay/payment-skills/blob/main/alipay-payment-skill/SKILL.md)：

| 项目 | 原实现 | 10月29日修正 |
| --- | --- | --- |
| 官方会话 | 用32位业务purchase_id代替 | 优先宿主AIPAY_SESSION_ID，否则当前Codex的CODEX_THREAD_ID / CODEX_SESSION_ID；必须为真实UUID，不随机生成、不用订单号兜底 |
| 会话绑定 | 按业务订单隔离，未保存真实会话 | prepare保存真实会话及来源，submit要求会话一致；旧记录缺少绑定则阻止，不自动改写或重提 |
| intent-summary | 将商户、商品、金额自然语言拼接 | submit从当前已确认报价重新提取，固定为“服务内容：xxx，支付金额：¥xx，支付对象：xxx” |
| 钱包预检 | 提交前强制check-wallet | 从支付路径移除；官方submit自行处理钱包状态；doctor仅供独立诊断，不是官方支付前置要求 |
| 查询凭证 | 诊断脚本拿signal别名做查询 | 官方要求本次submit输出的outShakeNo、shortUrl或支付确认链接；失败submit无有效凭证，不拿signal别名补查 |

当前运行环境实际提供 `CODEX_THREAD_ID`，为合法UUID，与 `CODEX_SESSION_ID` 一致。仅验证来源和格式，不公开会话原值。`purchase_id` 继续用于StablePay订单、幂等和审计；`out_trade_no` 继续由Payment Service管理；`paymentLink` 原样保存。

这些是协议偏差的修正，不证明其中任何一项就是 `SIGN_VERIFY_FAILED` 的根因，也不证明修复后官方沙箱付款已经成功。

## 本地测试

```powershell
node --test --test-isolation=none scripts/lib/alipay-agentpay.test.mjs
```

12项测试全部通过。包括来源优先级、无效/缺失UUID、官方摘要格式、整数分与币种、链接原样传递、会话变更、旧记录拒绝、报价/确认绑定、无钱包预检、无确认不提交、一次性提交及错误保留UNKNOWN。全部HTTP及原生命令使用替身，临时文件为独立合成夹具，不读取钱包或实际订单，不接触支付网络。

## 服务器实际配置与密钥指纹

经严格主机校验的SSH只读检查服务MainPID及 `/proc/<pid>/environ`，再从运行中配置指向的应用私钥推导公钥，按DER/SPKI的SHA-256计算指纹；不输出私钥、APP_ID或SELLER_ID。

| 检查 | 结果 |
| --- | --- |
| 实际运行模式 | alipay_sandbox |
| 运行中标识/密钥路径与fiat.env | 一致 |
| 运行中APP_ID、SELLER_ID与本机.env | 均一致 |
| 服务器私钥推导的应用公钥与本机原始私钥推导值 | 一致 |
| 本机已准备的app-public.pem | 与原始私钥推导值一致 |
| 服务器支付宝公钥与本机原始支付宝公钥 | 一致 |
| 支付宝控制台显示的应用公钥 | 已在用户已登录的沙箱页“系统默认密钥 → 公钥模式 → 查看”中独立核对，与本机及服务器推导值一致 |
| 当前控制台APP_ID、商户PID与本机配置 | 均一致；只输出布尔匹配结果 |

应用公钥SPKI SHA-256：`03aa44e62f4bfa9bf7bd63dc369229dae0db580a4d420e454febadd4ff02a548`。

支付宝公钥SPKI SHA-256：`0520383c03666f9ec7224b8aa01e734b613f807ac9a176ac97f9bf623a836451`。

### 控制台入口与独立复核

用户指出找不到字段后，直接只读检查其已登录的 `https://open.alipay.com/develop/sandbox/app`：网页/移动应用 → 开发信息 → 接口加签方式（当前为“系统默认密钥”）→ 公钥模式右侧“查看”。“查看密钥”弹窗内确有“应用公钥”，同窗还显示“应用私钥”和“支付宝公钥”。

仅定位“应用公钥”对应节点，在浏览器内用WebCrypto导入为RSA SPKI并计算SHA-256；未读取私钥字段、未复制到剪贴板、未截取包含密钥的画面。公钥为2048位，与上述应用指纹一致。同时在浏览器内对页面已显示的数字标识做摘要，与本机AppID及SellerID摘要比较，均匹配。没有切换系统默认/自定义模式、生成新密钥或点击保存。

因此无需用户再手动导出文件；本次已取得独立控制台证据。它确认了当前控制台显示的应用公钥与服务器私钥匹配，仍不能证明Agent Pay后台使用同一沙箱验签环境。脱敏复核结果在 `.local-run/agentpay-live/audit-20261029/console-public-key-comparison.json`，先前审计文件中的NOT_AVAILABLE是打开控制台前的历史状态，以本节后续结果为准。

## 官方安装与诊断信息

- 实际CLI仍为 `alipay-bot-cli 0.4.8`。只读检查Codex、共享agents及AutoClaw常用skills目录，未找到 `alipay-payment-skill` 或 `alipay-authenticate-wallet`，不宣称已完成完整宿主安装。
- 检查中曾运行缓存官方安装器的 `install --help`，但它未将此参数视为帮助，进入统一安装。CLI及Skill目标目录写入均被沙箱拒绝，安装返回失败；之后复查CLI仍为0.4.8、上述Skill仍缺失。未提权重跑，没有清理钱包目录。故不能把该命令当作安全的只读帮助入口，也不能声称所有临时文件均未变化。
- 安装器本次返回的信息显示可用CLI为0.4.9；本轮没有完成升级，不使用新版行为推断0.4.8兼容性。
- 两次已保存的失败JSON均为 `SIGN_VERIFY_FAILED / 签名校验失败`，未发现request ID、trace ID或correlation ID字段。
- 原生 `--help` 列表中未见明确的安全诊断导出命令，不猜测debug参数、不读取钱包凭据。存在 `problem-feedback --reason`；[官方反馈流程](https://github.com/alipay/payment-skills/blob/main/alipay-payment-skill/references/feedback.md)要求先向用户展示并确认内容，再发送。

脱敏原始核对结果留在忽略目录 `.local-run/agentpay-live/audit-20261029/read-only-audit.json`。没有为本轮审计调用官方支付或钱包命令，也没有更新服务器程序。

## 仍需官方确认

[办公智能体产品页](https://aipay.alipay.com/products/pc-agent-pay)可读到沙箱测试说明与测试买家步骤，但这些内容没有明确给出PC CLI 0.4.8与正式绑定钱包、沙箱商户链接的组合支持矩阵。[PC商家接入指南](https://aipay.alipay.com/docs/agent-pay/skillpay.html)也未给出这项组合的确定结论。[商户接入指南](https://aipay.alipay.com/merchant-guide)列出macOS/Linux、Node.js v22+；产品页列Codex CLI为已支持，但不能据此推断Windows上的当前组合必然完成全部初始化。

需确认PC沙箱测试路径、会话身份规则、完整Skill安装的影响、后台验签层及追踪方法。参见[已更新的咨询草稿](AGENTPAY-SUPPORT-REQUEST.md)。用户已明确选择不发送官方反馈、自行联系官方；本轮没有发送邮件或problem-feedback。

## 审计后的用户授权复核

用户随后明确要求拿到控制台公钥后再尝试支付。按修正后的源码 `509be11` 执行一笔新的同额沙箱订单，真实会话绑定和官方摘要格式已生效，未做钱包预检；signal成功，14:27:08的submit仍返回 `SIGN_VERIFY_FAILED`，业务订单未交付。见[本次单笔结果](AGENTPAY-RETRY-2026-10-29.md)。该结果来自用户新授权的一次验证，不覆盖只读审计或旧失败记录；没有自动循环重试。
