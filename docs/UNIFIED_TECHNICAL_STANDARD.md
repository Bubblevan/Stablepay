# StablePay 统一技术标准（TRD）

> 状态：第一阶段基线（Draft / Engineering Baseline）  
> 日期：2026-08-30  
> 适用范围：`api-gateway`、`did-service`、`payment-service`、`query-service`、`verification-service`、`blockchain-adapter`

## 1. 文档定位

本文件是 StablePay 的统一技术标准（Technical Requirements/Design Document，TRD），回答“系统必须怎样实现、怎样交付、怎样验收”。

现有的 [`agent-commerce-runtime-design.md`](./agent-commerce-runtime-design.md) 继续作为产品/业务设计参考，回答“系统要解决什么问题、有哪些业务能力”。本 TRD 不替代它，也不扩展业务需求。

本阶段的目标不是一次性重写六个服务，而是先固定共同规则，再按规则渐进迁移。任何服务的局部实现都不能突破本文件定义的契约、边界和质量门禁。

## 2. 由果推因：先定义必须得到的结果

### 2.1 第一阶段的最终结果

第一阶段完成后，六个微服务必须具备以下可验证结果：

1. **契约唯一**：Thrift IDL 只有一个权威来源；从 IDL 生成的客户端、服务端代码可以重复生成，且结果确定。
2. **通信统一**：外部请求只进入 `api-gateway`；服务间同步调用统一使用 Kitex RPC；事件统一使用 RocketMQ。
3. **代码入口唯一**：每个服务只有一个正式启动入口 `cmd/server/main.go`，不再同时维护旧的根目录 `main.go`、旧 Handler 和另一套运行模式。
4. **模块可独立交付**：每个服务拥有明确的 Go module、配置、构建、测试和容器启动方式，不依赖另一个服务的业务源码包。
5. **运行状态可判断**：每个服务都有一致的启动日志、健康检查、就绪检查、优雅退出和依赖失败行为。
6. **资金安全可审计**：支付、转账、链上提交等操作必须有幂等键、状态机和可追踪事件；重试不能导致重复扣款或重复转账。
7. **问题可定位**：HTTP、RPC、消息和数据库操作能够关联 `request_id`/`trace_id`，日志格式和错误模型统一。
8. **质量门禁可自动执行**：生成代码漂移、协议版本混用、未通过测试、Docker 探针错误和敏感配置入库都能在 CI 阶段被阻断。

### 2.2 第一阶段不承诺的事项

- 不在本阶段重做支付业务规则、DID 业务模型或前端产品交互。
- 不在本阶段引入服务网格、跨区域部署或新的消息中间件。
- 不在本阶段把 Thrift 全量切换为 Protobuf；如未来切换，必须另行提交架构决策记录。
- 不因为“统一”而强行共享领域模型；领域模型必须归属业务服务，跨服务只共享契约和基础设施约定。

## 3. 当前问题归因

当前六个服务并非单纯“代码风格不同”，而是四类会持续制造故障的系统性差异：

| 类别 | 当前表现 | 造成的结果 | 统一因果规则 |
|---|---|---|---|
| 契约漂移 | `stablepayai-idl`、服务内 `idl`、嵌套 IDL 副本并存；区块链生成接口与当前 IDL 方法数不一致 | 客户端、服务端、文档无法确认谁是准确信息 | IDL 单一来源，生成代码禁止手改 |
| 模块漂移 | 各服务 module path 不一致，部分服务用 `replace` 直接依赖其他服务源码 | 本地能编译，独立构建和发布不稳定 | 统一 module path，服务之间只依赖版本化契约 |
| 运行时漂移 | 有的服务用 Hertz，有的用标准库 HTTP；有根目录旧入口和 `cmd` 新入口；端口、探针不一致 | Docker、Kubernetes、本地运行行为不一致 | 一个正式入口，一套配置和生命周期规范 |
| 质量门禁漂移 | 生成工具版本不一致，vendor 不完整，部分测试/脚本包本身无法统一执行 | 代码合并后才发现构建或生成问题 | 将生成、测试、构建、探针加入统一 CI 门禁 |

