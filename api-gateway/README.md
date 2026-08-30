# StablePay API Gateway

StablePay 一期内层薄网关实现。
外层阿里云 API Gateway 负责公网入口和运维接入；本仓库只实现应用层网关能力。

## 核心能力

- 统一 HTTP 入口与路由
- 统一响应包装：`code/message/data/request_id/timestamp`
- `request_id` / `trace_id` 透传
- 鉴权：DID 签名、API Key
- 限流：IP、DID、接口级
- 重放保护：`timestamp + nonce`
- 快捷入口兼容：`/pay`、`/verify`
- 健康检查与就绪检查

## v0.1 签名串规则

```text
METHOD + "\n" +
PATH + "\n" +
RAW_QUERY + "\n" +
BODY_SHA256 + "\n" +
TIMESTAMP + "\n" +
NONCE
```

约束：

- `BODY_SHA256`：空 body 也计算固定 SHA256
- `RAW_QUERY`：按原始 query string，不重排
- `NONCE`：必填
- `TIMESTAMP`：兼容 RFC3339 / Unix 秒，建议统一 RFC3339

## 快速启动

```bash
make tidy
make run
```

Windows PowerShell:

```powershell
.\scripts\start.ps1
```

健康检查：

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
```

## 配置

主配置文件：`config/config.yaml`
本地覆盖示例：`config/config.local.yaml.example`

## 测试

```bash
make test
```

## Go 环境建议

当前如果出现 `GOPATH and GOROOT are the same directory`，建议将 `GOPATH` 调整到独立目录，例如：

```powershell
go env -w GOPATH=D:\gopath
```

## 文档

- 运行手册：`docs/runbook.md`
- 运维手册：`docs/operations.md`
- 模块说明：`docs/modules.md`
- API 说明：`docs/api.md`

## stablepay-common 依赖

- `api-gateway` 通过远程 Go Module 引用 `stablepay-common`
- 当前引用模块：
  - `code.wenfu.cn/stablepayai/stablepay-common/log`
  - `code.wenfu.cn/stablepayai/stablepay-common/money`
- 不使用 `replace`
- 使用 `stablepay-common` 单根模块版本
- 首次拉取或更新私有仓库权限时，执行 `go mod tidy`
