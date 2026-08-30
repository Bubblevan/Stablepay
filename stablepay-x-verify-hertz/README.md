# StablePay X Verify (CloudWeGo / Hertz)

一个使用真实 X API 读取推文进行验证的 MVP 服务。

## 工作原理

1. 用户在前端看到固定验证文案
2. 用户**手工去 X 发推**（包含指定格式和 DID）
3. 用户把 tweet URL 粘回前端
4. 后端使用 **X API 读取 tweet**（Bearer Token 或 OAuth 1.0a）进行验证
5. 验证成功后发放 mock 奖励

## 特点

- **不需要 OAuth 2.0 callback**：直接使用只读 API 查询公开推文
- **不需要 tweet.write 权限**：用户自己发推
- **支持两种认证方式**：
  - Bearer Token（推荐，适合只读访问公共数据）
  - OAuth 1.0a（Consumer Key + Access Token）

## 配置

复制 `.env.example` 为 `.env` 并填写你的 X API 凭证：

```bash
cp .env.example .env
```

### 方式一：Bearer Token（推荐）

从 X Developer Portal > Projects & Apps > your app > Keys and Tokens 获取 Bearer Token：

```env
X_BEARER_TOKEN=your_bearer_token_here
```

### 方式二：OAuth 1.0a

如果你有 Consumer Key/Secret 和 Access Token/Secret：

```env
X_CONSUMER_KEY=your_consumer_key_here
X_CONSUMER_SECRET=your_consumer_key_secret_here
X_ACCESS_TOKEN=your_access_token_here
X_ACCESS_TOKEN_SECRET=your_access_token_secret_here
```

## 运行

```bash
cd cmd/server
go run main.go
```

服务将在 `:8080` 端口启动。

## API

### 1) 创建 DID

```bash
curl -X POST http://127.0.0.1:8080/api/v1/did \
  -H 'Content-Type: application/json' \
  -d '{
    "did": "did:solana:4fK9x2HyJkMock1111111111111111111111111",
    "wallet_address": "4fK9x2HyJkMock1111111111111111111111111"
  }'
```

### 2) 调用验证接口（真实 X API）

```bash
curl -X POST http://127.0.0.1:8080/api/v1/verify-twitter \
  -H 'Content-Type: application/json' \
  -d '{
    "did": "did:solana:4fK9x2HyJkMock1111111111111111111111111",
    "tweet_url": "https://x.com/username/status/123456789"
  }'
```

后端会：
1. 使用 X API 查询该 tweet
2. 验证 tweet 是否存在且公开
3. 验证 tweet 文本包含指定前缀和 DID
4. 验证绑定关系（同一 DID 只能绑一个 X，同一 X 只能绑一个 DID）
5. 返回验证结果和 mock 奖励

### 3) 查询验证状态

```bash
curl "http://127.0.0.1:8080/api/v1/verify?did=did:solana:4fK9x2HyJkMock1111111111111111111111111"
```

### 4) Mock 端点（开发和测试用）

创建 mock DID（用于本地测试）：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/mock/did \
  -H 'Content-Type: application/json' \
  -d '{
    "did": "did:solana:4fK9x2HyJkMock1111111111111111111111111",
    "wallet_address": "4fK9x2HyJkMock1111111111111111111111111"
  }'
```

## 验证推文格式

用户需要发布包含以下内容的推文：

```
I'm verifying my StablePay DID: did:solana:xxxxxxxxx
```

前缀可通过环境变量 `X_VERIFY_TWEET_PREFIX` 配置，默认为 `"I'm verifying my StablePay DID:"`。

## 验证规则

后端会检查：

1. DID 格式是否有效
2. DID 是否存在于本地存储
3. Tweet URL 格式是否正确（支持 x.com 和 twitter.com）
4. Tweet 是否存在且可访问
5. Tweet 作者是否与 URL 中的用户名一致
6. Twitter 账号是否为公开账号（非 protected）
7. Tweet 文本是否包含完整 DID
8. Tweet 文本是否包含验证前缀
9. 同一 DID 是否已绑定其他 X 账号
10. 同一 X 账号是否已绑定其他 DID

## 技术栈

- Go 1.21+
- 标准库 `net/http`
- X API v2 (推文查询接口)

## 与 OAuth 2.0 版本的对比

| 功能 | 当前 MVP | OAuth 2.0 版本 |
|------|----------|----------------|
| X API 认证 | Bearer Token / OAuth 1.0a | OAuth 2.0 PKCE |
| 用户发推 | 用户手工发推 | 可代用户发推（需 tweet.write）|
| Callback | 不需要 | 需要 |
| 复杂度 | 简单 | 较复杂 |
| 适用场景 | 快速验证、只读场景 | 需要发帖、深度集成 |

OAuth 2.0 相关代码保留在仓库中，如需启用可参考 `Xoauth.md`。