当前审计依据和迁移输入包括：

- [`stablepayai-idl/README.md`](../stablepayai-idl/README.md)
- [`stablepayai-idl/docs/internal-rpc-contract.md`](../stablepayai-idl/docs/internal-rpc-contract.md)
- [`stablepayai-idl/docs/versioning-policy.md`](../stablepayai-idl/docs/versioning-policy.md)
- [`stablepayai-idl/docs/naming-conventions.md`](../stablepayai-idl/docs/naming-conventions.md)

## 4. 技术总决策

### 4.1 技术栈决策

| 层次 | 统一选择 | 使用范围 | 禁止或限制 |
|---|---|---|---|
| Go HTTP | CloudWeGo Hertz | 仅 `api-gateway` 对外提供业务 HTTP API；服务的管理端口可按需使用 | 不再新增标准库 `net/http` 业务入口 |
| 服务间 RPC | CloudWeGo Kitex | 六个服务之间的同步调用 | 不用 HTTP 作为服务间主要业务协议 |
| RPC 契约 | Apache Thrift IDL + thriftgo | Kitex 服务、方法、请求、响应和错误模型 | 不在 Handler 中手写跨服务协议 |
| 异步事件 | RocketMQ | 支付、验证、查询等最终一致性流程 | 不把事件当作同步 RPC 的替代品 |
| 主数据库 | MySQL | 各服务自己的业务数据 | 禁止跨服务直接读写对方数据库 |
| 缓存/短期状态 | Redis | 幂等键、锁、缓存、短期 nonce 等 | 不把 Redis 当作资金事实来源 |
| 链交互 | `blockchain-adapter` | 链 RPC、交易构造/提交/状态查询 | 支付规则和账户归属不放在 adapter |
| 可观测性 | 结构化日志 + OpenTelemetry trace/metric | HTTP、RPC、消息、数据库边界 | 禁止仅打印无法关联请求的自由文本 |
| 工具链 | Go 1.26.5 基线；Kitex/thriftgo 版本集中锁定 | 本地、CI、容器使用同一基线 | 禁止脚本使用 `@latest` |

