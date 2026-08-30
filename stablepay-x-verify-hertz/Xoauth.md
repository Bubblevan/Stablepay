你正在修改一个 CloudWeGo Hertz 后端项目，为 StablePay 的 X 验证链路接入真实的 X OAuth 2.0。

目标：
1. 用 X OAuth 2.0 Authorization Code Flow with PKCE 接入用户登录
2. 在 verify 页面完成 “连接 X 账号 -> 发验证推文 -> 粘贴推文链接 -> 验证并绑定”
3. 保留现有的 POST /verify-twitter 路由，但把内部逻辑从 mock tweet store 改成真实调用 X API
4. 先不实现 tweet.write；发推仍然由前端跳转到 X 发帖页完成
5. 支持本地开发，使用 .env 配置 X_CLIENT_ID / X_CLIENT_SECRET / X_REDIRECT_URI

实现要求：

一、增加配置
新增配置项：
- X_CLIENT_ID
- X_CLIENT_SECRET
- X_REDIRECT_URI
- X_OAUTH_SCOPES，默认 "tweet.read users.read offline.access"
- X_AUTHORIZE_URL，默认 "https://x.com/i/oauth2/authorize"
- X_TOKEN_URL，默认 "https://api.x.com/2/oauth2/token"
- X_API_BASE_URL，默认 "https://api.x.com/2"
- X_VERIFY_TWEET_TEMPLATE，默认 "I'm verifying my StablePay DID: {DID}"
- FRONTEND_VERIFY_URL，示例 "http://127.0.0.1:3000/verify"

二、增加数据模型
新增三张表或等价持久化结构：

1. x_oauth_sessions
字段：
- id
- did
- state
- code_verifier
- redirect_uri
- status (pending/authorized/expired)
- created_at
- expires_at

2. x_accounts
字段：
- x_user_id (unique)
- username
- name
- protected
- access_token (加密存储)
- refresh_token (加密存储, nullable)
- token_expires_at
- scope
- created_at
- updated_at

3. x_bindings
字段：
- did (unique)
- x_user_id (unique)
- username
- tweet_id
- tweet_url
- reward_tx
- verified_at
- created_at

要求：
- x_user_id 唯一
- did 唯一
- 提供 repository 层接口
- token 必须通过统一的 crypto helper 加密后存储，不允许明文写库

三、增加后端接口

1. GET /api/v1/x/oauth/start?did={did}
行为：
- 校验 did 存在
- 生成 state 和 code_verifier
- 计算 code_challenge (S256)
- 保存 x_oauth_session
- 返回：
{
  "authorize_url": "...",
  "state": "...",
  "expires_in": 300
}

authorize_url 形如：
https://x.com/i/oauth2/authorize
  ?response_type=code
  &client_id=...
  &redirect_uri=...
  &scope=tweet.read%20users.read%20offline.access
  &state=...
  &code_challenge=...
  &code_challenge_method=S256

2. GET /api/v1/x/oauth/callback
行为：
- 读取 code 和 state
- 校验 state 对应 session，且未过期
- 用 code + code_verifier 调 X token endpoint
- 对 confidential client 使用 Basic Auth(client_id:client_secret 的 base64)
- 保存 access_token / refresh_token / expires_in / scope
- 调 GET /2/users/me 获取 x_user_id / username / name / protected
- 将 session 状态改为 authorized
- 跳回前端 verify 页面，例如：
  FRONTEND_VERIFY_URL?did=...&oauth=success&username=alice
失败时跳回：
  FRONTEND_VERIFY_URL?did=...&oauth=failed&reason=...

3. GET /api/v1/x/oauth/status?did={did}
行为：
- 返回当前 did 是否已经完成 OAuth 连接
- 若已连接，返回 x_user_id / username / protected / token_expired

返回示例：
{
  "connected": true,
  "username": "alice",
  "x_user_id": "123456",
  "protected": false,
  "token_expired": false
}

4. POST /api/v1/verify-twitter
请求体：
{
  "did": "did:solana:xxxxx",
  "tweet_url": "https://x.com/username/status/123456789"
}

行为：
- 校验 did 存在
- 校验 did 已完成 OAuth 连接
- 解析 tweet_url 中的 tweet_id
- 调 GET /2/tweets/{id}
  query:
    tweet.fields=author_id,created_at,text
    expansions=author_id
    user.fields=id,username,protected
