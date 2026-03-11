# stablepayai-idl

## 1. 仓库定位

`stablepayai-idl` 用于沉淀 StablePay DEMO **一期（最小可用、优先跑通链路）**的接口契约与 IDL，供各微服务在开发前基于契约进行代码生成与实现。

本仓库只包含：

- 对外 HTTP API 契约（API Gateway 对外暴露）
- 内部 RPC 契约（Kitex/Thrift，服务间调用优先稳定）
- 事件消息契约（RocketMQ，JSON payload，至少一次投递语义）
- Skill 侧接入契约（官方 `stablepay-skill` 与开发者 `skill.md` 插入规范）
- IDL 文件、OpenAPI 草稿、示例、以及生成脚本

本仓库不包含任何微服务业务实现代码（包括但不限于 handler、domain、DAO、数据库脚本、部署脚本）。

## 2. 目录结构

```
stablepayai-idl/
  docs/                       # 正式契约文档（中文）
  idl/                        # Thrift IDL（thriftgo/kitex 生成入口）
  openapi/                    # 对外 HTTP API OpenAPI 草稿
  examples/                   # 示例：HTTP / 事件
  scripts/                    # 生成脚本
  Makefile
```

## 3. 四类契约说明

- **external（对外 HTTP）**：面向 API Gateway 暴露的 RESTful `/api/v1/*` 为 canonical API；同时兼容 skill.md 嵌入场景的短链接 `/pay`、`/verify`（由网关做协议兼容映射）。
- **internal（内部 RPC）**：面向微服务之间的 Kitex/Thrift 调用，优先保证稳定、字段语义清晰、可向后兼容演进。
- **event（事件）**：面向 RocketMQ 的异步事件体契约，JSON payload，至少一次投递语义，消费者幂等去重。
- **skill（skill 侧）**：官方 `stablepay-skill` 的职责边界、skill definition/execute 的最小契约，以及开发者在 `skill.md` 中插入支付区块的规范。

## 4. 与 stablepay-common 的关系

后续各微服务开发过程中**需要复用**公共包 `stablepay/stablepay-common`。本仓库不复制 common 的实现代码；文档中仅给出建议复用边界。

由于当前无法直接读取 `stablepay-common` 仓库内容，下列内容在 `stablepay-common` 中是否已存在属于**待对齐项**：

- 统一错误码定义与映射
- 统一响应结构（HTTP/RPC）
- trace/request_id 透传结构与中间件
- DID 签名与 API Key 鉴权的公共解析/校验工具

在对齐完成前，本仓库在 `idl/common.thrift` 中提供**最小自洽**的通用类型与错误码枚举，便于先跑通一期链路。

## 5. Thrift / OpenAPI 的用途

- `idl/*.thrift`：内部 RPC 契约的唯一来源（source of truth），供 `thriftgo` / `kitex` 生成代码。
- `openapi/stablepay-public.yaml`：对外 HTTP API 的 OpenAPI 草稿，用于网关配置、SDK 生成或联调对齐。

## 6. 版本管理与变更约定

版本策略见 `docs/versioning-policy.md`：

- 仓库版本建议使用 **SemVer**（例如 `v0.1.0`），并使用 Git Tag 发布。
- Thrift / 事件 / OpenAPI 的变更需要遵循向后兼容原则；破坏性变更需升主版本并提供迁移说明。

## 7. 各微服务如何引用本仓库

推荐方式（示例，按你们实际仓库管理策略调整）：

1. 将 `stablepayai-idl` 作为子模块或以依赖方式拉取到各微服务仓库（只读）。
2. 在微服务仓库中基于 `idl/*.thrift` 进行代码生成：
   - Kitex（示例命令，需在服务仓库中执行，并替换 `-module` 为实际 Go module）：
     - `kitex -module <your-module> -service did-service -thrift <path-to>/stablepayai-idl/idl/did-service.thrift`
   - Thriftgo（示例命令）：
     - `thriftgo -g go -o <out-dir> <path-to>/stablepayai-idl/idl/did-service.thrift`
3. 生成代码不提交到本仓库，建议在服务仓库中 `.gitignore` 生成目录。

更多生成建议见 `scripts/generate.sh` 与 `Makefile`。

