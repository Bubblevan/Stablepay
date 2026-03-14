## 1. 项目概述

### 1.1 项目背景

随着 AI Agent 生态的快速发展，去中心化身份和小额支付需求日益增长。传统中心化支付方案存在账户管理复杂、资金托管风险、跨境支付限制等问题，无法满足 AI Agent 生态的去中心化特性需求。

StablePay AI 支付服务基于 W3C did:solana 标准构建去中心化支付基础设施，采用 HTTP 402 协议实现 AI Agent 小额支付能力。系统主要服务于 ClawHub 平台的 skill 交易场景，为 AI Agent 和 skill 开发者提供安全、便捷的支付解决方案。

## 1.2 业务目标

- 支持 50+ 付费 skill 集成 StablePay，建立 AI Agent 支付生态基础设施
- 处理 500+ 笔 skill 购买交易，验证 HTTP 402 协议在 AI Agent 场景的可行性
- 实现 10,000 USDC 平台 GMV，建立可持续的商业模式
- API 响应时间 < 2 秒，支付成功率 ≥ 95%，系统可用性 ≥ 99.5%
  
## 1.3 项目范围

包含模块：

- DID 服务：基于 W3C did:solana 标准的身份管理
- 支付服务：HTTP 402 协议实现和 Solana 链上交易处理
- 验证服务：购买记录验证和防篡改机制
- 查询服务：余额、交易记录、收益统计查询
- 区块链适配器：Solana 网络交互和 Gas 费补贴
- API 网关：统一接入、认证鉴权、限流熔断
  
不包含模块（后续规划）：

- 多链支持：以太坊、Polygon 等其他区块链网络
- 企业管理：企业级 Agent 管理和权限控制
- 高级分析：支付行为分析和风控系统
  
1.4 架构原则

- 去中心化优先：基于 DID 和区块链技术，避免中心化风险
- 对话式交互：所有用户交互通过自然语言对话完成
- 服务独立：每个微服务承担单一职责，支持独立部署和扩展
- 数据隔离：每个服务管理自己的数据，避免数据耦合
- 安全可控：多层安全防护，支持降级和熔断
- 标准协议：遵循 W3C DID、HTTP 402 等开放标准
  
2. 技术选型

2.1 技术栈总览

技术分类
选型方案
版本要求
应用场景
云服务平台
阿里云（香港）
-
满足合规要求，提供低延迟服务
编程语言
Golang

1.21+
高性能、并发友好、生态完善
微服务框架
CloudWeGo
Latest
字节跳动开源，高性能微服务生态
RPC 框架
Kitex

v0.7+
高性能 RPC 通信，支持 Thrift 协议
IDL 编译器
Thriftgo
v0.3+
生成服务接口代码，类型安全
HTTP 框架
Hertz
v0.7+
高性能 HTTP 服务，API 网关和对外接口
交易数据库
MySQL
8.0+
ACID 事务保证，支付数据强一致性
配置数据库
MongoDB
6.0+
灵活 Schema，DID 和配置数据存储
缓存系统
Redis
7.0+
会话缓存、分布式锁、消息发布订阅
消息队列
RocketMQ
5.0+
异步消息处理、削峰填谷
API 网关
阿里云 API 网关
-
统一接入、认证鉴权、限流保护
容器编排
ACK Kubernetes
1.24+
服务发现、自动扩缩容、滚动更新
日志监控
阿里云 SLS
-
分布式日志收集、实时监控告警

2.2 技术选型说明

2.2.1 CloudWeGo 生态

- Kitex：高性能 RPC 框架，支持 Thrift 协议，用于微服务间内部通信
  
  - 连接池管理和负载均衡
  - 服务发现和熔断降级
  - 链路追踪和监控指标
- Hertz：高性能 HTTP 框架，用于对外 API 服务
  
  - 支持 HTTP/1.1 和 HTTP/2
  - 中间件生态丰富
  - 与 Kitex 无缝集成
- Thriftgo：IDL 编译器，生成类型安全的服务接口
  
  - 跨语言服务定义
  - 接口版本兼容性管理
  - 自动生成客户端代码
    
2.2.2 数据存储选型

- MySQL：交易数据存储，保证 ACID 特性
  
  - 支付记录、购买关系、资金流水
  - 复杂查询和事务一致性
  - 主从复制和读写分离
- MongoDB：配置和元数据存储
  
  - DID 身份信息和钱包配置
  - skill 元数据和支付模板
  - 动态 Schema 支持快速迭代
- Redis：缓存和分布式协调
  
  - API 响应缓存和会话存储
  - 分布式锁和限流计数器
  - 消息发布订阅和异步队列
    
2.2.3 基础设施选型

- 阿里云香港：满足跨境支付合规要求，提供稳定的网络连接
- ACK Kubernetes：容器编排平台，支持服务发现和弹性伸缩
- RocketMQ：异步消息处理，支持事务消息和顺序消息
  
3. 系统架构设计

