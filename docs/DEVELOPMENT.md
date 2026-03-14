# 开发指南

## 环境要求

### 必需
- Go 1.21 或更高版本
- Python 3.8 或更高版本
- make (Windows 可用 WSL 或 Git Bash)

### 可选
- Postman/curl (API 测试)
- VS Code + Go 扩展

## 本地开发流程

### 1. 直接运行后端（推荐 Windows）

```powershell
cd cmd/server
go run main.go
```

### 或：编译后运行

```powershell
cd cmd/server
go build -o ../../bin/server .
```

然后运行编译后的程序：
```powershell
# Windows
.\bin\server.exe

# Linux/Mac
./bin/server
```

### 2. 启动服务

上面任一方式启动后，输出：

输出应该看到：
```
StablePay M0 Server listening on :8080
Skill DID: did:skill:stablepay:v1
```

### 3. 测试 API

**终端 1**：
```bash
make server
```

**终端 2**：运行测试

#### 测试 1: 健康检查
```bash
curl http://localhost:8080/health
```
预期：`{"status":"ok"}`

#### 测试 2: 首次请求（402）
```bash
curl -H "X-Agent-DID: did:agent:test:m0" \
  http://localhost:8080/protected
```
预期：
```json
{
  "recipient": "4zMMUHCXxYNbtjS7MBvVh8G7zVqKjn6fKgcR88VVFBq",
  "amount": 10000,
  "currency": "USDC",
  "description": "Payment required for StablePay Skill access",
  "timeout": 300
}
```
HTTP 状态码：`402`

#### 测试 3: 提交支付
```bash
curl -X POST http://localhost:8080/pay \
  -H "Content-Type: application/json" \
  -d '{
    "agent_did": "did:agent:test:m0",
    "tx_hash": "5mVz4n7kL2pQwR9xJ8tY3uA6bC1dE5fG7hI9jK0lM2nO3pQ4rStU5vW",
    "amount": 10000
  }'
```
预期：
```json
{
  "status": "payment_recorded",
  "message": "Payment recorded successfully. Please retry the request."
}
```

#### 测试 4: 重试请求（200）
```bash
curl -H "X-Agent-DID: did:agent:test:m0" \
  http://localhost:8080/protected
```
预期：
```json
{
  "status": "ok",
  "data": "This is paid premium skill content. Agent: did:agent:test:m0",
  "paid_at": {
    "agent_did": "did:agent:test:m0",
    "skill_did": "did:skill:stablepay:v1",
    "amount": 10000,
    "tx_hash": "5mVz4n7kL2pQwR9xJ8tY3uA6bC1dE5fG7hI9jK0lM2nO3pQ4rStU5vW"
  }
}
```
HTTP 状态码：`200`

#### 测试 5: 验证支付
```bash
curl -X POST http://localhost:8080/verify \
  -H "Content-Type: application/json" \
  -d '{
    "agent_did": "did:agent:test:m0",
    "skill_did": "did:skill:stablepay:v1"
  }'
```
预期：
```json
{
  "verified": true,
  "message": "Agent has valid payment"
}
```

### 4. 运行 Python 客户端

```bash
python3 cmd/client/main.py
```

或使用自定义参数：
```bash
python3 cmd/client/main.py \
  --server http://localhost:8080 \
  --agent-did did:agent:custom:123
```

## 代码结构详解

### 后端 (cmd/server/main.go)

核心组件：

```go
// 1. 数据结构
type PaymentRecord struct {
    AgentDID string
    SkillDID string
    Amount   int
    TxHash   string
}

// 2. 全局状态
var payments = make(map[string]PaymentRecord)
var paymentMu sync.Mutex  // 并发控制

// 3. HTTP 处理函数
func handleProtected(w http.ResponseWriter, r *http.Request) { ... }
func handlePay(w http.ResponseWriter, r *http.Request) { ... }
func handleVerify(w http.ResponseWriter, r *http.Request) { ... }

// 4. main() 注册路由
http.HandleFunc("/protected", handleProtected)
http.HandleFunc("/pay", handlePay)
http.HandleFunc("/verify", handleVerify)
```

### 客户端 (cmd/client/main.py)

核心类：

```python
class X402Client:
    def __init__(self, server_url, agent_did):
        ...
    
    def access_protected_resource(self):
        # 步骤 1: 请求受保护资源
        
    def handle_402_payment(self, requirement):
        # 步骤 2+3: 处理 402，执行支付
        
    def retry_protected_resource(self):
        # 步骤 4: 重试请求
        
    def run(self):
        # 完整流程调度
```

