# 模块说明

## `cmd/server`

程序入口，负责配置加载、日志初始化、应用启动。

## `internal/app`

应用装配层，初始化 Hertz、中间件链、下游客户端和路由。

## `internal/interfaces/http`

HTTP 适配层：请求解析、响应封装、统一错误映射、路由绑定。

## `internal/interfaces/http/middleware`

中间件层：

- `Recovery`
- `RequestMeta`
- `ReadinessGuard`
- `AuthExtract`
- `AuthVerify`
- `ReplayProtection`
- `RateLimit`
- `AccessLog`

## `internal/application`

网关应用服务，负责按路由名转发到下游服务客户端。

## `internal/domain`

领域模型：统一响应、错误定义、路由策略、上下文 key。

## `internal/infrastructure`

- `config`：配置加载
- `auth`：签名规范、时间解析、nonce 存储
- `ratelimit`：Redis/内存限流
- `clients`：下游客户端（当前为 mock 实现）
- `resilience`：熔断与重试执行器
- `observability`：日志能力