3.1 整体架构

StablePay 系统采用微服务架构设计，分为接入层、业务服务层、数据层和区块链层：

┌─────────────────────────────────────────────────────────────┐
│ 客户端层                                                    │
│ ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│ │StablePay Skill│  │开发者后端服务 │  │StablePay网站 │      │
│ │(OpenClaw中)   │  │              │  │              │      │
│ └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│ 接入层                                                      │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ 阿里云 API 网关（路由、鉴权、限流、协议转换）            │ │
│ └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│ 业务服务层                                                  │
│ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│ │DID服务   │ │支付服务  │ │验证服务  │ │查询服务  │       │
│ │(Hertz)   │ │(Hertz)   │ │(Hertz)   │ │(Hertz)   │       │
│ └──────────┘ └──────────┘ └──────────┘ └──────────┘       │
│              ┌────────────────────────────────────┐         │
│              │     区块链适配器 (Kitex RPC)       │         │
│              └────────────────────────────────────┘         │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│ 数据层                                                      │
│ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│ │MySQL     │ │MongoDB   │ │Redis     │ │RocketMQ  │       │
│ │交易数据  │ │配置数据  │ │缓存      │ │消息队列  │       │
│ └──────────┘ └──────────┘ └──────────┘ └──────────┘       │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│ 区块链层                                                    │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │               Solana 主网                                │ │
│ │         (USDC/USDT SPL Token 转账)                      │ │
│ └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘

3.2 架构层次说明

3.2.1 接入层

- 职责：统一接入、协议转换、安全认证、流量控制
- 组件：阿里云 API 网关
- 关键能力：
  - 路由转发：根据请求路径和方法路由到对应微服务
  - 认证鉴权：DID 签名验证和 API Key 认证
  - 限流保护：防刷机制和 DDoS 防护
  - 协议转换：HTTP 到内部 Thrift RPC 协议转换
    
3.2.2 业务服务层

- 职责：核心业务逻辑处理
- 服务列表：
  - DID 服务：W3C did:solana 身份管理和钱包注册
  - 支付服务：HTTP 402 协议处理和支付流程管理
  - 验证服务：购买记录验证和防篡改检查
  - 查询服务：余额、交易记录、收益统计查询
  - 区块链适配器：Solana 网络交互和链上交易处理
    
3.2.3 数据层

- 职责：数据持久化、缓存和消息传递
- 组件：
  - MySQL：交易数据、购买记录、资金流水
  - MongoDB：DID 信息、钱包配置、skill 元数据
  - Redis：API 缓存、会话管理、分布式锁
  - RocketMQ：异步消息、事件通知、审计日志
    
3.3 数据流向

3.3.1 HTTP 402 支付数据流

1. 请求流向：StablePay Skill → API 网关 → 支付服务 → 区块链适配器 → Solana 网络
2. 响应流向：Solana 网络 → 区块链适配器 → 支付服务 → API 网关 → StablePay Skill
3. 异步通知：支付服务 → RocketMQ → 验证服务（更新购买记录）
  
3.3.2 查询数据流

1. 实时查询：StablePay Skill → API 网关 → 查询服务 → Redis（缓存命中）→ 返回结果
2. 缓存未命中：查询服务 → MySQL/MongoDB → Redis（更新缓存）→ 返回结果
3. 跨服务数据：查询服务 → Kitex RPC → 区块链适配器 → 链上余额查询
  
3.3.3 验证数据流

1. 开发者验证：开发者后端 → API 网关 → 验证服务 → MySQL 查询购买记录
2. 未购买处理：验证服务返回 HTTP 402 → 开发者后端 → StablePay Skill → 触发支付流程
  
4. 关键业务流程设计

4.1 核心业务场景一：HTTP 402 支付流程

业务描述：
Agent 访问付费 skill 时，通过 HTTP 402 协议完成支付验证和链上交易的完整流程。

流程步骤：

1. Agent 通过 StablePay Skill 访问付费 skill 资源
2. 支付服务检查购买记录，未购买则返回 HTTP 402 响应
3. StablePay Skill 根据本地配置决定自动购买或请求用户确认
4. StablePay Skill 使用本地私钥对支付请求进行签名
5. 支付服务验证签名合法性和钱包余额充足性
6. 区块链适配器调用 Solana 网络执行 USDC 转账
7. 支付服务记录购买关系并发送异步通知
8. 返回 HTTP 200，Agent 可正常访问 skill 资源
  
涉及服务：

- 支付服务：处理 HTTP 402 协议和支付流程控制
- DID 服务：验证 Agent 身份和签名合法性
- 区块链适配器：执行链上交易和 Gas 费补贴
- 验证服务：更新和查询购买记录
  
关键节点：

- 签名验证（同步）：确保支付请求的真实性和不可伪造性
- 链上交易（同步）：确保资金转账的原子性和最终一致性
- 记录更新（异步）：通过消息队列保证数据最终一致性
  