## 常见修改场景

### 修改支付金额

编辑 `cmd/server/main.go`：
```go
const PAYMENT_AMOUNT = 10000  // → 修改这里
```

重新编译：
```bash
make build
make server
```

### 修改服务端口

编辑 `cmd/server/main.go` 的 main()：
```go
port := "8080"  // → 改成其他端口，如 "3000"
```

### 修改收款钱包

编辑 `cmd/server/main.go`：
```go
const PAYMENT_WALLET = "4zMMUHCXxY..."  // → 替换成自己的 Solana 钱包
```

### 添加新的 HTTP 接口

在 `cmd/server/main.go` 中：

```go
// 1. 实现处理函数
func handleNewEndpoint(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// 2. 在 main() 中注册
http.HandleFunc("/new-endpoint", handleNewEndpoint)
```

### 添加日志

使用 Go 的 `log` 包（已导入）：

```go
log.Printf("Payment recorded for %s, amount: %d\n", agentDID, amount)
```

### 测试 Python 客户端修改

编辑 `cmd/client/main.py` 后，直接运行：
```bash
python3 cmd/client/main.py
```

## 调试技巧

### 后端调试

#### 打印请求信息
```go
log.Printf("Received request: Method=%s, Path=%s\n", r.Method, r.RequestURI)
log.Printf("Headers: %+v\n", r.Header)
```

#### 检查支付状态
```go
paymentMu.Lock()
log.Printf("Current payments: %+v\n", payments)
paymentMu.Unlock()
```

#### 启用详细日志
修改 main()，增加日志中间件（M1+ 改进）

### 客户端调试

#### 启用详细输出
编辑 `cmd/client/main.py`：
```python
# 每个请求前后打印详细信息
resp = requests.get(url, headers=headers, timeout=5)
print(f"Response: {resp.status_code}, {resp.headers}, {resp.text}")
```

#### 使用 curl 测试
```bash
curl -v -H "X-Agent-DID: did:agent:test:m0" \
  http://localhost:8080/protected
```

## 单元测试 (未来)

### 后端测试 (建议 M1+ 添加)

```go
// cmd/server/main_test.go
package main

import "testing"

func TestHandleProtected(t *testing.T) {
    // 测试 402 返回
    // 测试 200 返回
    // 测试错误处理
}

func TestHandlePay(t *testing.T) {
    // 测试支付记录
    // 测试验证
}
```

运行测试：
```bash
go test -v ./cmd/server
```

## 性能测试

### 简单负载测试

使用 `ab` (Apache Bench)：

```bash
# 100 个请求，10 并发
ab -n 100 -c 10 http://localhost:8080/health

# 结果示例:
# Requests per second: 1000+ [#/sec]
# Time per request: 10 [ms]
```

## 故障排除

### 问题 1: Go 版本不兼容

```bash
go version
# 需要 1.21 或更高
```

解决：升级 Go 或修改 go.mod

### 问题 2: 端口被占用

```bash
# Linux/Mac
lsof -i :8080

# Windows (需要管理员)
netstat -ano | findstr :8080
```

解决：改用其他端口或关闭占用进程

### 问题 3: Python requests 库缺失

```bash
pip install requests
```

### 问题 4: 支付后仍然返回 402

可能原因：
- 支付记录未保存到 `payments` map
- Agent DID 不匹配

调试：
```bash
# 检查支付数据
curl -X POST http://localhost:8080/verify \
  -H "Content-Type: application/json" \
  -d '{"agent_did":"did:agent:test:m0","skill_did":"did:skill:stablepay:v1"}'

# 如果返回 verified: false，说明支付未被记录
```

## Git 提交规范 (建议)

```
M0 开发提交示例：

feat: Add basic x402 flow
- Implement HTTP 402 handler
- Add payment verification endpoint
- Create Python CLI client

fix: Fix payment record race condition
- Add mutex protection for payments map

docs: Update API documentation
```

## 下一步 (M1)

- [ ] 集成 Solana RPC 验证真实交易
- [ ] 添加 PostgreSQL 数据库
- [ ] 实现本地私钥管理
- [ ] 编写单元测试
- [ ] 设置 CI/CD (GitHub Actions)
- [ ] 性能优化 (CloudWeGo 框架迁移)

## 参考资源

- [Go net/http 文档](https://pkg.go.dev/net/http)
- [Python requests 文档](https://docs.python-requests.org/)
- [Solana RPC API](https://docs.solana.com/api)
- [x402 协议](https://tools.ietf.org/html/rfc7231#section-6.5.2)