- 校验：
  a. tweet 存在
  b. tweet.author_id == 当前 OAuth 登录用户的 x_user_id
  c. tweet 文本包含 did
  d. tweet 文本包含最小模板前缀 "I'm verifying my StablePay DID:"
  e. 作者账号 protected != true
  f. 当前 did 尚未绑定
  g. 当前 x_user_id 尚未绑定其他 did
- 通过后：
  - 写 x_bindings
  - 调用现有 reward service 发 1 USDC
  - 返回：
{
  "success": true,
  "twitter_handle": "@alice",
  "reward_tx": "xxx",
  "message": "Verification successful, 1 USDC sent"
}

错误码建议：
- x_oauth_not_connected
- invalid_tweet_url
- tweet_not_found
- tweet_author_mismatch
- did_not_in_tweet
- twitter_account_protected
- did_already_bound
- twitter_already_bound
- reward_failed

5. GET /api/v1/verify?did={did}
行为：
- 若 x_bindings 存在，返回 verified + twitter_handle + reward_tx
- 保持与当前前端兼容

四、X API 客户端实现
新增 xclient 包：
- BuildAuthorizeURL(...)
- ExchangeCodeForToken(...)
- RefreshAccessToken(...)
- GetMyUser(...)
- GetTweetByID(...)

要求：
- 所有 X API 调用统一封装
- 统一 request id / timeout / retry（只对网络错误和 429/5xx 做有限重试）
- token endpoint 使用 application/x-www-form-urlencoded
- 对 confidential client 使用 Basic Auth
- 封装 X API 错误到内部错误结构

五、PKCE 工具
新增 pkce 工具包：
- GenerateCodeVerifier() string
- GenerateCodeChallengeS256(verifier string) string
- GenerateState() string

要求：
- verifier 长度符合 OAuth PKCE 常见要求
- challenge 使用 S256
- state 具备足够随机性

六、前端 verify 页面改造（如果仓库内已有 React verify 页面）
页面需求：
- 读取 query 中的 did
- “Connect X” 按钮：调用 /api/v1/x/oauth/start，然后 window.location 跳 authorize_url
- OAuth 成功后展示“已连接 @username”
- “Post Verification Tweet” 按钮：
  打开 x.com 发帖页，并预填：
  "I'm verifying my StablePay DID: {did}

  Join me on @StablePay to enable AI Agent payments on Solana! ?

  #StablePay #Solana #AIAgent"
- 文本框输入 tweet_url
- “Verify & Claim” 调 POST /api/v1/verify-twitter
- 成功后显示 reward_tx 和已绑定账号
- 页面增加轮询 /api/v1/x/oauth/status?did=... 或在 callback 回跳后直接读取状态

七、必须补的测试
1. OAuth start 成功生成 authorize_url
2. callback 正常换 token 并保存 x account
3. callback state 不匹配拒绝
4. verify-twitter 在未 OAuth 时拒绝
5. verify-twitter tweet 作者不匹配拒绝
6. verify-twitter tweet 文本不含 did 拒绝
7. verify-twitter did 已绑定拒绝
8. verify-twitter x_user_id 已绑定拒绝
9. verify-twitter 成功写 binding 并调用 reward

八、必须提供的开发文档
请生成：
- docs/x-oauth-local-dev.md
内容包括：
1. 如何在 X Developer Console 创建 Web App
2. 需要配置的 callback URL
3. 需要的 env
4. 本地启动命令
5. 手工验证流程
6. 常见报错排查（state mismatch / redirect_uri mismatch / code expired / token expired）

九、不要做的事
- 不要先接 tweet.write
- 不要把 access_token 明文写日志
- 不要把 client_secret 下发到前端
- 不要只用 username 做唯一键，必须以 x_user_id 为主
- 不要移除现有 /verify-twitter 路由，只替换内部逻辑

十、验收标准
- 本地从 verify 页面点 Connect X 能跳到 X 授权页
- 授权后能回到本地页面并显示已连接的 @username
- 粘贴本人发的验证推文链接后验证成功
- 同一个 X 账号不能绑定两个 DID
- 同一个 DID 不能绑定两个 X 账号
- 能发奖励并返回 reward_tx