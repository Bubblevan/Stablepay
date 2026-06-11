# StablePay Merchant Server
提供商品列表展示、x402 标准支付流程等能力，与 `stablepay-openclaw-plugin` 配合实现 AI Agent 付费购买能力。

## 技术栈

| 组件 | 选型 | 说明 |
|------|------|------|
| HTTP 框架 | [CloudWeGo Hertz](https://www.cloudwego.io/zh/docs/hertz/) | 字节跳动开源的高性能 Go HTTP 框架 |
| 架构风格 | COLA 4层架构 | 适配层/应用层/领域层/基础设施层 |
| 数据库 | SQLite (第一阶段) | 使用 modernc.org/sqlite（纯 Go，无 CGO 依赖） |
| 协议 | x402 Payment Standard | HTTP 402 触发支付，无缝集成 StablePay |

---

## 快速开始

```bash
# 1. 克隆后进入目录
cd merchant

# 2. 下载依赖
go mod tidy

# 3. 编译运行
go run ./cmd/merchant-server

# 4. 验证
curl http://127.0.0.1:8787/healthz
```

## 项目结构

```
merchant/
├── cmd/
│   └── merchant-server/          # 启动入口
│       └── main.go
├── config/
│   ├── config.go                 # 配置加载与解析
│   └── config.yaml               # 默认配置文件
├── internal/
│   ├── adapter/                  # 🟢 适配层 (Controller)
│   │   ├── dto/                  #    请求/响应 DTO
│   │   │   ├── request.go
│   │   │   └── response.go
│   │   ├── handler/              #    HTTP Handler
│   │   │   └── health_handler.go
│   │   └── router.go             #    路由注册
│   ├── application/              # 🔵 应用层 (Application)
│   │   └── service/
│   │       └── product_app_service.go
│   ├── domain/                   # 🟡 领域层 (Domain)
│   │   ├── entity/               #    领域实体
│   │   │   └── product.go
│   │   ├── repository/           #    仓储接口
│   │   │   └── product_repo.go
│   │   └── service/              #    领域服务
│   │       └── product_domain_service.go
│   └── infrastructure/           # 🟠 基础设施层 (Infrastructure)
│       ├── client/               #    外部服务调用
│       │   └── stablepay_client.go
│       └── persistence/          #    数据持久化
│           └── sqlite/
│               └── product_repo_impl.go
├── docs/
│   ├── architecture.md           # 架构设计文档
│   ├── development-guide.md      # 开发指南
│   └── interview-guide.md        # 面试讲解指南
├── go.mod
└── go.sum
```

---

## API 接口（规划中）

| 方法 | 路径 | 说明 | 状态 |
|------|------|------|------|
| GET | `/healthz` | 健康检查 | ✅ 可用 |
| GET | `/api/v1/products` | 商品列表 | 🔜 第2步 |
| GET | `/api/v1/products/:id` | 商品详情 | 🔜 第2步 |
| GET | `/api/v1/products/:id/execute` | 执行购买(x402) | 🔜 第3步 |

## 相关项目

- [stablepay-openclaw-plugin](https://github.com/stablepay/stablepay-openclaw-plugin) — OpenClaw 插件（Agent 侧）
- [showmethemoney-skill](https://github.com/stablepay/showmethemoney-skill) — 旧版 Skill 后端（Node.js）
- [infra-deployment](https://github.com/stablepay/infra-deployment) — K8s 部署配置
