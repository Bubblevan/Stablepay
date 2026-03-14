## 1. 需求概述

### 1.1 项目背景

随着 AI Agent 生态的快速发展，去中心化身份和小额支付需求日益增长。ClawHub 作为 AI Skill 市场平台，迫切需要一个安全、便捷的支付解决方案来支持付费 skill 的商业化。传统的中心化支付方式存在账户管理复杂、资金托管风险、跨境支付限制等问题，无法满足 AI Agent 生态的去中心化特性。

StablePay 基于 W3C did:solana 标准提供去中心化支付解决方案，采用 HTTP 402 协议实现 AI Agent 小额支付。产品架构包含三个核心组件：

- StablePay Skill（客户端）：运行在 OpenClaw 中，所有用户交互通过对话完成，无需复杂界面操作
- StablePay API（服务端）：处理支付验证、链上交易、记录管理等核心功能
- StablePay 网站（https://stablepay.co/ai）：展示代码模板和产品介绍，无需用户注册/登录
  
一期产品仅支持 Solana 主网，使用 USDC/USDT 作为支付货币，后续将扩展至以太坊等其他区块链网络。

### 1.2 需求来源

市场需求：

- AI Agent 生态快速发展，需要去中心化的身份和支付体系
- 现有支付方案无法满足 Agent 自主决策和小额支付需求
- 开发者需要简单易用的商业化工具
  
业务需求：

- ClawHub 平台需要支持付费 skill，实现平台和开发者收益
- 用户需要便捷的 skill 购买体验，Agent 需要自主支付能力
- 平台需要防篡改的支付验证机制
  
技术驱动：

- HTTP 402 协议为 AI Agent 小额支付提供标准化解决方案
- W3C DID 标准实现去中心化身份管理
- Solana 区块链提供低成本、高性能的支付基础设施
 
### 1.3 目标用户

主要用户：AI Agent 用户（终端用户）

- 用户画像：使用 OpenClaw 平台的 AI Agent 用户，希望通过 Agent 购买和使用付费 skill
- 核心需求：通过对话便捷购买付费 skill，Agent 能够在用户授权下自主完成小额支付
- 交互方式：所有操作通过与 Agent 对话完成，无需学习复杂的支付界面
  
次要用户：Skill 开发者

- 用户画像：在 ClawHub 发布 skill 的开发者，希望将 skill 商业化并获得收益
- 核心需求：快速集成支付能力，查询销售收益，防止支付被绕过
- 交互方式：复制官网代码模板集成到 skill，通过 StablePay skill 对话查询收益
  
### 1.4 产品目标

业务目标：

- 上线后 3 个月内支持 50+ 付费 skill 集成 StablePay
- 促成 500+ 笔 skill 购买交易，建立用户使用习惯
- 平台 GMV 达到 10,000 USDC，验证商业模式
  
用户体验目标：

- Agent 用户首次购买 skill 耗时 < 2 分钟，包含钱包创建和充值引导
- 开发者集成 StablePay 耗时 < 5 分钟，复制-替换-粘贴即可完成
- 所有交互通过对话完成，用户无需学习复杂的支付界面
- 用户通过单次购买限额和自动购买阈值灵活控制 Agent 支付行为
  
成功指标：

- 核心指标：付费 skill 数量、购买交易数、GMV
- 辅助指标：StablePay skill 安装量、开发者活跃数
  
## 2. 功能需求

### 2.1 用户侧（Agent）功能

#### 2.1.1 功能列表

功能模块
功能名称
优先级
状态
功能描述
身份管理
钱包注册
P0
待开发
Agent 通过对话引导注册 Solana 钱包
身份管理
X 账号绑定
P0
待开发
通过 X 平台发推验证并绑定账号（防刷）
身份管理
注册奖励
P0
待开发
注册成功后获得 1 USDC 奖励
身份管理
钱包充值
P0
待开发
用户向钱包转入 USDT/USDC
支付控制
限额配置
P0
待开发
对话设置单次购买限额和自动购买阈值（本地存储）
Skill 购买
自动购买
P0
待开发
金额低于阈值时自动完成购买
Skill 购买
确认购买
P0
待开发
金额高于阈值时对话确认后购买
账户查询
余额查询
P0
待开发
对话查询当前钱包余额
账户查询
交易记录
P0
待开发
对话查询历史购买记录

#### 2.1.2 功能详细说明

##### 2.1.2.1 钱包注册与 X 账号绑定

功能描述：
用户在 OpenClaw 中安装 StablePay skill 后，通过对话引导完成 Solana 钱包注册和 X 账号绑定验证，生成符合 W3C did:solana 标准的唯一 Agent 身份。绑定成功后获得 1 USDC 注册奖励。

用户价值：

- 去中心化身份：用户完全掌控自己的资产和身份
- 防刷机制：通过 X 账号验证防止批量注册和滥用
- 注册奖励：完成验证即可获得 1 USDC，可立即体验小额支付
- 一次注册：DID 身份可在所有支持 StablePay 的 skill 中通用
- 对话引导：无需理解区块链技术细节
  
功能流程：

1. 用户在 OpenClaw 中对 Agent 说："安装 StablePay skill"
2. Agent 提示："StablePay skill 已安装，是否创建支付钱包？"
3. 用户回复："创建"
4. StablePay skill 调用 API，生成 Solana 钱包和 DID
5. Agent 回复："钱包创建成功！你的 DID 是 did:solana:xxxxx，钱包地址是 xxxxx"
6. Agent 继续提示："为了防止滥用，需要绑定你的 X 账号。请访问验证页面：https://stablepay.co/verify?did=xxxxx"
7. 用户访问验证页面，看到三个步骤：
  - 步骤 1：点击"Post Verification Tweet"按钮
  - 步骤 2：在 X 平台自动发布验证推文（内容包含 DID）
  - 步骤 3：复制推文链接，粘贴到验证页面，点击"Verify & Claim"