4.2 核心业务场景二：DID 身份注册

业务描述：
用户首次使用 StablePay 时，通过对话引导完成 Solana 钱包创建和 W3C DID 身份生成。

流程步骤：

1. 用户在 OpenClaw 中对 Agent 发起钱包创建请求
2. StablePay Skill 调用 DID 服务生成 Solana 密钥对
3. DID 服务根据 W3C did:solana 规范构造 DID 标识符
4. DID 服务将 DID 信息存储到 MongoDB
5. StablePay Skill 将私钥加密存储到本地
6. 返回 DID 和钱包地址给用户
  
涉及服务：

- DID 服务：负责 DID 生成和身份信息管理
- 查询服务：提供 DID 信息查询接口
  
关键节点：

- 密钥生成（同步）：使用加密安全的随机数生成器
- DID 构造（同步）：严格遵循 W3C did:solana 规范
- 本地加密（同步）：私钥使用 AES-256 加密存储
  
4.3 核心业务场景三：购买验证防篡改

业务描述：
开发者后端通过调用验证 API 检查 Agent 购买记录，防止用户篡改 skill 源代码绕过支付。

流程步骤：

1. Agent 访问有后端服务的 skill 功能
2. 开发者后端调用验证服务 API 检查购买记录
3. 验证服务从 MySQL 查询 Agent 和 skill 的购买关系
4. 如果未购买，开发者后端返回 HTTP 402
5. StablePay Skill 拦截 402 响应，触发支付流程
6. 支付完成后，Agent 重新访问 skill 后端服务
  
涉及服务：

- 验证服务：提供购买记录查询和验证接口
- 支付服务：处理 402 响应触发的支付流程
  
关键节点：

- 购买验证（同步）：快速查询购买关系，支持高并发
- 防篡改机制（异步）：通过后端验证避免前端代码被修改
- 支付触发（异步）：seamless 的用户体验，自动完成支付
  
4.4 异常处理机制

4.4.1 超时处理

- API 调用超时：2 秒，超时后返回降级响应或重试
- 链上交易超时：30 秒，超时后查询链上状态并补偿
- RPC 调用超时：1 秒，支持快速失败和熔断
  
4.4.2 失败重试

- 链上交易重试：网络拥堵时最大重试 3 次，指数退避
- API 调用重试：临时故障时重试 2 次，幂等性保证
- 消息消费重试：消息处理失败时重试 5 次，超过后进入死信队列
  
4.4.3 补偿机制

- 支付补偿：链上交易成功但记录更新失败时，通过定时任务补偿
- 余额补偿：Gas 费补贴失败时，通过异步任务补偿
- 数据补偿：跨服务数据不一致时，通过对账机制修复
  
5. 微服务拆分设计

5.1 服务拆分原则

- 业务领域驱动：按照 DID 管理、支付处理、数据查询等业务领域拆分
- 单一职责：每个服务只负责一个核心业务功能
- 高内聚低耦合：服务内部功能高度相关，服务间依赖最小化
- 数据独立：每个服务拥有独立的数据存储，避免数据耦合
- 可独立部署：支持独立开发、测试、部署和扩容
  
5.2 微服务清单

服务名称
服务职责
核心功能
数据边界
API Gateway
统一接入和路由
认证鉴权、限流熔断、协议转换
无持久化数据
DID Service
身份管理
DID 生成、身份验证、钱包注册
DID 信息表
Payment Service
支付处理
HTTP 402 协议、签名验证、支付流程
支付记录表
Verification Service
购买验证
购买记录查询、防篡改验证
购买关系表
Query Service
数据查询
余额查询、交易记录、收益统计
查询缓存
Blockchain Adapter
区块链交互
Solana 链上交易、Gas 费补贴
交易状态表

5.3 服务依赖关系

API Gateway → DID Service (HTTP)
API Gateway → Payment Service (HTTP) 
API Gateway → Verification Service (HTTP)
API Gateway → Query Service (HTTP)

Payment Service → DID Service (Kitex RPC)
Payment Service → Blockchain Adapter (Kitex RPC)
Payment Service → RocketMQ (异步消息)

Verification Service → RocketMQ (异步消息消费)

Query Service → Blockchain Adapter (Kitex RPC)
Query Service → Redis (缓存)

Blockchain Adapter → Solana Network (链上调用)

依赖原则：

- 避免循环依赖，保持单向依赖关系
- 控制依赖深度，最大依赖深度不超过 3 层
- 优先使用异步解耦，减少同步调用依赖
- 核心服务（Payment、DID）避免依赖非核心服务
  
6. 各微服务详细设计

6.1 DID Service 设计

6.1.1 服务职责

负责 W3C did:solana 身份管理，包括 DID 生成、钱包注册、身份验证等功能。

6.1.2 核心功能模块

模块 1：DID 生成管理

- 功能描述：根据 W3C did:solana 规范生成 DID 标识符
- 业务规则：
  - DID 格式：did:solana:{base58_encoded_pubkey}
  - 私钥使用 Ed25519 算法生成
  - DID 唯一性校验
    
