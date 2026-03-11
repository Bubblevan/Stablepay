# 命名规范（一期）

## 1. 总体原则

- 仓库名 / 服务名 / 文件名统一使用 **小写字母 + `-`**（kebab-case），例如：`did-service`、`payment-service`。
- 文档中出现的服务名称必须与服务名一致（kebab-case）。
- IDL 字段命名使用 **lower_snake_case**（Thrift 通用、可读性好、对多语言友好）。
- IDL 结构体/枚举/服务/方法命名使用 **PascalCase**（Thrift/Go 生态常见约定）。

## 2. Thrift 命名与 Go 包名映射

Thrift 的 `namespace` 不建议包含 `-`。本仓库采用如下映射：

- 服务名（kebab-case）：`did-service`
- Thrift 文件名：`idl/did-service.thrift`
- Thrift `namespace go`（snake_case）：`stablepay.did_service`

说明：

- `idl/*.thrift` 的文件名遵循服务名（kebab-case）。
- `namespace go` 使用 `stablepay.<service_snake_case>`，便于在 Go 中生成稳定包名。

## 3. 外部 HTTP API 命名

- canonical API：`/api/v1/*`
- 兼容短链接（skill.md 场景）：`/pay`、`/verify`（由网关映射到 canonical API）
- Query 参数使用 lower_snake_case：`agent_did`、`skill_did`、`tx_id`、`limit`、`offset`
- 统一响应格式：`code` / `message` / `data`（详见 `docs/external-api-contract.md`）

## 4. 事件命名

- 事件类型使用 `.` 分隔的 lower_snake_case：`payment.success`、`payment.failed`
- 一期约定：RocketMQ `topic = event_type`（tag 后续再细化）
- 事件体字段使用 lower_snake_case（JSON）

