我是 StablePay DEMO 的研发人员，目前我已经完成了 StablePay DEMO 的 PRD 设计文档和技术方案文档。

请基于以下 PRD 文档和技术方案文档，产出 StablePay 的单个服务代码仓库：https://codeup.aliyun.com/6878738f874c52f1221c8c29/stablepayai/api-gateway
当前仓库仅对应一个服务：API Gateway

PRD 文档：https://qjpkawdabe9q.jp.larksuite.com/wiki/PVoZwyWk5iQHzikxPmrjEirlpUe?chunked=false
技术方案文档：https://qjpkawdabe9q.jp.larksuite.com/wiki/IwzFwkOttiDIaZkjN4GjyrGZpCb?chunked=false

说明：
- 如果 larksuite / 飞书链接需要 MCP，请使用 MCP 访问
- 代码 go 多模块工作区模式，各个模块设计参考 COLA v5：https://github.com/alibaba/COLA
- 如果任一链接无法访问、内容不一致、或信息缺失，请先停止并明确指出问题，等待我确认后再继续
- 只生成当前这个服务的代码仓库，不要生成整个 StablePay
- 代码注释使用中文
- 必须包含完整单元测试、运行手册、运维手册、模块说明文档、对外接口说明
- 先详细列出技术选型、模块设计、接口设计、依赖关系和目录结构，等我确认后再生成代码

【当前服务】
服务名称：API Gateway
核心职责：
- 统一接入层
- 认证鉴权
- 限流熔断
- 协议转换
- 请求路由与错误码归一化
- 统一 request_id / trace_id 透传
- 对外暴露 HTTP 接口，向 DID Service、Payment Service、Verification Service、Query Service 转发请求
- 不承载核心业务规则，不直接操作链上，也不直接做交易状态持久化

【技术要求】
- 技术选型必须与 StablePay DEMO 保持一致，优先遵循技术方案文档
- 使用 Go 语言
- 使用 CloudWeGo 生态，HTTP 层优先使用 Hertz
- 仓库结构参考 COLA v5 分层思想
- 单仓库模式；如果你认为适合使用 go work / 多模块工作区模式，请先给出理由
- 对外只提供 HTTP 接口
- 要支持配置化路由、限流、熔断、鉴权、日志、健康检查
- 要考虑生产可用性：超时、重试、熔断、panic recover、优雅停机、配置管理、日志分级
- 要预留后续接入监控与链路追踪的扩展点

【必须实现的能力】
1. 统一 HTTP 入口
2. 路由到下游服务：
   - DID Service
   - Payment Service
   - Verification Service
   - Query Service
3. 统一鉴权中间件：
   - 支持 DID 签名透传/校验前置逻辑（以技术方案为准）
   - 支持 API Key / 内部服务调用鉴权（如果文档中有）
4. 限流熔断：
   - IP 级
   - DID 级
   - 接口级
5. 统一错误码和响应格式
6. 统一日志与 request_id
7. 健康检查、就绪检查
8. 基础配置热更新或配置抽象能力（如果文档支持）
9. 编写面向网关层的单元测试与中间件测试

【输出顺序要求】
第一步：先输出，不要写代码
1. 你从 PRD / 技术方案中提取到的 API Gateway 需求摘要
2. 技术选型清单（框架、配置、日志、鉴权、限流、熔断）
3. 代码目录结构设计
4. 中间件设计
5. 路由设计
6. 下游服务调用方式设计
7. 测试策略
8. 运行与部署说明提纲
9. 你发现的文档冲突点 / 待确认问题

第二步：在我确认后，再生成完整代码仓库，要求包含：
- 完整 Go 代码
- 配置文件
- Makefile / 启动脚本
- Dockerfile（如合适）
- 单元测试
- README
- 运维手册
- API 说明文档

检查：
- 只生成 API Gateway 仓库
- 不要生成其他服务代码
- 如果文档之间有命名冲突，先列出并等待确认