模块 2：身份验证

- 功能描述：验证 DID 签名和身份合法性
- 业务规则：
  - 支持 Ed25519 签名验证
  - 签名内容包含时间戳防重放
  - DID 状态检查（激活/禁用）
    
模块 3：钱包配置管理

- 功能描述：管理与 DID 关联的钱包配置信息
- 业务规则：
  - 支持多钱包绑定
  - 配置信息加密存储
  - 支持配置版本管理
    
6.1.3 对外接口

接口名称
接口路径
方法
功能说明
创建 DID
/api/v1/did
POST
生成新的 DID 和钱包
查询 DID
/api/v1/did/{did}
GET
查询 DID 详细信息
验证签名
/api/v1/did/verify
POST
验证 DID 签名合法性
更新配置
/api/v1/did/{did}/config
PUT
更新钱包配置信息

6.1.4 依赖的其他服务

无外部服务依赖，作为基础服务被其他服务调用

6.1.5 发布的事件

- DID 创建事件
  
  - 触发时机：新 DID 创建成功时
  - 事件内容：DID、钱包地址、创建时间
- 配置更新事件
  
  - 触发时机：钱包配置发生变更时
  - 事件内容：DID、配置版本、变更字段
    
6.2 Payment Service 设计

6.2.1 服务职责

处理 HTTP 402 协议支付流程，包括支付请求验证、签名校验、链上交易协调。

6.2.2 核心功能模块

模块 1：HTTP 402 协议处理

- 功能描述：实现标准 HTTP 402 Payment Required 协议
- 业务规则：
  - 未购买资源返回 402 状态码和支付要求
  - 支持支付挑战和响应机制
  - 兼容标准 HTTP 402 头部字段
    
模块 2：支付签名验证

- 功能描述：验证 Agent 提交的支付签名合法性
- 业务规则：
  - 验证签名算法和格式
  - 检查签名时间戳防重放攻击
  - 验证支付金额和接收方地址
    
模块 3：支付流程协调

- 功能描述：协调完整的支付流程执行
- 业务规则：
  - 余额检查和支付能力验证
  - 链上交易发起和状态跟踪
  - 支付结果确认和记录更新
    
6.2.3 对外接口

接口名称
接口路径
方法
功能说明
发起支付
/api/v1/pay
POST
处理 HTTP 402 支付请求
支付状态
/api/v1/pay/{tx_id}
GET
查询支付交易状态
支付历史
/api/v1/pay/history
GET
查询支付历史记录

6.2.4 依赖的其他服务

- DID Service：用于验证 Agent 身份和签名，调用方式为同步 Kitex RPC
- Blockchain Adapter：用于执行链上交易，调用方式为同步 Kitex RPC
  
6.2.5 发布的事件

- 支付成功事件
  
  - 触发时机：链上交易确认成功时
  - 事件内容：支付 ID、Agent DID、Skill DID、金额、交易哈希
- 支付失败事件
  
  - 触发时机：支付过程发生错误时
  - 事件内容：支付 ID、失败原因、错误代码
    
6.3 Verification Service 设计

6.3.1 服务职责

提供购买记录验证和查询服务，防止 skill 源代码篡改绕过支付。

6.3.2 核心功能模块

模块 1：购买记录管理

- 功能描述：管理 Agent 购买 skill 的关系记录
- 业务规则：
  - 购买记录永久保存
  - 支持批量查询和统计
  - 记录包含购买时间、金额、交易哈希
    
模块 2：防篡改验证

- 功能描述：为开发者后端提供购买验证 API
- 业务规则：
  - 快速响应验证请求
  - 支持批量验证
  - 提供购买证明和时间戳
    
模块 3：审计追踪

- 功能描述：记录所有验证请求和结果
- 业务规则：
  - 验证请求日志记录
  - 支持验证行为分析
  - 异常验证告警机制
    
6.3.3 对外接口

接口名称
接口路径
方法
功能说明
验证购买
/api/v1/verify
GET
验证 Agent 是否购买指定 skill
批量验证
/api/v1/verify/batch
POST
批量验证多个购买记录
购买证明
/api/v1/verify/proof
GET
获取购买证明和详细信息

6.3.4 依赖的其他服务

无外部服务依赖，通过 RocketMQ 异步消费支付事件

6.3.5 发布的事件

- 验证请求事件
  - 触发时机：收到购买验证请求时
  - 事件内容：请求来源、验证结果、响应时间
    
7. 服务间通信设计

7.1 同步通信方案

7.1.1 通信协议

- 外部 HTTP 接口：使用 Hertz HTTP 框架，支持 RESTful API
- 内部 RPC 通信：使用 Kitex Thrift RPC，支持高性能服务间调用
- 选择原因：HTTP 适合对外接口，Thrift RPC 适合内部高频调用
  
7.1.2 调用场景

