# ShowMeTheMoney Pro Demo Backend

用于演示“带后端验证的付费 Skill”最小闭环。

完整链路如下：

1. OpenClaw / Skill 调用 `GET /execute?agent_did=...`
2. merchant backend 先调用 StablePay 网关 `GET /api/v1/verify`
3. 若用户尚未购买，backend 返回 `402 Payment Required`
4. OpenClaw 侧的 StablePay 插件接管支付流程（调用 `/api/v1/pay`）
5. 支付成功后，Skill 重试 `GET /execute`
6. backend 再次校验购买状态，并返回一个 **merchant-generated proof**
7. 这个 proof 只能由 merchant backend 生成，用来证明“受保护能力已通过后端验证后释放”

## 核心设计

这个 demo backend 的重点不是返回一个普通字符串，而是返回一个：

- 只有 backend 才能生成
- 不能由插件本地伪造
- 也不能单纯靠查询 Solana RPC 获得

的独特结果。

成功后返回的数据中会包含：

- `proof_id`
- `canonical`
- `signature`
- `display_token`

其中 `display_token` 是最直观的“购买后解锁证明”。

## 主要接口

### `GET /healthz`

健康检查。

返回：

- 服务状态
- skill_name
- skill_did
- 默认价格与币种
- gateway base url

### `GET /execute?agent_did=<did>&q=<optional_text>`

唯一真正的付费商品入口。

行为：

1. 读取 `agent_did`
2. 调用 StablePay 网关 `/api/v1/verify`
3. 若未购买，返回 `402 Payment Required`
4. 若已购买，生成并返回 merchant-generated proof

#### 未购买时返回

HTTP `402`

```json
{
  "code": 402,
  "message": "Payment Required",
  "agent_did": "did:solana:...",
  "skill_did": "did:solana:...",
  "skill_name": "ManualDemoSkill2",
  "price": "1.00",
  "currency": "USDC",
  "payment_endpoint": "/api/v1/pay",
  "merchant_backend": {
    "endpoint": "/execute",
    "request_text": "..."
  },
  "payment_requirement": { "...": "..." }
}
```

#### 已购买时返回
HTTP `200`
```json
{
  "ok": true,
  "product": "showmethemoney-pro",
  "protected_result": {
    "kind": "merchant-generated-proof",
    "message": "Show me the money: premium access granted",
    "request_text": "demo",
    "proof": {
      "proof_id": "smtm_...",
      "issued_at": "2026-04-13T11:11:11Z",
      "canonical": "...",
      "signature": "...",
      "display_token": "SHOW-ME-THE-MONEY::..."
    }
  },
  "access": {
    "agent_did": "did:solana:...",
    "skill_did": "did:solana:...",
    "verified_by_backend": true,
    "verified_at": "2026-04-13T11:11:11Z"
  },
  "verify_snapshot": { "...": "..." }
}
```
## 为什么这能证明“付费后真正解锁”

因为成功结果来自 merchant backend 的二次验证和独立生成：

* 不是插件本地状态里的缓存
* 不是“只要有钱包就默认放行”
* 不是直接查链上余额就能得到
* 不是单纯由 Skill 文本模板拼出来的

只有当 backend 通过 StablePay 验证确认该 `agent_did` 已购买当前 `skill_did` 后，才会生成 proof。

## 启动

### 1. 安装依赖

```bash
cd /mnt/d/MyLab/StablePay/showmethemoney-skill/merchant-backend
npm install
```

### 2. 配置环境变量

本地 `.env` 用于保存不进 Git 的后端配置。按需参考 `.env.example` 增补变量；不要用模板覆盖已有 `.env`：

```bash
# .env 已存在时只增补需要的变量，不要覆盖已有本地配置
```

本机 `.env` 已包含随机生成的 `MERCHANT_PROOF_SECRET`。Alipay 应用密钥单独由 Skill 写入 `.alipay-sandbox.json`；不要复制到 `.env`。

### 3. 启动服务

```bash
npm start
```

或：

```bash
node server.mjs
```

## 环境变量

通用服务配置通过本机 `.env` 或部署环境变量提供；`.env.example` 只包含非敏感模板值。Alipay 沙箱应用密钥从 `.alipay-sandbox.json` 读取。