8. 验证成功后，StablePay 自动发送 1 USDC 到用户钱包
9. Agent 通知："✅ X 账号绑定成功！你已获得 1 USDC 注册奖励，当前余额 1 USDC"
  
验收标准：

- 成功生成符合 W3C DID 规范的 did:solana 标识符
- 钱包私钥加密存储在本地
- X 账号验证成功率 ≥ 95%（排除网络问题）
- 验证时间 < 60 秒
- 同一 X 账号只能绑定一个 DID（防止重复注册）
- 同一 DID 只能绑定一个 X 账号
- 注册奖励自动发放，到账时间 < 30 秒
- 对话流程顺畅，无需用户手动输入复杂信息
  
2.1.2.2 支付控制配置

功能描述：
用户通过对话设置单次购买限额和自动购买阈值。配置加密存储在 StablePay skill 本地，用户完全掌控。

用户价值：

- 风险控制：限制单次购买最高金额，防止 Agent 失控消费
- 效率提升：低价 skill 自动购买，无需反复确认
- 隐私保护：配置存储在本地，无需上传服务器
  
功能流程：

1. 用户对 Agent 说："配置支付限额"
2. Agent 询问："请告诉我单次购买最多花费多少 USDC？"
3. 用户回复："50"
4. Agent 询问："好的，单次购买限额设为 50 USDC。接下来，请告诉我多少 USDC 以下可以自动购买（无需确认）？"
5. 用户回复："5"
6. Agent 解析配置，加密保存到 StablePay skill 本地
7. Agent 确认："已配置成功：单次购买限额 50 USDC，自动购买阈值 5 USDC"
  
验收标准：

- 配置采用 AES-256 加密存储在本地
- Agent 能准确解析自然语言输入的数字
- 修改配置需要用户二次确认
  
2.1.2.3 HTTP 402 支付流程

功能描述：
Agent 访问付费 skill 资源时，根据支付金额和配置，自动完成购买或请求用户确认，通过 Solana 钱包签名完成支付。

用户价值：

- 无缝体验：低价 skill 自动购买，高价 skill 对话确认
- 安全可控：每笔支付都有签名验证
- 标准协议：基于 HTTP 402 协议，跨平台通用
  
功能流程（自动购买）：

1. Agent 访问 skill 资源链接，收到 HTTP 402 响应
2. StablePay skill 解析支付要求（金额 3 USDC）
3. 检查本地配置（阈值 5 USDC），判断可自动购买
4. 检查余额（10 USDC），判断余额充足
5. StablePay skill 使用本地私钥签名支付请求
6. 调用 StablePay API 完成链上交易
7. Agent 通知用户："已自动购买 skill（3 USDC），当前余额 7 USDC"
  
功能流程（确认购买）：

1. Agent 访问 skill 资源链接，收到 HTTP 402 响应
2. StablePay skill 解析支付要求（金额 15 USDC）
3. 检查本地配置（阈值 5 USDC），判断超过阈值
4. Agent 询问用户："发现付费 skill（15 USDC），当前余额 50 USDC，是否购买？"
5. 用户回复："购买"
6. StablePay skill 使用本地私钥签名支付请求
7. 调用 StablePay API 完成链上交易
8. Agent 通知用户："支付成功（15 USDC），当前余额 35 USDC"
  
验收标准：

- 自动购买：金额 ≤ 阈值时无需用户确认
- 确认购买：金额 > 阈值时通过对话确认
- 支付成功率 ≥ 95%
- 链上确认时间 ≤ 30 秒
- 余额不足时提示用户充值
  
2.1.2.4 余额与交易记录查询

功能描述：
用户通过对话查询钱包余额和历史购买记录。StablePay skill 调用 API 获取数据，通过对话展示。

用户价值：

- 实时掌握资产状况
- 追踪消费记录
- 对话交互，无需打开额外页面
  
功能流程（余额查询）：

1. 用户对 Agent 说："查询余额"
2. StablePay skill 调用 StablePay API 查询链上余额
3. Agent 回复："你的钱包余额为 25.5 USDC"
  
功能流程（交易记录）：

1. 用户对 Agent 说："查看最近的购买记录"
2. StablePay skill 调用 StablePay API 获取交易记录
3. Agent 回复："最近 3 笔购买：
  - 2026-02-13 10:30 | Skill: AI 写作助手 | 15 USDC
  - 2026-02-12 15:20 | Skill: 数据分析工具 | 8 USDC
  - 2026-02-10 09:45 | Skill: 图像生成器 | 3 USDC"
    
验收标准：

- 余额查询响应时间 < 2 秒
- 交易记录默认展示最近 10 笔
- 支持按时间筛选（本月、上月、全部）
- 对话展示清晰易懂
  
2.2 开发者侧（Skill）功能

2.2.1 功能列表

功能模块
功能名称
优先级
状态
功能描述
身份管理
钱包注册
P0
待开发
开发者创建 Solana 钱包作为 Skill 身份
身份管理
X 账号绑定
P0
待开发
通过 X 平台发推验证并绑定账号（防刷）
身份管理
注册奖励
P0
待开发
注册成功后获得 1 USDC 奖励
支付集成
获取代码模板
P0
待开发
从官网复制支付代码模板
支付集成
修改 DID
P0
待开发
将模板中的 DID 替换为自己的钱包地址
支付集成
集成到 skill.md
P0
待开发
将代码插入 skill.md
后端验证
购买验证 API
P0
待开发
后端调用 API 验证购买记录（可选）
收益管理
查询收益
P0
待开发
通过 StablePay skill 对话查询收益
收益管理
交易记录
P0
待开发
通过 StablePay skill 对话查询销售记录

2.2.2 功能详细说明

2.2.2.1 支付代码获取与集成

功能描述：
开发者访问 StablePay 官网（https://stablepay.co/ai），复制支付代码模板，将模板中的 {SKILL_DID} 替换为自己的 Solana 钱包地址，插入到 skill.md 文件中。