CloudWeGo 是生态层，Kitex 是 RPC 框架，Hertz 是 HTTP 框架，thriftgo 是 Thrift IDL 编译器；四者不是互相替代的方案，而是分别处于不同技术层。[Kitex 官方仓库](https://github.com/cloudwego/kitex)、[Hertz 官方仓库](https://github.com/cloudwego/hertz)、[thriftgo 官方仓库](https://github.com/cloudwego/thriftgo)

```
StablePay/
├── stablepayai-idl/                    # 唯一契约源
│   ├── idl/
│   ├── openapi/
│   ├── events/
│   ├── docs/
│   └── scripts/
│
├── stablepay-common/                   # 非通信类公共基础库
│   ├── log/
│   ├── tracing/
│   ├── id_generator/
│   └── money/
│
├── api-gateway/
├── did-service/
├── payment-service/
├── query-service/
├── verification-service/
└── blockchain-adapter/


<service>/
├── cmd/
│   └── server/
│       └── main.go                     # 唯一正式启动入口
│
├── internal/
│   ├── app/
│   │   ├── bootstrap.go                # 依赖组装
│   │   └── lifecycle.go                # 启停、优雅退出
│   │
│   ├── application/                   # 用例编排
│   │   ├── dto/
│   │   └── service/
│   │
│   ├── domain/                        # 领域模型和业务规则
│   │   ├── entity/
│   │   ├── valueobject/
│   │   ├── service/
│   │   └── repository.go               # 仓储接口
│   │
│   ├── adapter/
│   │   ├── rpc/                       # Kitex Server/Client 适配器
│   │   └── http/                      # Gateway 公共 API或管理端口
│   │
│   └── infrastructure/
│       ├── config/
│       ├── persistence/                # MySQL、Repository 实现
│       ├── cache/                      # Redis
│       ├── messaging/                  # RocketMQ
│       ├── external/                   # 外部 SDK、链节点
│       └── observability/              # 日志、Trace、Metrics
│
├── kitex_gen/                         # IDL 生成代码，禁止手改
│   └── stablepay/
│
├── config/
│   ├── config.example.yaml
│   ├── config.local.yaml
│   └── config.docker.yaml
│
├── migrations/                        # 本服务自己的数据库迁移
├── scripts/                           # 启动、检查、测试辅助脚本
├── tests/                             # 集成测试、契约测试
│
├── Dockerfile
├── Makefile
├── README.md
├── go.mod
└── go.sum
```

其中：
```
cmd/server/main.go
    只负责启动，不写业务

internal/application
    负责业务用例编排

internal/domain
    负责业务规则，不依赖 Kitex、MySQL、Redis

internal/adapter/rpc
    负责 Thrift DTO 与领域对象转换

internal/infrastructure
    负责数据库、缓存、消息队列和外部 SDK

kitex_gen
    只允许重新生成，禁止手工修改
```
### 4.2 统一协议边界

```
外部客户端
    │ HTTPS / JSON
    ▼
api-gateway（Hertz）
    │ Kitex RPC / Thrift
    ├── did-service
    ├── payment-service ── Kitex ── blockchain-adapter
    ├── verification-service
    └── query-service

payment-service / verification-service / query-service
    └── RocketMQ 事件（最终一致性、可重放、消费者幂等）
```

强制规则：

- 客户端不能直连内部服务端口。
- Gateway 负责认证、限流、请求校验、协议转换和对外错误映射；不持有业务事实，不直接操作业务数据库。
- 服务间的业务请求统一走 Kitex；服务端不得同时维护一套“HTTP 业务入口”和一套“Kitex 业务入口”来实现同一个能力。
- 管理、探针、调试 HTTP 与业务 API 分离；如服务只有 Kitex，则使用 TCP 探针或独立管理端口，不伪装成业务 HTTP 服务。

## 5. 契约与代码生成标准

### 5.1 单一来源

`D:\MyLab\projects\Stablepay\stablepayai-idl` 是唯一契约源：

```text
stablepayai-idl/
├── idl/                  # 唯一 Thrift 源文件
├── openapi/              # 对外 API 描述；只描述 Gateway 公共接口
├── events/               # 事件 schema 和示例
├── docs/                 # 契约说明、兼容性和命名规则
├── scripts/              # 统一生成、校验脚本
└── tools/                # 生成工具版本和校验配置
```

服务目录下的 `idl/`、嵌套的 `stablepayai-idl/` 以及手工复制的 Thrift 文件均不再作为来源。迁移期间可以保留只读副本，但必须在 CI 中禁止修改，完成迁移后删除。

### 5.2 生成物策略

第一阶段采用“契约集中、生成物可追溯”的模式：

- Thrift 源码只在 `stablepayai-idl/idl` 修改。
- Kitex 生成的 Go 代码放在各服务自己的 `kitex_gen/`，以保证服务可以独立构建。
- 每个生成目录必须带生成工具版本和源 IDL commit 信息。
- 生成代码不可手工修改；业务逻辑写在 `internal/adapter/rpc` 或 `internal/application`。
- `stablepay-common` 只放非线上的基础类型、日志、配置和工具；不得作为跨服务 wire type 的第二来源。
- 服务之间不导入对方的 `domain`、`handler`、`repository` 或业务包；跨服务依赖只能是 RPC 契约、事件契约或稳定的基础设施客户端。

### 5.3 Thrift 兼容规则

- 已发布字段的 `field id` 永不复用。
- 新增字段必须可选或有向后兼容的默认行为。
- 不直接修改已有字段类型、语义和枚举含义；破坏性变化使用新方法或新版本命名空间。
- RPC 错误必须区分：传输错误、依赖错误、业务拒绝、参数错误和未知内部错误。
- 方法命名使用动词加领域对象，例如 `CreatePayment`、`GetPayment`、`SubmitSignedTransaction`。
- 所有产生副作用的方法必须在契约中明确幂等键或幂等规则。
- 区块链契约第一阶段冻结为 5 个方法：`TransferStableCoin`、`GetBalance`、`GetTxStatus`、`BuildUnsignedTransaction`、`SubmitSignedTransaction`。前 3 个支持服务端执行/查询路径，后 2 个支持客户端签名的两阶段交易路径；同一笔支付不能混用两条执行路径。

### 5.4 事件标准

RocketMQ 事件采用统一包络：

```json
{
  "event_id": "uuid",
  "event_type": "payment.succeeded",
  "schema_version": 1,
  "producer": "payment-service",
  "occurred_at": "2026-08-30T00:00:00Z",
  "trace_id": "trace-id",
  "aggregate_id": "payment-id",
  "payload": {}
}
```

要求：

- 投递语义按至少一次处理；消费者必须以 `event_id` 或业务聚合键实现幂等。
- 生产数据库写入与事件发布使用 outbox 或等价的可靠发布机制。
- 事件 payload 只能追加兼容字段；消费者必须忽略未知字段。
- 事件名称、topic、tag、schema version 必须在 `stablepayai-idl/events` 登记。

## 6. 六个服务的职责边界

| 服务 | 必须负责 | 不得负责 | 对外暴露 |
|---|---|---|---|
| `api-gateway` | HTTP 路由、鉴权、限流、参数校验、RPC 编排、错误映射 | 支付事实、链上调用、跨库查询 | Hertz `/api/v1/*`、管理探针 |
| `did-service` | DID 注册、解析、密钥/签名策略、nonce 与身份相关事实 | 支付状态、链上交易提交 | Kitex；管理探针 |
| `payment-service` | 支付状态机、幂等、金额校验、支付订单、outbox | 直接实现链 RPC、验证平台详情 | Kitex；管理探针 |
| `blockchain-adapter` | 链账户、构造交易、签名交易校验、提交、确认和余额/状态查询 | 决定业务是否应付款、拥有支付订单 | Kitex；管理探针 |
| `verification-service` | 验证支付结果、生成 entitlement/凭证、消费相关事件 | 直接修改支付事实或查询支付库 | Kitex；管理探针 |
| `query-service` | 面向查询的读模型、索引、分页和聚合 | 作为任意服务的主写库 | Kitex；管理探针 |

每个服务拥有自己的数据库 schema 和迁移；“共享数据库连接”不等于“共享数据库事实”。查询服务需要的数据通过事件或明确的只读同步机制获得，不能绕过服务边界直接读取其他服务表。

## 7. 统一服务工程模板

每个服务最终统一为以下结构；名称可按领域增加子目录，但职责不能倒置：

```text
<service>/
├── cmd/server/main.go             # 唯一正式入口
├── internal/app/                  # 启动、依赖注入、生命周期
├── internal/application/          # 用例编排、事务和幂等
├── internal/domain/               # 实体、值对象、领域规则
├── internal/adapter/rpc/          # Kitex server/client 适配器
├── internal/adapter/http/         # 仅 Gateway 或管理端口需要
├── internal/infrastructure/       # DB、Redis、MQ、链 SDK、配置
├── kitex_gen/                     # 由统一 IDL 生成，禁止手改
├── config/config.example.yaml     # 非敏感示例
├── migrations/                    # 本服务 schema 迁移
├── Dockerfile
├── Makefile
└── README.md                      # 启动、端口、依赖、探针、测试
```

### 7.1 启动与配置

- `cmd/server/main.go` 只做配置加载、依赖组装、服务启动和退出信号处理。
- 依赖通过构造函数注入；禁止生产路径使用全局 `DB`、全局 RPC client、全局 MQ client。
- 配置优先级统一为：命令行参数 > 环境变量 > 配置文件 > 安全的默认值。
- 服务地址、端口、超时、重试、数据库、Redis、RocketMQ 和链节点全部可配置；禁止在 client 构造函数内硬编码生产地址。
- 密钥、数据库密码、RPC token 不进入 Git、镜像层或日志。
- 启动时检查必需配置；可选依赖允许降级时必须明确记录并暴露在 readiness 状态中。

### 7.2 健康检查与生命周期

每个服务必须定义：

- `liveness`：进程和核心循环仍在运行。
- `readiness`：服务已经能够接受请求，必需依赖已经可用。
- `shutdown`：停止接收新请求，等待在途请求和消息处理，在超时后退出。

Kitex-only 服务使用 TCP 探针或独立管理 HTTP 端口；Hertz 服务提供 `/healthz` 和 `/readyz`。Docker Compose、Kubernetes 和本地 README 中的端口必须来自同一份配置，不允许出现“探针访问 HTTP、实际进程只监听 Kitex”的情况。

## 8. 关键业务安全标准

### 8.1 支付与转账

- 每个支付意图必须有业务幂等键；同一幂等键只能产生一个业务结果。
- 状态只能按显式状态机迁移，禁止通过重复调用覆盖终态。
- 对链上不可逆操作，网络重试必须先查询交易状态，不能盲目再次提交。
- 支付记录、链上交易 hash、事件 ID、trace ID 必须能够互相追踪。
- 失败补偿通过明确的重试/人工介入状态完成，不用“重新创建一笔支付”代替补偿。

### 8.2 区块链 adapter

- 构造交易、签名交易校验、提交和确认是独立用例；每一步返回可审计结果。
- 校验 fee payer、付款方、收款方、金额、token mint、网络和近期区块/nonce；不能以空校验或占位校验放行生产交易。
- 私钥只允许来自受控密钥系统或明确的签名服务；adapter 不在日志中输出私钥、完整交易敏感材料或认证信息。
- 链 SDK 的具体类型不得泄漏到 RPC 契约；边界层负责转换。

## 9. 版本、模块和依赖管理

### 9.1 Go module

服务 module path 统一为：

```text
github.com/stablepay/api-gateway
github.com/stablepay/did-service
github.com/stablepay/payment-service
github.com/stablepay/query-service
github.com/stablepay/verification-service
github.com/stablepay/blockchain-adapter
```

开发环境可以使用根目录 `go.work` 联调，但 CI 和容器构建必须使用 `GOWORK=off` 验证每个服务的独立性。提交的服务 `go.mod` 不应通过 `replace` 依赖另一个服务的业务源码；本地联调使用 `go.work` 或版本化的契约模块解决。

### 9.2 工具版本

工具版本必须集中定义并被本地、CI、容器共同使用：

```text
Go       1.26.5（第一阶段基线）
Kitex    全仓统一一个版本
thriftgo 全仓统一一个版本
```

Kitex 与 thriftgo 的具体升级必须成对评审。版本升级后必须重新生成全部受影响的 `kitex_gen`，不能出现“go.mod 是一个版本、生成头信息是另一个版本”的状态。

### 9.3 vendor 策略

第一阶段统一采用 Go module 模式作为构建基线。若因离线构建必须使用 vendor，则六个服务必须全部通过同一工具链重新生成 vendor，并在 CI 中执行完整性校验；不接受只有部分服务有 vendor、且 vendor 内容不完整的混合状态。

## 10. 统一命令与 CI 门禁

根目录或 `stablepayai-idl` 必须提供等价的统一命令；PowerShell 和 Unix 版本可以不同，但语义必须相同：

```text
make generate          # 从唯一 IDL 生成全部代码
make verify-generated   # 临时目录生成并检查 git diff 为空
make lint               # 格式、静态检查、依赖安全检查
make test               # 六个服务的单元/集成测试
make build              # GOWORK=off 独立构建六个服务
make compose-config     # 校验 Compose 配置、端口和环境变量
```

CI 至少阻断以下情况：

- IDL 变更但生成物未更新。
- 生成物不是由规定版本生成。
- 服务 module path 不符合规范，或出现跨服务业务源码依赖。
- `go test`、独立构建或 lint 失败。
- 服务入口重复、Docker/Kubernetes 探针与实际监听协议/端口不一致。
- 生产配置中出现明文密钥、密码、token 或私钥。
- 涉及资金的代码缺少幂等测试、重复调用测试和失败恢复测试。

## 11. 第一阶段实施顺序

### Phase 1-A：冻结契约

1. 以 `stablepayai-idl/idl` 为唯一源。
2. 冻结六个服务名、Kitex service name、RPC 端口和 topic 命名。
3. 已将 `blockchain-adapter` 冻结为 5 个方法，形成评审基线：3 个现有执行/查询方法，加上 2 个已由活动实现支持的两阶段签名方法。
4. 补齐公共错误、分页、幂等键、事件包络等契约。

### Phase 1-B：统一工程基线

1. 统一六个服务 module path。
2. 锁定 Go、Kitex、thriftgo 版本。
3. 重写统一生成脚本，使其一次生成六个服务所需代码。
4. 删除或标记失效的服务内 IDL、旧生成目录和旧入口。

### Phase 1-C：统一运行时

1. 每个服务只保留 `cmd/server/main.go` 作为正式入口。
2. 统一配置加载、依赖注入、日志、trace、超时和优雅退出。
3. 统一 liveness/readiness；修正 Compose、Dockerfile、Kubernetes 探针。
4. 清除生产路径中的硬编码地址和全局依赖对象。

### Phase 1-D：统一通信路径

1. Gateway 到 Payment 从 HTTP 切换为 Kitex RPC。
2. 其他 Gateway 下游调用全部使用同一套生成客户端和超时/错误映射。
3. 支付、验证、查询的事件采用统一 envelope，并增加消费者幂等。
4. 保留 HTTP 仅作为 Gateway 公共 API 和明确的管理端口。

### Phase 1-E：质量门禁

1. 接入 `generate`、`verify-generated`、`test`、`build`、探针和敏感配置检查。
2. 修复测试脚本包冲突、mock 契约落后和不完整 vendor 等基线问题。
3. 以 CI 全绿作为第一阶段结束条件，而不是以“本地某个服务能启动”作为结束条件。

## 12. 第一阶段验收标准（Definition of Done）

以下条件全部满足，才算第一阶段完成：

- [ ] 六个服务的正式 module path 已统一。
- [ ] 所有 Thrift 源文件均来自 `stablepayai-idl/idl`，服务内副本已删除或被 CI 禁止使用。
- [x] 区块链 adapter 的 IDL、生成接口和活动实现均冻结为同一组 5 个方法。
- [ ] 生成工具版本已锁定；重复执行生成不会产生 diff。
- [ ] 每个服务只有一个正式启动入口，且 README、Dockerfile、Compose、Kubernetes 使用同一入口。
- [ ] Gateway 对外使用 Hertz；服务间业务调用使用 Kitex/Thrift；没有新增服务间 HTTP 业务链路。
- [ ] 六个服务可以在 `GOWORK=off` 下独立构建。
- [ ] 六个服务的测试、lint、生成校验均纳入 CI。
- [ ] Docker/Kubernetes 健康检查与实际监听协议、端口一致。
- [ ] 支付和链上提交具备幂等、状态机、重复调用和失败恢复测试。
- [ ] 配置、日志、trace、超时、重试、优雅退出和敏感信息处理遵循同一规范。
- [ ] 架构变更和未完成迁移项登记在 ADR/迁移清单中，不依靠口头约定。

## 13. 迁移期间的临时规则

在所有服务迁移完成前，允许旧代码暂时存在，但必须满足：

- 旧入口、旧 IDL、旧生成物必须在文件头或 README 中标记 `DEPRECATED`，并写明删除条件。
- 新功能只能写入目标目录结构，不得继续扩展旧根目录 Handler 或旧启动入口。
- 新增跨服务调用必须先更新 canonical IDL，再生成客户端，禁止手写请求结构。
- 如果旧实现与本标准冲突，优先保证资金安全和数据一致性，再做协议切换。
- 每次迁移提交只解决一类问题：契约、模块、运行时、通信或质量门禁，避免把业务重构和基础设施重构混在一个不可回滚的提交中。

## 14. 结论

StablePay 的统一方案确定为：

> **CloudWeGo 生态 + Hertz 对外 HTTP + Kitex 内部 RPC + Thrift IDL 契约 + RocketMQ 事件 + 服务独立数据库。**

其中最重要的不是框架名称，而是四条不可妥协的工程约束：**契约单一来源、服务边界清晰、运行入口唯一、生成与质量门禁自动化**。这四条先落地，现有六个服务可以渐进收敛；如果这四条不落地，继续增加功能只会继续放大三套实现之间的差异。