- 场景 1：Payment Service 调用 DID Service 验证身份签名（需要立即获取验证结果）
- 场景 2：Payment Service 调用 Blockchain Adapter 执行链上交易（需要确认交易结果）
- 场景 3：Query Service 调用 Blockchain Adapter 查询链上余额（需要实时余额信息）
  
7.1.3 超时与重试

- 超时时间：内部 RPC 调用 1000ms，外部 HTTP 调用 2000ms
- 重试策略：最大重试 3 次，指数退避间隔（100ms, 200ms, 400ms）
- 熔断策略：连续失败 5 次后熔断 30 秒，半开状态测试恢复
  
7.2 异步通信方案

7.2.1 消息中间件

- 选择：RocketMQ 5.0+
- 消息模式：发布订阅模式，支持广播和集群消费
- 选择原因：支持事务消息、顺序消息、延时消息，可靠性高
  
7.2.2 调用场景

- 场景 1：Payment Service 支付成功后发布事件，Verification Service 订阅更新购买记录
- 场景 2：DID Service 创建身份后发布事件，Query Service 订阅初始化缓存
- 场景 3：异步审计日志和监控数据收集
  
7.2.3 消息可靠性保证

- 生产端：使用 RocketMQ 事务消息，保证本地事务和消息发送的一致性
- 消费端：ACK 确认机制，消费失败时重试，支持幂等性处理
- 死信队列：重试次数超限的消息进入死信队列，人工处理
  
7.3 服务调用链路

7.3.1 HTTP 402 支付链路

StablePay Skill → API Gateway → Payment Service → DID Service (验证签名)
                                      ↓
Payment Service → Blockchain Adapter → Solana Network
                                      ↓  
Payment Service → RocketMQ → Verification Service (更新记录)

7.3.2 购买验证链路

开发者后端 → API Gateway → Verification Service → MySQL (查询购买记录)

7.3.3 余额查询链路

StablePay Skill → API Gateway → Query Service → Redis (缓存查询)
                                     ↓
Query Service → Blockchain Adapter → Solana Network (链上余额)

8. 数据库设计

8.1 数据库拆分原则

- 每服务独立数据库：每个微服务拥有独立的 MySQL 数据库实例
- 数据所有权明确：数据只能由所属服务直接访问，其他服务通过 API 调用
- 避免跨库 Join：通过服务调用获取关联数据，保持服务解耦
- 数据冗余可接受：适度的数据冗余换取服务独立性和查询性能
  
8.2 各服务数据库设计

8.2.1 DID Service 数据库

数据库名称：stablepay_did_db

核心表结构：

表 1：did_identities

- 功能说明：存储 DID 身份核心数据
- 主要字段：
  - id：主键，自增 ID
  - did：DID 标识符（唯一索引）
  - public_key：公钥（base58 编码）
  - wallet_address：Solana 钱包地址
  - status：状态字段（active/disabled）
  - created_at：创建时间
  - updated_at：更新时间
- 索引设计：
  - 主键索引：PRIMARY KEY (id)
  - 唯一索引：UNIQUE KEY uk_did (did)
  - 普通索引：KEY idx_status_created (status, created_at)
    
表 2：did_configs

- 功能说明：存储 DID 关联的配置信息
- 主要字段：
  - id：主键，自增 ID
  - did：DID 标识符（外键）
  - config_type：配置类型（wallet/payment/preference）
  - config_data：配置内容（JSON 格式，加密存储）
  - version：配置版本号
  - created_at：创建时间
  - updated_at：更新时间
- 索引设计：
  - 主键索引：PRIMARY KEY (id)
  - 联合索引：KEY idx_did_type (did, config_type)
    
数据量预估：

- 初期数据量：1 万个 DID
- 日增长量：100-500 个 DID
- 数据保留策略：永久保存，定期归档历史配置
  
8.2.2 Payment Service 数据库

数据库名称：stablepay_payment_db

核心表结构：

表 1：payment_transactions

- 功能说明：存储支付交易核心数据
- 主要字段：
  - id：主键，自增 ID
  - tx_id：交易 ID（UUID，唯一索引）
  - agent_did：支付方 DID
  - skill_did：收款方 DID
  - amount：支付金额（单位：USDC 最小单位）
  - currency：货币类型（USDC/USDT）
  - tx_hash：链上交易哈希
  - status：交易状态（pending/confirmed/failed）
  - created_at：创建时间
  - confirmed_at：确认时间
- 索引设计：
  - 主键索引：PRIMARY KEY (id)
  - 唯一索引：UNIQUE KEY uk_tx_id (tx_id)
  - 联合索引：KEY idx_agent_status (agent_did, status)
  - 联合索引：KEY idx_skill_confirmed (skill_did, confirmed_at)
    
表 2：gas_subsidies