用户价值：

- 零门槛：无需注册账户，直接使用
- 5 分钟集成：复制-替换-粘贴即可
- 灵活定价：自定义 skill 价格
  
功能流程：

1. 开发者访问 https://stablepay.co/ai
2. 网站展示支付代码模板：
## 💰 StablePay 支付

此 Skill 需要支付 {PRICE} USDC 购买

支付链接：https://api.stablepay.co/pay?skill={SKILL_DID}&price={PRICE}

购买验证：https://api.stablepay.co/verify?skill={SKILL_DID}&agent={AGENT_DID}
3. 开发者复制模板，替换 {SKILL_DID} 为自己的钱包地址
4. 设置 {PRICE} 为自定义价格（如 5）
5. 将代码粘贴到 skill.md 文件开头或结尾
6. 发布 skill 到 ClawHub
  
验收标准：

- 官网无需注册/登录即可访问代码模板
- 代码模板清晰标注需要替换的变量
- 提供示例说明如何替换
  
2.2.2.2 购买验证 API（可选）

功能描述：
对于有后端服务的 skill，开发者可以在后端调用 StablePay API 验证 Agent 是否已购买，防止用户篡改 skill 源代码绕过支付。

用户价值：

- 防篡改：防止用户直接修改 skill 代码跳过支付
- 可选功能：简单 skill 无需后端验证
  
功能流程：

1. Agent 访问 skill 的后端服务
2. 后端服务调用 StablePay API：
GET https://api.stablepay.co/verify?skill=did:solana:dev123&agent=did:solana:user456
3. StablePay API 返回验证结果：
  - 已购买：{"purchased": true, "timestamp": "2026-02-13T10:30:00Z"}
  - 未购买：{"purchased": false}
4. 如果未购买，后端返回 HTTP 402
5. StablePay skill 拦截 402 响应，引导 Agent 完成支付
  
验收标准：

- API 响应时间 < 500ms
- API 调用免费（开发者无需付费）
  
2.2.2.3 收益与交易记录查询

功能描述：
开发者在 OpenClaw 中安装 StablePay skill，通过对话查询自己的 skill 销售收益和交易记录。

用户价值：

- 实时收益：随时查看 skill 销售情况
- 统一入口：与用户使用相同的 skill 查询
- 对话交互：无需登录后台系统
  
功能流程（收益查询）：

1. 开发者对 Agent 说："查询我的 skill 收益"
2. StablePay skill 识别开发者身份（读取本地开发者 DID）
3. 调用 StablePay API 查询该 DID 的总收益
4. Agent 回复："你的 skill 总收益为 120 USDC，共售出 24 次，当前余额 120 USDC"
  
功能流程（交易记录）：

1. 开发者对 Agent 说："查看最近的销售记录"
2. StablePay skill 调用 API 获取交易记录
3. Agent 回复："最近 3 笔销售：
  - 2026-02-13 14:20 | 购买者: did:solana:abc... | 5 USDC
  - 2026-02-13 10:30 | 购买者: did:solana:def... | 5 USDC
  - 2026-02-12 16:45 | 购买者: did:solana:ghi... | 5 USDC"
    
验收标准：

- 开发者和用户使用同一个 StablePay skill
- 自动识别身份（用户 vs 开发者）
- 支持按 skill 筛选（开发者可能有多个付费 skill）
  
2.3 StablePay 平台侧功能

2.3.1 功能列表

功能模块
功能名称
优先级
状态
功能描述
HTTP 402
402 请求生成
P0
待开发
生成标准 HTTP 402 支付要求响应
HTTP 402
签名验证
P0
待开发
验证 Agent 钱包签名的合法性
链上处理
交易处理
P0
待开发
调用 Solana 链完成 USDC 转账
链上处理
Gas 费补贴
P0
待开发
StablePay 补贴 Solana 交易 Gas 费
记录管理
购买记录存储
P0
待开发
存储 Agent 购买 Skill 的关系记录
记录管理
购买验证 API
P0
待开发
提供验证接口供开发者后端调用
记录管理
X 账号绑定记录
P0
待开发
存储 DID 与 X 账号的绑定关系，防止重复注册
资金结算
注册奖励发放
P0
待开发
验证成功后自动发放 1 USDC 到用户钱包
资金结算
实时结算
P0
待开发
支付成功后立即转账到开发者钱包
数据查询
余额查询 API
P0
待开发
查询 Agent 钱包余额和消费统计
数据查询
交易记录 API
P0
待开发
查询用户/开发者的交易历史

2.3.2 功能详细说明

2.3.2.1 HTTP 402 支付流程处理

功能描述：
StablePay 作为 HTTP 402 协议的 Facilitator，处理完整的支付流程：生成 402 响应、验证签名、执行链上交易、记录购买关系。

核心价值：

- 标准协议：基于 HTTP 402 标准，实现 AI Agent 小额支付
- 安全可靠：签名验证 + 链上交易 + 记录存储
- 实时结算：无需中间商，资金直达开发者
  
功能流程：

1. 第一阶段：Agent 首次访问
  
  - Agent 通过 StablePay skill 访问付费 skill 资源
  - StablePay API 检测到未购买记录
  - 返回 HTTP 402 响应，包含支付要求：
{
  "status": 402,
  "skill_did": "did:solana:dev123",
  "price": 5,
  "currency": "USDC",
  "message": "此 Skill 需要支付 5 USDC"
}
2. 第二阶段：Agent 签名支付
  
  - StablePay skill 读取本地钱包私钥
  - 构造支付请求并签名
  - 重新访问资源，携带签名：
Authorization: StablePay signature=xxx, agent_did=did:solana:user456
3. 第三阶段：StablePay 验证和执行
  
  - 验证签名合法性（防伪造）
  - 检查钱包余额（防透支）
  - 调用 Solana 链完成 USDC 转账（Agent → 开发者）
  - StablePay 补贴 Gas 费
  - 记录购买关系到数据库
  - 返回 HTTP 200，Agent 可访问 skill
    
