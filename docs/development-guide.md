# 开发指南

面向不熟悉 CloudWeGo 和 Go 后端的开发者。

---

## 一、前置知识

### 你需要了解什么

| 概念 | 必/选 | 说明 |
|------|-------|------|
| Go 基础语法 | 必 | 结构体、接口、函数、错误处理 |
| HTTP 基础 | 必 | GET/POST、状态码、Header |
| JSON | 必 | 序列化/反序列化 |
| SQL 基础 | 选 | 后面用 SQLite 时会用到 |
| Solana/DID | 选 | 理解 x402 流程即可 |

### 你不需要了解什么

- ❌ 不需要 DDD / 整洁架构经验（COLA 架构本身就是帮你理清概念的）
- ❌ 不需要云原生/容器化经验（后面会手把手加）
- ❌ 不需要区块链开发经验（支付在链上，但商户后端只做 HTTP 调用）

---

## 二、为什么用 CloudWeGo Hertz？

4 个理由：

1. **高性能** — 字节跳动内部验证，生产级
2. **与 Gin API 兼容** — 如果你用过 Gin，Hertz 几乎一样
3. **中文生态** — 文档齐全、社区活跃
4. **与现有 K8s 基础设施匹配** — 现有 MicroService 也是 CloudWeGo 技术栈

---

## 三、开发环境搭建

### 3.1 安装 Go

```bash
# 检查版本
go version
# 需要 Go 1.22+ (当前: 1.26.1)
```

### 3.2 下载依赖

```bash
cd D:\MyLab\StablePay\merchant
go mod tidy
```

这个命令会自动下载 Hertz 和 YAML 解析库。

### 3.3 编译运行

```bash
# 方式 1: 直接运行
go run ./cmd/merchant-server

# 方式 2: 编译后运行
go build -o merchant-server.exe ./cmd/merchant-server
./merchant-server.exe

# 验证
curl http://127.0.0.1:8787/healthz
```

### 3.4 编译时可能遇到的问题

**Q: `go mod tidy` 下载 Hertz 失败（网络问题）？**

在 Go 1.26 中，`GOPROXY` 默认可以通过。如果下载失败，设置代理：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
go mod tidy
```

---

## 四、代码开发指引

### 4.1 添加新 API 的标准步骤

```
1. 定义 DTO        → internal/adapter/dto/xxx.go
2. 定义 Handler    → internal/adapter/handler/xxx_handler.go
3. 注册路由        → internal/adapter/router.go → Register()
4. 定义 AppService  → internal/application/service/xxx_app_service.go
5. 定义 Domain      → internal/domain/entity/xxx.go (实体)
                    → internal/domain/repository/xxx_repo.go (接口)
6. 实现 Repository  → internal/infrastructure/persistence/xxx_repo_impl.go
7. 注入依赖        → cmd/merchant-server/main.go
```

### 4.2 代码风格

- 使用 `// Copyright 2025 StablePay. All rights reserved.` 文件头
- 使用 `TODO:` 标记未完成的功能
- 导出函数需要有 Go 风格的注释（以函数名开头）
- 错误处理使用 `fmt.Errorf("上下文: %w", err)` 包装

### 4.3 关于 TODO

代码中有很多 `TODO` 标记，它们是"骨架代码"的占位符。后续步骤会逐个实现。

---

## 五、常见概念解释

### 什么是 DTO？

DTO (Data Transfer Object) 是 API 的输入/输出数据结构。

它和 Entity（领域实体）的区别：
- **DTO**：关注"接口长什么样"（比如响应中的字段名用 snake_case）
- **Entity**：关注"业务模型有什么属性"

### 什么是 Repository？

Repository 是数据访问的抽象。在 Domain 层定义接口，在 Infrastructure 层实现。

优点：切换数据库时，只需要改实现层，Domain 层完全不受影响。

### 什么是 x402？

x402 是一个 HTTP 支付标准：
- 服务器返回 `402 Payment Required`
- 响应中包含标准化的支付信息（用 Base64 编码的 JSON）
- 客户端解析后调用支付网关完成付款
- 付款后重试原请求

---

## 六、测试

```bash
# 编译检查
go build ./...

# 运行所有测试
go test ./...

# 运行特定包的测试
go test ./internal/domain/...
```