- 功能说明：记录 Gas 费补贴详情
- 主要字段：
  - id：主键，自增 ID
  - tx_id：关联的支付交易 ID
  - gas_amount：Gas 费金额（单位：SOL 最小单位）
  - subsidy_tx_hash：补贴交易哈希
  - status：补贴状态（pending/completed/failed）
  - created_at：创建时间
- 索引设计：
  - 主键索引：PRIMARY KEY (id)
  - 外键索引：KEY fk_tx_id (tx_id)
    
数据量预估：

- 初期数据量：500 笔交易
- 日增长量：50-200 笔交易
- 数据保留策略：永久保存，按月分表存储
  
8.2.3 Verification Service 数据库

数据库名称：stablepay_verification_db

核心表结构：

表 1：purchase_records

- 功能说明：存储购买关系记录
- 主要字段：
  - id：主键，自增 ID
  - agent_did：购买方 DID
  - skill_did：被购买的 skill DID
  - tx_id：关联的支付交易 ID
  - purchase_amount：购买金额
  - purchase_time：购买时间
  - proof_hash：购买证明哈希
  - created_at：记录创建时间
- 索引设计：
  - 主键索引：PRIMARY KEY (id)
  - 联合唯一索引：UNIQUE KEY uk_agent_skill (agent_did, skill_did)
  - 普通索引：KEY idx_purchase_time (purchase_time)
    
表 2：verification_logs

- 功能说明：记录验证请求日志
- 主要字段：
  - id：主键，自增 ID
  - request_id：请求 ID（UUID）
  - agent_did：被验证的 Agent DID
  - skill_did：被验证的 skill DID
  - verification_result：验证结果（purchased/not_purchased）
  - request_source：请求来源 IP
  - response_time：响应时间（毫秒）
  - created_at：请求时间
- 索引设计：
  - 主键索引：PRIMARY KEY (id)
  - 普通索引：KEY idx_created_result (created_at, verification_result)
    
8.3 跨库查询方案

8.3.1 服务间调用

- 场景：Query Service 需要聚合多个服务的数据
- 方案：Query Service 通过 Kitex RPC 调用其他服务获取数据
- 优点：保持服务边界清晰，数据一致性由服务保证
- 缺点：增加网络调用开销，需要处理服务依赖
  
8.3.2 数据冗余

- 场景：Query Service 频繁需要 payment 和 verification 数据进行统计
- 方案：Query Service 通过 RocketMQ 订阅数据变更事件，本地冗余存储
- 同步方式：异步事件驱动，实时同步关键数据
- 一致性保证：最终一致性，通过定时对账检测数据偏差
  
8.3.3 CQRS 模式

- 场景：复杂的收益统计和交易分析查询
- 方案：构建独立的查询数据库，聚合所有相关数据
- 同步方式：订阅各服务的数据变更事件，更新查询视图
- 查询优化：针对查询场景设计专门的数据结构和索引
  
8.4 数据一致性保障

8.4.1 事务场景分类

- 单服务事务：服务内部操作使用 MySQL 本地事务
- 跨服务强一致性：关键支付流程使用分布式事务
- 跨服务最终一致性：数据同步和统计使用异步消息机制
  
8.4.2 分布式事务方案

- 方案选择：基于 RocketMQ 的本地消息表模式
- 适用场景：支付成功后需要原子性更新购买记录
- 实现机制：
  1. 本地事务执行业务操作并插入消息表
  2. 异步任务发送消息到 RocketMQ
  3. 消费方接收消息并执行补偿操作
  4. 失败时通过消息重试和幂等性保证最终一致
    
8.4.3 最终一致性方案

- 实现方式：基于 RocketMQ 的异步事件驱动
- 补偿策略：
  - 定时对账任务检测数据不一致
  - 自动补偿简单的数据偏差
  - 复杂情况通过告警通知人工介入
- 监控告警：实时监控数据一致性指标，异常时及时告警
  
## 9. API 接口设计

### 9.1 API 设计规范

9.1.1 RESTful 规范

URL 设计：

- 使用名词复数：/api/v1/payments
- 资源嵌套不超过 2 层：/api/v1/payments/{id}/status
- 使用连字符分隔：/api/v1/purchase-records
  
HTTP 方法：

- GET：查询资源
- POST：创建资源
- PUT：全量更新资源
- PATCH：部分更新资源
- DELETE：删除资源
  
版本管理：

- URL 版本：/api/v1/
- 版本兼容性：保持向后兼容，新版本增量更新
  
9.1.2 请求响应格式

统一响应格式：

{
  "code": 0,
  "message": "success",
  "data": {
    // 业务数据
  },
  "request_id": "uuid",
  "timestamp": "2024-01-01T00:00:00Z"
}

分页响应格式：

{
  "code": 0,
  "message": "success", 
  "data": {
    "items": [],
    "total": 100,
    "page": 1,
    "page_size": 20
  }
}

HTTP 402 特殊响应格式：

{
  "code": 402,
  "message": "Payment Required",
  "data": {
    "skill_did": "did:solana:dev123",
    "price": 5,
    "currency": "USDC",
    "payment_endpoint": "https://api.stablepay.co/pay"
  }
}