验收标准：

- 符合 HTTP 402 Payment Required 标准规范
- 签名验证成功率 ≥ 99.9%
- 链上交易确认时间 ≤ 30 秒
- 购买记录永久存储，支持验证查询
  
2.3.2.2 实时资金结算与 Gas 费补贴

功能描述：
支付成功后，USDC 立即从 Agent 钱包转账到开发者钱包，无需中间账户。StablePay 补贴所有 Solana 链上交易的 Gas 费。

核心价值：

- 资金安全：无需信任中间方，链上直接结算
- 开发者友好：Gas 费由 StablePay 承担
- 用户友好：用户只需持有 USDC，无需准备 SOL
  
功能流程：

1. Agent 支付 5 USDC 购买 skill
2. StablePay 调用 Solana SPL Token 合约：
  - 转账：Agent 钱包 → 开发者钱包（5 USDC）
  - Gas 费支付：StablePay 钱包 → Solana 网络（约 0.00001 SOL）
3. 交易上链确认
4. StablePay 记录交易哈希和 Gas 费支出
  
验收标准：

- 资金实时到账，无延迟
- Gas 费由 StablePay 承担，用户和开发者无需支付
- 交易哈希可追溯，公开透明
  
2.3.2.3 X 账号验证与注册奖励

功能描述：
用户或开发者创建钱包后，需要通过 X 平台发推验证并绑定账号。验证成功后，StablePay 自动发放 1 USDC 注册奖励。

核心价值：

- 防刷机制：通过真实社交账号验证，防止批量注册和滥用
- 用户激励：注册即得 1 USDC，降低使用门槛
- 社交传播：用户发推有助于产品推广
  
验证流程：

1. 用户访问验证页面
  
  - URL 格式：https://stablepay.co/verify?did={USER_DID}
  - 页面展示三个步骤和当前状态
2. 步骤 1：发布验证推文
  
  - 用户点击"Post Verification Tweet"按钮
  - 自动跳转到 X 平台，预填充推文内容：
I'm verifying my StablePay DID: did:solana:xxxxx

Join me on @StablePay to enable AI Agent payments on Solana! 🚀

#StablePay #Solana #AIAgent
  - 用户确认并发布推文
3. 步骤 2：提交推文链接
  
  - 用户复制推文链接（如：https://x.com/username/status/123456789）
  - 粘贴到验证页面的输入框
4. 步骤 3：验证并领取奖励
  
  - 用户点击"Verify & Claim"按钮
  - StablePay 后端验证：
    - 推文是否存在且公开可见
    - 推文内容是否包含正确的 DID
    - 推文发布者的 X 账号是否已绑定其他 DID
    - DID 是否已绑定其他 X 账号
  - 验证成功后：
    - 存储 DID ↔ X 账号绑定关系
    - 发送 1 USDC 到用户钱包
    - 返回成功状态
5. OpenClaw 中通知用户
  
  - Agent 自动检测验证状态
  - 验证成功后通知："✅ X 账号绑定成功！已绑定 X 账号：@username，注册奖励 1 USDC 已到账"
    
验收标准：

- 推文内容验证准确率 ≥ 99%
- 防止重复绑定：同一 X 账号只能绑定一个 DID
- 防止多账号：同一 DID 只能绑定一个 X 账号
- 验证时间 < 60 秒
- 奖励发放成功率 ≥ 99.5%
- 奖励到账时间 < 30 秒
- 支持推文链接格式：x.com 和 twitter.com
  
2.3.2.4 购买验证与数据查询 API

**功能描述：**StablePay 提供 API 接口，支持：

- 开发者后端验证 Agent 购买记录
- Agent 查询余额和交易记录
- 开发者查询 skill 收益和销售记录
  
核心价值：

- 防篡改：后端验证防止用户跳过支付
- 数据透明：所有交易可查询、可追溯
- 免费调用：API 调用不收费
  
API 列表：

1. X 账号绑定验证 API
  
POST /verify-twitter
请求体：{
  "did": "did:solana:xxxxx",
  "tweet_url": "https://x.com/username/status/123456789"
}
响应：{
  "success": true,
  "twitter_handle": "@username",
  "reward_tx": "xxx...",
  "message": "Verification successful, 1 USDC sent"
}
2. 购买验证 API
  
GET /verify?skill={SKILL_DID}&agent={AGENT_DID}
响应：{"purchased": true/false, "timestamp": "..."}
3. 余额查询 API
  
GET /balance?agent={AGENT_DID}
响应：{"balance": 25.5, "currency": "USDC"}
4. 交易记录 API（用户）
  
GET /transactions?agent={AGENT_DID}&limit=10
响应：[{"skill_did": "...", "amount": 5, "timestamp": "..."}]
5. 收益查询 API（开发者）
  
GET /revenue?skill={SKILL_DID}
响应：{"total_revenue": 120, "total_sales": 24, "currency": "USDC"}
6. 销售记录 API（开发者）
  
GET /sales?skill={SKILL_DID}&limit=10
响应：[{"agent_did": "...", "amount": 5, "timestamp": "..."}]
  
验收标准：

- 所有 API 响应时间 < 500ms
- 支持 HTTPS 加密传输
- API 调用免费，无需开发者付费
- 支持 CORS，方便前端调用
  
3. 用户体验设计

3.1 交互流程

所有用户交互都通过对话形式完成，不存在可点击的按钮、表单或配置页面。

3.1.1 Agent 首次注册和充值流程

对话脚本：

用户："安装 StablePay skill"
Agent："StablePay skill 已安装。是否创建支付钱包？这将生成一个去中心化身份（DID），用于购买付费 skill。"

