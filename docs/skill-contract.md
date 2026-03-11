# Skill 侧接入契约（一期）

## 1. 适用范围

本文档定义 StablePay DEMO 一期与 skill 侧的最小接入契约，包括：

- 官方客户端 skill：`stablepay-skill` 的职责边界
- skill definition / execute 层面的最小契约
- 开发者在 `skill.md` 中插入的支付区块规范（短链接）
- skill 如何使用验证接口（短链接或 canonical API）
- 安全约束（密钥不得明文存储等）

## 2. stablepay-skill 职责边界（一期）

`stablepay-skill`（官方客户端 skill）用于在 OpenClaw/ClawHub 场景中提供最小支付体验与查询能力，其职责边界建议如下：

- 负责：
  - 引导用户/Agent 完成支付必要信息收集（skill_did、价格、币种等）
  - 调用对外 API 完成支付提交（`POST /api/v1/pay`）与结果查询
  - 可选：提供余额/收益等查询入口（调用 `/api/v1/balance`、`/api/v1/revenue` 等）
- 不负责：
  - 保存用户 DID 私钥到服务端
  - 直接访问内部 RPC
  - 绕过对外 API 直接写入购买关系

## 3. skill definition / execute 最小契约（一期）

一期目标是跑通“付费 skill 买断制购买闭环”。对 skill 的最小要求：

- skill 元数据中应包含可被 Agent 读取的“支付说明/支付入口/验证入口”
- skill 执行时（或后端服务处理请求时）能够在必要场景调用验证接口，防止篡改绕过支付

## 4. 开发者 `skill.md` 支付区块规范（一期）

### 4.1 支付区块（建议模板）

开发者在 `skill.md` 中插入支付区块（可放在文件开头或结尾）：

```text
## StablePay 支付

此 Skill 需要支付 {PRICE} USDC 购买

支付链接：https://api.stablepay.co/pay?skill={SKILL_DID}&price={PRICE}

购买验证：https://api.stablepay.co/verify?skill={SKILL_DID}&agent={AGENT_DID}
```

变量说明：

- `{SKILL_DID}`：一期等同于收款方 DID（developer/payee DID）
- `{PRICE}`：价格（字符串 decimal，建议 2 位小数展示）
- `{AGENT_DID}`：付款方 DID（由 Agent 在运行时填充/携带）

### 4.2 短链接语义

- `GET /pay?skill=...&price=...`：支付挑战/引导入口（网关短链，面向 skill.md 嵌入）
  - 不替代 `POST /api/v1/pay`
- `GET /verify?skill=...&agent=...`：验证入口（网关短链）
  - 由网关映射到 `GET /api/v1/verify`

## 5. skill 如何使用 verify 接口（一期）

一期提供两种使用方式：

1. **短链接方式（推荐用于 skill.md）**
   - `GET https://api.stablepay.co/verify?skill={SKILL_DID}&agent={AGENT_DID}`
2. **canonical API（推荐用于开发者后端服务）**
   - `GET /api/v1/verify?skill_did=...&agent_did=...`
   - 支持 API Key 或 DID 签名认证（见 `docs/external-api-contract.md`）

## 6. 安全约束（一期必须遵守）

- 密钥/私钥不得明文存储在代码仓库、配置文件或日志中
- DID 私钥应由客户端本地安全存储，从不通过网络传输
- 签名请求应包含时间戳（有效期 5 分钟）并具备防重放机制（nonce/缓存已处理签名）