9.2 核心 API 接口

9.2.1 DID 管理 API

接口 1：创建 DID

- 接口路径：POST /api/v1/did
- 功能说明：创建新的 W3C DID 身份
- 请求参数：
  
{
  "user_type": "agent|developer",
  "metadata": {
    "name": "用户名称",
    "description": "描述信息"
  }
}

- 响应示例：
  
{
  "code": 0,
  "message": "success",
  "data": {
    "did": "did:solana:4fK9x2HyJk...",
    "public_key": "4fK9x2HyJk...", 
    "wallet_address": "4fK9x2HyJk...",
    "created_at": "2024-01-01T00:00:00Z"
  }
}

接口 2：验证 DID 签名

- 接口路径：POST /api/v1/did/verify
- 功能说明：验证 DID 签名的合法性
- 请求参数：
  
{
  "did": "did:solana:4fK9x2HyJk...",
  "message": "待签名内容",
  "signature": "签名结果",
  "timestamp": "2024-01-01T00:00:00Z"
}

接口 3：查询 DID 信息

- 接口路径：GET /api/v1/did/{did}
- 功能说明：查询 DID 的详细信息
- 路径参数：
  - did：DID 标识符
    
9.2.2 支付处理 API

接口 1：发起支付

- 接口路径：POST /api/v1/pay
- 功能说明：处理 HTTP 402 支付请求
- 请求参数：
  
{
  "agent_did": "did:solana:agent123",
  "skill_did": "did:solana:dev456", 
  "amount": 5,
  "currency": "USDC",
  "signature": "支付签名",
  "timestamp": "2024-01-01T00:00:00Z"
}

- 响应示例：
  
{
  "code": 0,
  "message": "Payment successful",
  "data": {
    "tx_id": "uuid",
    "tx_hash": "solana_tx_hash",
    "status": "confirmed",
    "confirmed_at": "2024-01-01T00:00:00Z"
  }
}

接口 2：查询支付状态

- 接口路径：GET /api/v1/pay/{tx_id}
- 功能说明：查询支付交易状态
- 路径参数：
  - tx_id：交易 ID
    
9.2.3 购买验证 API

接口 1：验证购买记录

- 接口路径：GET /api/v1/verify
- 功能说明：验证 Agent 是否购买了指定 skill
- 查询参数：
  - agent_did：Agent DID
  - skill_did：Skill DID
- 响应示例：
  
{
  "code": 0,
  "message": "success",
  "data": {
    "purchased": true,
    "purchase_time": "2024-01-01T00:00:00Z",
    "tx_id": "uuid",
    "amount": 5,
    "currency": "USDC"
  }
}

接口 2：批量验证

- 接口路径：POST /api/v1/verify/batch
- 功能说明：批量验证多个购买记录
- 请求参数：
  
{
  "agent_did": "did:solana:agent123",
  "skill_dids": [
    "did:solana:dev456",
    "did:solana:dev789"
  ]
}

9.2.4 数据查询 API

接口 1：余额查询

- 接口路径：GET /api/v1/balance
- 功能说明：查询 Agent 钱包余额和消费统计
- 查询参数：
  - agent_did：Agent DID
- 响应示例：
  
{
  "code": 0,
  "message": "success",
  "data": {
    "balance": 25.5,
    "currency": "USDC", 
    "monthly_spent": 24.5,
    "monthly_limit": 50
  }
}

接口 2：交易记录查询

- 接口路径：GET /api/v1/transactions
- 功能说明：查询用户或开发者的交易历史
- 查询参数：
  - did：DID 标识符
  - type：交易类型（purchase|revenue）
  - limit：返回数量限制
  - offset：分页偏移量
    
接口 3：收益统计查询

- 接口路径：GET /api/v1/revenue
- 功能说明：查询开发者 skill 收益统计
- 查询参数：
  - skill_did：Skill DID
- 响应示例：
  
{
  "code": 0,
  "message": "success",
  "data": {
    "total_revenue": 120,
    "total_sales": 24,
    "currency": "USDC",
    "sales_trend": [
      {"date": "2024-01-01", "amount": 15},
      {"date": "2024-01-02", "amount": 25}
    ]
  }
}

9.3 认证与鉴权方案

9.3.1 认证方式

DID 签名认证：

- 认证流程：
  1. 客户端使用 DID 私钥对请求内容签名
  2. API 网关验证 DID 签名合法性
  3. 网关将验证后的 DID 信息传递给后端服务
- 签名内容：请求路径 + 请求体 + 时间戳
- 防重放：时间戳有效期 5 分钟，防止签名被重复使用
  
API Key 认证（开发者后端）：

- 用于开发者后端服务调用验证 API
- API Key 通过安全渠道分发
- 支持 API Key 权限范围限制
  
9.3.2 鉴权策略

基于 DID 的权限控制：