用户："创建"
Agent："正在生成钱包...
✅ 钱包创建成功！
- 你的 DID：did:solana:4fK9x2Hy...
- 钱包地址：4fK9x2HyJk...
- 当前余额：0 USDC

接下来需要绑定你的 X 账号以完成注册。请访问验证页面：
https://stablepay.co/verify?did=did:solana:4fK9x2Hy...

（点击上方链接自动打开）"

用户："已打开验证页面"
Agent："请按照页面提示完成验证：
1. 点击 'Post Verification Tweet' 按钮
2. 在 X 平台发布验证推文
3. 复制推文链接并粘贴到验证页面
4. 点击 'Verify & Claim' 完成验证

验证成功后，你将获得 1 USDC 注册奖励！"

（用户完成 X 账号验证）

Agent："✅ X 账号绑定成功！
- 已绑定 X 账号：@username
- 注册奖励：1 USDC 已到账
- 当前余额：1 USDC

现在可以开始使用 StablePay 购买付费 skill 了。建议配置支付限额，防止意外消费。"

（用户完成 X 账号验证）

Agent："✅ X 账号绑定成功！
- 已绑定 X 账号：@username
- 注册奖励：1 USDC 已到账
- 当前余额：1 USDC"

用户："如何充值？"
Agent："你可以通过以下方式充值：
1. 从中心化交易所（如 Binance、OKX）提现 USDC 到钱包地址
2. 从其他 Solana 钱包转账 USDC
3. 使用支持 Solana 的去中心化交易所（如 Jupiter）

钱包地址：4fK9x2HyJk...（点击复制）"

（用户完成充值后）

用户："查询余额"
Agent："✅ 充值成功！
- 当前余额：51 USDC（包含 1 USDC 注册奖励）

建议配置支付限额，防止意外消费。"

用户："配置限额"
Agent："请告诉我单次购买最多花费多少 USDC？"

用户："50"
Agent："好的，单次购买限额设为 50 USDC。接下来，请告诉我多少 USDC 以下可以自动购买（无需确认）？"

用户："5"
Agent："✅ 配置已保存（加密存储在本地）：
- 单次购买限额：50 USDC
- 自动购买阈值：5 USDC

低于 5 USDC 的 skill 将自动购买，超过 5 USDC 将请求你确认。单次购买不会超过 50 USDC。"

3.1.2 自动购买流程（金额低于阈值）

对话脚本：

用户："安装 AI 写作助手 skill"
Agent："正在检查 skill 信息...

📦 Skill 名称：AI 写作助手
💰 价格：3 USDC
✅ 低于自动购买阈值（5 USDC），正在自动购买...

（处理中...）

✅ 购买成功！
- 花费：3 USDC
- 当前余额：47 USDC

AI 写作助手 skill 已安装，可以开始使用。"

3.1.3 确认购买流程（金额高于阈值）

对话脚本：

用户："安装专业数据分析工具 skill"
Agent："正在检查 skill 信息...

📦 Skill 名称：专业数据分析工具
💰 价格：15 USDC
⚠️ 超过自动购买阈值（5 USDC）

当前余额：47 USDC

是否购买此 skill？"

用户："购买"
Agent："正在处理支付...

✅ 购买成功！
- 花费：15 USDC
- 当前余额：32 USDC

专业数据分析工具 skill 已安装，可以开始使用。"

拒绝购买场景：

用户："不购买"
Agent："已取消购买。你可以稍后访问 ClawHub 查看 skill 详情。"

余额不足场景：

用户："安装高级图像生成器 skill"
Agent："正在检查 skill 信息...

📦 Skill 名称：高级图像生成器
💰 价格：50 USDC

❌ 余额不足
- 当前余额：32 USDC
- 需要充值：至少 18 USDC

请向钱包地址 4fK9x2HyJk... 充值后重试。"

3.1.4 开发者发布付费 skill 流程

步骤说明：

1. 创建开发者钱包（与用户流程相同）
  
  - 开发者在 OpenClaw 中安装 StablePay skill
  - 通过对话创建 Solana 钱包
  - 获得 DID 和钱包地址
2. 获取支付代码模板
  
  - 访问 https://stablepay.co/ai（无需注册）
  - 查看代码模板和集成说明
  - 复制模板
3. 修改并集成代码
  
  - 将 {SKILL_DID} 替换为自己的 DID
  - 设置 {PRICE} 为 skill 价格
  - 将代码插入 skill.md
4. 发布 skill
  
  - 将 skill 发布到 ClawHub
  - 用户安装时将触发 StablePay 支付流程
5. 查询收益（通过对话）
  
开发者："查询我的 skill 收益"
Agent："正在查询...

📊 你的 skill 总收益：
- 总收入：120 USDC
- 总销量：24 次
- 当前余额：120 USDC

💰 销售最好的 skill：
- AI 写作助手：80 USDC（16 次）
- 数据分析工具：40 USDC（8 次）"

开发者："查看最近的销售记录"
Agent："最近 5 笔销售：

1. 2026-02-13 14:20
   - Skill：AI 写作助手
   - 购买者：did:solana:abc...
   - 金额：5 USDC

2. 2026-02-13 10:30
   - Skill：AI 写作助手
   - 购买者：did:solana:def...
   - 金额：5 USDC

3. 2026-02-12 16:45
   - Skill：数据分析工具
   - 购买者：did:solana:ghi...
   - 金额：5 USDC

...（更多记录）"

3.1.5 后端服务验证购买流程

场景说明：
对于有后端服务的 skill，可以在后端验证购买记录，防止用户篡改 skill 源代码绕过支付。

流程：

1. Agent 访问 skill 的后端 API
  
Agent 请求：GET https://api.my-skill.com/execute
携带参数：agent_did=did:solana:user456
2. 后端调用 StablePay 验证 API
  
后端请求：GET https://api.stablepay.co/verify?skill=did:solana:dev123&agent=did:solana:user456