| 变量名                     | 默认值                                             | 说明                               |
| ----------------------- | ----------------------------------------------- | -------------------------------- |
| `PORT`                  | `8787`                                          | backend 监听端口                     |
| `GATEWAY_BASE_URL`      | `https://ai.wenfu.cn`                           | StablePay api-gateway 地址         |
| `STABLEPAY_API_KEY`     | `stablepay-dev-key`                             | 调 `/api/v1/verify` 时使用的 API key  |
| `SKILL_DID`             | *(必填，见 .env)*                                | 当前商品对应的 skill DID                |
| `SKILL_NAME`            | `showmethemoney-pro`                            | 当前商品名                            |
| `PRICE`                 | *(必填，见 .env)*                                | 价格（与 `CURRENCY` 组合使用）          |
| `CURRENCY`              | `USDC`                                          | 默认币种                             |
| `MESSAGE`               | `Pay to unlock ShowMeTheMoney Pro premium content` | 默认支付提示                        |
| `MERCHANT_PROOF_SECRET` | *(必填)*                                         | 用于生成 merchant proof 的后端私有 secret |
| `ENABLE_DEBUG_ROUTES`   | `0`                                             | 是否启用调试接口                         |
| `INTERNAL_BASE_URL`     | `http://127.0.0.1:8184`                         | 内部调试服务地址                         |

**注意**：`SKILL_DID` 和 `PRICE` 是必填项，必须在 `.env` 或 `.env.local` 中配置。服务启动时会读取这些值并在 402 响应中返回给客户端。

## 配置示例

### 使用 .env 文件（推荐）

默认 `.env` 文件已包含开发环境配置。直接启动：

```bash
npm start
```

### 使用 .env.local 覆盖特定配置

```bash
cp .env .env.local
# 编辑 .env.local，例如修改价格：
# PRICE=15.00
npm start
```

### 使用环境变量直接覆盖（用于 CI/CD）

```bash
export PRICE=20.00
export MERCHANT_PROOF_SECRET='your-production-secret-here'
node server.mjs
```

## 本地手工验证

### 健康检查

```bash
curl 'http://127.0.0.1:8787/healthz'
```

### 执行受保护能力

```bash
curl 'http://127.0.0.1:8787/execute?agent_did=did:solana:C2vKSxoDErVhhLrKZvXHMmJcNKqKapx9MNQZrDab33vS&q=demo'
```

* 未购买时应返回 `402`
* 购买后应返回 `200`，并带 `merchant-generated-proof`

### `GET /a2m/execute?q=<optional_text>[&agent_did=<optional_did>]`

同一个受保护能力还支持支付宝 AI 按量付费。已有 `/execute` 的 x402/StablePay 路径保持原样：

1. 首次调用返回 HTTP `402` 和 `Payment-Needed` Header。
2. 客户端完成支付后，带原请求参数和 `Payment-Proof` 重试。
3. 后端调用 `alipay.aipay.agent.payment.verify`，核对本地订单、金额、交易号和资源标识。
4. 后端将履约结果和唯一交易号事务性写入 MySQL，再调用 `alipay.aipay.agent.fulfillment.confirm`。
5. 确认成功后返回同一份已持久化的 merchant-generated proof，并设置 `Payment-Validation` Header。

MySQL 的 `a2m_orders` 表会在首次请求时创建，使用唯一订单号、唯一支付宝交易号、行锁和事务保证幂等；确认失败时可用同一 `Payment-Proof` 重试。沙箱配置从项目根目录的 `.alipay-sandbox.json` 读取，应用和签名密钥取自 `appIds[0]`，Agent Pay 卖家 2088 ID 取自同一应用配置的 `pid`，不复制密钥到 `.env`。沙箱网关和 `api_mock_service_id` 固定为沙箱值。

本机开发时，服务会从 StablePay 根目录 `.env` 只读取已有的 `COMMERCE_RUNTIME_MYSQL_DSN`，复用正在运行的 MySQL；没有该项时，可复制 `.env.example` 并填完整的 `A2M_MYSQL_*`，也支持 `A2M_MYSQL_URL` 或 `A2M_MYSQL_DSN`。生产环境需显式配置数据库连接。`A2M_AMOUNT_CNY` 默认 `0.01`。支付宝配置缺失时，A2M 路由会明确返回 `503`，不会用示例密钥或内存订单继续。

## 调试接口（可选）

仅在 `ENABLE_DEBUG_ROUTES=1` 时开放：

* `GET /developer/revenue`
* `GET /developer/sales`
* `GET /agent/balance`
* `GET /agent/transactions`

这些接口仅用于开发排查，不属于 skill 的主商品路径。

## 适合和 Skill 如何配合

Skill 侧应只围绕 `/execute` 工作：

1. 先请求 `/execute?agent_did=<buyer_did>`
2. 若返回 `200`，直接展示 premium result
3. 若返回 `402`，调用 `stablepay_pay_via_gateway`
4. 支付成功后重试 `/execute`
5. 展示后端返回的 proof token

## 注意事项

* 这个 backend 不是“公开无保护接口”
* 本地钱包存在不等于已购买
* backend verification 才是最终授权边界
* 不要把 `/developer/*` 或 `/agent/*` 路由当成 premium skill 的正式输出