- Agent DID 只能查询自己的数据
- Developer DID 只能查询自己发布的 skill 数据
- 支持 DID 权限委托和代理机制
  
接口级权限控制：

- 支付接口：需要 Agent DID 签名认证
- 验证接口：支持 API Key 或 DID 签名认证
- 查询接口：需要对应的 DID 权限
  
9.3.3 安全机制

HTTPS 传输：

- 所有 API 强制使用 HTTPS
- 支持 TLS 1.3 加密协议
- HTTP 请求自动重定向到 HTTPS
  
签名验证：

- 关键接口增加双重签名验证
- 支持签名算法升级和兼容性处理
- 签名失败时详细错误日志记录
  
防重放攻击：

- 时间戳窗口验证（±5 分钟）
- Nonce 机制防止相同请求重复
- 服务端缓存已处理的请求签名
  
限流保护：

- IP 级限流：单 IP 每分钟 100 次请求
- DID 级限流：单 DID 每分钟 50 次请求
- 接口级限流：支付接口每分钟 10 次请求
  
9.4 错误码规范

9.4.1 错误码设计

错误码
错误信息
说明
0
success
请求成功
10001
invalid parameters
请求参数不合法
10002
resource not found
请求的资源不存在
10003
permission denied
无权限访问该资源
10004
signature verification failed
DID 签名验证失败
20001
insufficient balance
钱包余额不足
20002
payment already exists
重复支付
20003
blockchain network error
区块链网络错误
20004
gas subsidy failed
Gas 费补贴失败
30001
internal server error
系统内部错误
30002
service unavailable
依赖服务不可用
30003
database connection error
数据库连接错误
30004
rate limit exceeded
请求频率超限

9.4.2 HTTP 状态码使用

- 200：成功
- 400：客户端请求错误（参数错误、签名失败等）
- 401：未认证（DID 签名缺失）
- 402：需要支付（Payment Required，核心状态码）
- 403：无权限（DID 权限不足）
- 404：资源不存在
- 429：请求过于频繁（触发限流）
- 500：服务器内部错误
- 503：服务不可用（依赖服务故障）
  
10. 安全设计

10.1 网络安全

10.1.1 网络隔离

VPC 隔离：

- 所有服务部署在独立的阿里云 VPC 内
- 生产环境和测试环境完全隔离
- 数据库和中间件不暴露公网访问
  
安全组规则：

- 遵循最小权限原则，只开放必要端口
- API 网关：开放 443 端口（HTTPS）
- 微服务：仅内网开放服务端口
- 数据库：仅允许应用服务器访问
  
内外网隔离：

- 业务服务运行在私有子网，无公网 IP
- 通过 NAT 网关访问外部服务（Solana RPC）
- 敏感配置和密钥不通过公网传输
  
10.1.2 DDoS 防护

防护措施：

- 使用阿里云 Anti-DDoS 服务
- 清洗异常流量和恶意攻击
- 自动识别和拦截 DDoS 攻击模式
  
流量清洗：

- 基于 IP 信誉和行为分析
- 自动清洗恶意爬虫和僵尸网络流量
- 保护关键支付和验证接口
  
黑名单机制：

- 恶意 IP 自动封禁 24 小时
- 恶意 DID 限制访问频率
- 支持手动添加黑名单规则
  
10.2 API 安全

10.2.1 认证鉴权

多层认证：

- API 网关层：DID 签名验证
- 服务层：业务权限检查
- 数据层：数据访问权限控制
  
DID 签名管理：

- 签名有效期 5 分钟，自动过期
- 支持签名算法升级（Ed25519 → 未来算法）
- 签名验证失败的详细错误日志
  
权限控制：

- 基于 DID 的细粒度权限控制
- Agent 只能操作自己的资产
- Developer 只能管理自己的 skill
  
10.2.2 数据传输安全

HTTPS 加密：

- 所有外部 API 强制使用 HTTPS
- TLS 1.3 加密协议，支持前向保密
- HTTP 自动重定向到 HTTPS
  
敏感数据保护：

- DID 私钥客户端本地存储，从不传输
- 支付签名使用一次性 nonce
- API 响应中敏感字段脱敏处理
  
请求完整性：

- 支付请求必须包含完整签名
- 防止请求内容被篡改
- 支持请求内容哈希校验
  
10.2.3 接口防护

限流策略：

- 支付接口：每 DID 每分钟 10 次
- 查询接口：每 DID 每分钟 30 次
- 验证接口：每 API Key 每分钟 100 次
  
防重放攻击：

- 时间戳窗口验证（±5 分钟）
- Nonce 机制确保请求唯一性
- Redis 缓存已处理的请求签名
  
参数校验：

- 严格的参数类型和格式验证
- 金额参数范围检查（防止异常大额）
- DID 格式符合 W3C 规范验证
  
SQL 注入防护：

- 使用参数化查询（prepared statement）
- ORM 层自动转义特殊字符
- 数据库连接使用最小权限账号