StablePay 响应（已购买）：
{
  "purchased": true,
  "timestamp": "2026-02-13T10:30:00Z"
}

或（未购买）：
{
  "purchased": false
}
3. 后端返回结果
  
  - 如果已购买：后端正常处理请求，返回 HTTP 200
  - 如果未购买：后端返回 HTTP 402
4. Agent 处理 402 响应
  
  - StablePay skill 拦截 HTTP 402
  - 触发支付流程（自动购买或请求确认）
  - 支付成功后重新请求后端 API
    
3.2 界面设计要求

由于所有交互都通过对话完成，界面设计要求简化如下：

对话界面设计原则：

- 清晰的信息层级：使用标题符号（📦💰✅❌）突出关键信息
- 简洁的回复：避免冗长文本，分段展示
- 明确的引导：提示用户下一步可以做什么
- 友好的语气：使用贴近用户的自然语言
  
对话展示规范：

- 金额显示：始终标注货币单位（USDC）
- 状态提示：使用图标标识成功（✅）、失败（❌）、警告（⚠️）
- DID 展示：长 DID 缩略显示，支持复制完整地址
- 时间格式：使用易读格式（2026-02-13 14:20）
  
3.3 典型用户场景

场景1：新手小李首次购买付费 skill

用户角色： 刚接触 OpenClaw 的新用户
使用环境： 在 OpenClaw 平台浏览 skill
操作目标： 购买一个付费的 AI 写作助手 skill

对话脚本：

小李："安装 AI 写作助手"
Agent："AI 写作助手是付费 skill（3 USDC），需要安装 StablePay 支付 skill。是否安装？"

小李："安装"
Agent："StablePay skill 已安装。是否创建支付钱包？"

小李："创建"
Agent："✅ 钱包创建成功！
- DID：did:solana:4fK9x2...
- 钱包地址：4fK9x2HyJk...
- 当前余额：0 USDC

接下来需要绑定 X 账号。请访问验证页面：
https://stablepay.co/verify?did=did:solana:4fK9x2..."

小李："已打开验证页面"
Agent："请按照页面提示完成验证：
1. 点击 'Post Verification Tweet' 发布推文
2. 复制推文链接粘贴到验证页面
3. 点击 'Verify & Claim' 完成验证"

（小李完成 X 账号验证）

Agent："✅ X 账号绑定成功！
- 已绑定 X 账号：@xiaoli
- 注册奖励：1 USDC 已到账
- 当前余额：1 USDC"

（小李在交易所充值 50 USDC）

小李："查询余额"
Agent："✅ 充值成功！当前余额：51 USDC（包含 1 USDC 注册奖励）。建议配置支付限额，防止意外消费。"

小李："配置限额"
Agent："请告诉我单次购买最多花费多少 USDC？"

小李："50"
Agent："好的，单次购买限额设为 50 USDC。接下来，请告诉我多少 USDC 以下可以自动购买（无需确认）？"

小李："5"
Agent："✅ 配置已保存：单次购买限额 50 USDC，自动购买阈值 5 USDC。"

小李："安装 AI 写作助手"
Agent："✅ AI 写作助手（3 USDC）低于阈值，自动购买成功！
- 当前余额：48 USDC
- skill 已安装，可以开始使用。"

小李："帮我写一篇关于 AI 的文章"
Agent："好的，我将使用 AI 写作助手 skill 为你生成文章...（调用 skill）"

预期结果： 小李在 5 分钟内完成钱包创建、X 账号绑定、获得注册奖励、完成首次购买，顺利使用付费 skill

场景2：资深用户老王管理支付配置

用户角色： 长期使用 OpenClaw 的资深用户
使用环境： 已购买多个付费 skill
操作目标： 调整支付限额，控制消费

对话脚本：

老王："查询余额"
Agent："当前余额：15 USDC"

老王："查看我的限额配置"
Agent："当前配置：
- 单次购买限额：50 USDC
- 自动购买阈值：5 USDC"

老王："提高单次购买限额到 100"
Agent："确认修改单次购买限额为 100 USDC？"

老王："确认"
Agent："✅ 单次购买限额已更新为 100 USDC。"

老王："修改自动购买阈值为 10"
Agent："✅ 自动购买阈值已更新为 10 USDC。10 USDC 以下的 skill 将自动购买。"

老王："查看最近购买记录"
Agent："最近已购买 7 个 skill，共消费 35 USDC：
1. AI 写作助手 - 5 USDC
2. 数据分析工具 - 10 USDC
3. 图像生成器 - 3 USDC
...（更多记录）"

预期结果： 老王快速调整配置，灵活控制支付行为

场景3：开发者小张发布付费 skill 并获得收益

用户角色： Skill 开发者
使用环境： 准备在 ClawHub 发布付费 skill
操作目标： 集成 StablePay，发布 skill，查询收益

操作步骤：

1. 创建开发者钱包
  
小张："安装 StablePay skill"
Agent："StablePay skill 已安装。是否创建钱包？"

小张："创建"
Agent："✅ 钱包创建成功！DID：did:solana:dev123...

接下来需要绑定 X 账号。请访问验证页面：
https://stablepay.co/verify?did=did:solana:dev123..."

（小张完成 X 账号验证）

Agent："✅ X 账号绑定成功！
- 已绑定 X 账号：@zhangdev
- 注册奖励：1 USDC 已到账
- 当前余额：1 USDC"
2. 访问 StablePay 官网
  
  - 小张访问 https://stablepay.co/ai
  - 查看代码模板：
## 💰 StablePay 支付

此 Skill 需要支付 {PRICE} USDC 购买

支付链接：https://api.stablepay.co/pay?skill={SKILL_DID}&price={PRICE}
3. 集成代码到 skill.md
  
  - 小张复制模板，替换 {SKILL_DID} 为 did:solana:dev123...
  - 设置 {PRICE} 为 5
  - 粘贴到 skill.md 顶部
4. 发布 skill
  
  - 小张将 skill 发布到 ClawHub
  - 设置 skill 名称：AI 写作助手
5. 用户购买（一周后）
  
  - 16 名用户购买了小张的 skill
6. 查询收益
  
小张："查询我的 skill 收益"
Agent："✅ 你的 skill 总收益：
- 总收入：80 USDC
- 总销量：16 次
- 平均单价：5 USDC
- 当前钱包余额：80 USDC"

小张："查看最近的销售记录"
Agent："最近 5 笔销售：
1. 2026-02-13 14:20 | did:solana:abc... | 5 USDC
2. 2026-02-13 10:30 | did:solana:def... | 5 USDC
3. 2026-02-12 16:45 | did:solana:ghi... | 5 USDC
...（更多记录）"
  
预期结果： 小张在 5 分钟内完成钱包创建、X 账号绑定、获得注册奖励、集成 StablePay，一周后获得首笔收益

场景4：用户遇到余额不足

用户角色： 普通用户
使用环境： 购买 skill 时余额不足
操作目标： 充值并完成购买

对话脚本：

用户："安装高级图像生成器"
Agent："高级图像生成器需要支付 50 USDC。

❌ 余额不足
- 当前余额：15 USDC
- 需要充值：至少 35 USDC

请向钱包地址 4fK9x2HyJk... 充值后重试。"

用户："复制充值地址"
Agent："已复制钱包地址：4fK9x2HyJk...

你可以从交易所或其他钱包转入 USDC。"

（用户完成充值）

用户："查询余额"
Agent："✅ 充值成功！
- 当前余额：65 USDC

现在可以购买高级图像生成器了。"

用户："安装高级图像生成器"
Agent："高级图像生成器需要支付 50 USDC，超过自动购买阈值。是否购买？"

用户："购买"
Agent："✅ 购买成功！
- 花费：50 USDC
- 当前余额：15 USDC
- skill 已安装，可以开始使用。"

预期结果： 用户顺利完成充值并购买

4. 非功能需求

4.1 性能要求

- API 响应时间：
  - 余额查询：< 2 秒
  - 交易记录查询：< 3 秒
  - 购买验证：< 500ms
- 链上交易：
  - 交易提交时间：< 5 秒
  - 交易确认时间：< 30 秒
- 并发能力：
  - 支持 100+ TPS（每秒交易数）
  - 支持 1000+ 并发 API 请求
- 系统可用性：
  - 服务可用性 ≥ 99.5%
  - 计划停机时间 < 1 小时/月
    
4.2 安全要求

- 身份认证：
  - 基于 W3C DID 标准的去中心化身份
  - 每笔支付需要钱包签名验证
  - 防止签名重放攻击（timestamp + nonce）
- 数据加密：
  - 本地配置采用 AES-256 加密存储
  - 钱包私钥加密存储，不上传服务器
  - API 通信使用 HTTPS 加密
- 支付安全：
  - 支付前检查余额，防止透支
  - 支付金额验证，防止篡改
  - 单次购买限额保护，防止失控消费
  - 超过单次限额的购买请求将被拒绝
- 访问控制：
  - 开发者只能查询自己的 skill 收益
  - Agent 只能查询自己的交易记录
  - API 调用需要 DID 签名认证
    
4.3 兼容性要求

- 区块链兼容：
  - 一期支持：Solana 主网（Mainnet）
  - 不支持测试网（MVP 阶段）
  - 未来扩展：以太坊、Polygon 等
- 稳定币支持：
  - 一期支持：USDC、USDT（Solana SPL Token）
  - 未来扩展：其他链的稳定币
- 平台兼容：
  - 主要支持：OpenClaw 平台
  - 未来扩展：其他支持 HTTP 402 的 AI Agent 平台
    
4.4 可用性要求

- 易用性：
  - 新用户完成首次购买耗时 < 5 分钟
  - 开发者集成 StablePay 耗时 < 5 分钟
  - 所有操作通过对话完成，无需学习复杂界面
- 帮助文档：
  - 提供用户操作指南（如何充值、配置、购买）
  - 提供开发者集成指南（如何获取代码、集成、查询）
  - 提供常见问题解答（FAQ）
- 多语言：
  - 一期支持中文和英文
  - 对话内容支持双语
- 错误提示：
  - 余额不足：明确提示需要充值金额
  - 网络错误：提示重试
  - 签名失败：提示检查钱包配置
    
5. 风险评估

5.1 风险识别

风险类型
风险描述
影响程度
发生概率
风险等级
技术风险
Solana 网络拥堵导致交易延迟
高
中
中等
技术风险
StablePay API 服务中断
高
低
中等
安全风险
用户钱包私钥泄露
高
低
中等
安全风险
X 账号被盗用于验证
中
低
低
安全风险
重复注册刷奖励
中
中
中等
安全风险
开发者后端验证被绕过
中
中
中等
业务风险
注册奖励成本过高
中
中
中等
业务风险
Gas 费补贴成本过高
中
高
中等
业务风险
付费 skill 数量不足，用户需求低
高
中
中等
合规风险
稳定币支付可能涉及法律监管
高
低
中等
用户体验
对话理解错误，误操作支付
中
中
中等

5.2 应对策略

技术风险应对

风险1：Solana 网络拥堵

- 预防措施：
  - 监控 Solana 网络状态，拥堵时提前告知用户
  - 设置合理的交易优先级费用
  - 未来支持多链，分散风险
- 应急预案：
  - 网络拥堵时暂停新交易，保护已提交交易
  - 向用户展示交易排队状态
  - 提供交易哈希供用户追踪
- 责任人： 技术负责人
  
风险2：StablePay API 服务中断

- 预防措施：
  - 部署高可用架构（负载均衡 + 多实例）
  - 数据库定时备份
  - 7x24 监控告警
- 应急预案：
  - 5 分钟内切换到备用服务
  - 紧急情况下临时关闭支付，保护用户资金
  - 服务恢复后补偿受影响用户
- 责任人： 运维负责人
  
安全风险应对

风险3：用户钱包私钥泄露

- 预防措施：
  - 私钥加密存储在本地，不上传服务器
  - 提供私钥导出和备份功能
  - 教育用户保护私钥安全
- 应急预案：
  - 发现泄露后立即提示用户创建新钱包
  - 提供资产转移工具
- 责任人： 安全负责人
  
风险3：X 账号被盗用于验证

- 预防措施：
  - 要求推文公开可见且内容包含完整 DID
  - 推文必须从待验证的 X 账号发布
  - 检测异常验证行为（同一账号短时间多次尝试）
- 应急预案：
  - 发现异常后冻结相关 DID
  - 人工审核可疑账号
- 责任人： 安全负责人
  
风险4：重复注册刷奖励

- 预防措施：
  - 同一 X 账号只能绑定一个 DID
  - 同一 DID 只能绑定一个 X 账号
  - 监控注册行为，识别批量注册模式
  - 限制每日注册奖励发放总量
- 应急预案：
  - 发现批量注册后暂停奖励发放
  - 审核可疑账号，封禁违规 DID
  - 追回已发放的异常奖励
- 责任人： 风控负责人
  
风险5：开发者后端验证被绕过

- 预防措施：
  - 在开发者文档中强调后端验证的重要性
  - 提供示例代码和最佳实践
  - 对于高价值 skill，建议强制后端验证
- 应急预案：
  - 监控异常购买行为（同一 Agent 短时间多次购买同一 skill）
  - 提供购买记录查询工具，帮助开发者排查
- 责任人： 产品负责人
  
业务风险应对

风险6：注册奖励成本过高

- 预防措施：
  - 监控每日奖励发放量
  - 设置奖励预算上限
  - 通过 X 账号验证控制注册量
- 应急预案：
  - 奖励超预算时降低奖励金额或暂停
  - 调整为邀请制注册
- 责任人： 财务负责人
  
风险7：Gas 费补贴成本过高

- 预防措施：
  - 监控每日 Gas 费支出
  - 设置 Gas 费预算上限
  - 选择 Gas 费较低的区块链（Solana 相对便宜）
- 应急预案：
  - Gas 费超预算时，转为用户承担部分费用
  - 优化交易批处理，降低 Gas 费
- 责任人： 财务负责人
  
风险8：付费 skill 数量不足

- 预防措施：
  - 主动邀请优质开发者入驻
  - 提供开发者激励计划（前 100 个 skill 免除平台费）
  - 市场推广，吸引更多开发者
- 应急预案：
  - 官方发布示范性付费 skill
  - 与知名开发者合作，提升平台吸引力
- 责任人： 运营负责人
  
合规风险应对

风险9：稳定币支付法律监管

- 预防措施：
  - 咨询法律顾问，了解各国监管政策
  - 在用户协议中明确责任和风险
  - StablePay 作为技术服务商，不托管资金
- 应急预案：
  - 监管政策变化时，及时调整产品设计
  - 支持合规的稳定币（如 USDC）
- 责任人： 法务负责人
  
用户体验风险应对

风险10：对话理解错误导致误操作

- 预防措施：
  - 对于支付操作，增加二次确认
  - 支付金额超过阈值时强制用户确认
  - 提供撤销功能（限时）
- 应急预案：
  - 用户误操作后可申诉，人工审核退款
  - 优化对话理解模型，降低误判率
- 责任人： 产品负责人
  
## 6. 附录

6.1 参考文档

技术标准：

- W3C DID 规范
- Solana DID 方法规范
- HTTP 402 Payment Required 规范
- Solana 开发者文档
- SPL Token 标准
  
产品资源：

- StablePay 官方网站
- ClawHub 平台文档
- OpenClaw Agent 开发指南
  
竞品参考：

- Web Monetization API
- Lightning Network（比特币二层支付）
- Stripe Connect（传统支付平台）
  
6.2 术语解释

术语
解释
DID
Decentralized Identifier，去中心化标识符，符合 W3C 标准的身份体系
did:solana
Solana 区块链上的 DID 方法，格式如 did:solana:4fK9x2Hy...
HTTP 402
HTTP 状态码"Payment Required"，表示需要支付才能访问资源
Facilitator
HTTP 402 协议中的支付促进者，负责验证和处理支付
Agent
AI Agent，人工智能代理，可代表用户执行任务
Skill
OpenClaw 平台上的功能模块，类似于插件或应用
ClawHub
Skill 市场，开发者发布 skill，用户安装 skill
USDC
USD Coin，美元稳定币，1 USDC = 1 USD
USDT
Tether，美元稳定币，1 USDT ≈ 1 USD
SPL Token
Solana Program Library Token，Solana 链上的代币标准
Gas 费
区块链交易手续费，Solana 上约为 0.00001 SOL/交易
Solana 主网
Solana Mainnet，正式运行的区块链网络
钱包地址
Solana 公钥地址，用于接收和发送代币
私钥
钱包的密钥，用于签名交易，需妥善保管
签名
使用私钥对交易进行加密签名，证明交易由本人发起
链上交易
在区块链上记录的交易，公开透明、不可篡改
TPS
Transactions Per Second，每秒交易数，衡量系统性能
MVP
Minimum Viable Product，最小可行产品，包含核心功能的初始版本
P0/P1
功能优先级，P0 为核心功能必须实现，P1 为重要功能优先实现
GMV
Gross Merchandise Volume，商品交易总额
API
Application Programming Interface，应用程序接口
HTTPS
HTTP Secure，加密的 HTTP 通信协议
AES-256
高级加密标准，256 位密钥长度，安全性强
X 账号验证
通过在 X (Twitter) 平台发布推文来验证用户真实性的机制
注册奖励
用户完成钱包注册和 X 账号绑定后获得的 1 USDC 奖励
