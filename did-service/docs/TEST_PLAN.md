# DID Service 接口测试方案

## 1. 测试目标

验证DID服务的三个核心接口功能正确性、安全性和稳定性：
- **CreateDID**: 创建DID身份
- **GetDID**: 查询DID信息
- **VerifySignature**: 验证Ed25519签名

## 2. 测试分层策略

```
┌─────────────────────────────────────────────┐
│  端到端测试 (E2E)                            │
│  - 启动完整服务，发送真实RPC请求             │
└──────────────────┬──────────────────────────┘
                   │
┌──────────────────▼──────────────────────────┐
│  集成测试 (Integration)                      │
│  - 应用层+仓储层，连接真实MySQL              │
└──────────────────┬──────────────────────────┘
                   │
┌──────────────────▼──────────────────────────┐
│  单元测试 (Unit)                             │
│  - 应用层逻辑，Mock仓储层                    │
└─────────────────────────────────────────────┘
```

## 3. 测试环境准备

### 3.1 测试配置文件 `conf/test.yaml`

```yaml
server:
  host: "127.0.0.1"
  port: 18081  # 测试端口

log:
  level: "debug"
  format: "console"

database:
  host: "127.0.0.1"
  port: 3306
  user: "root"
  password: "password"
  dbname: "did_service_test"  # 独立测试数据库
  charset: "utf8mb4"

encryption:
  key: "test_key_32_bytes_for_aes_256!!"  # 测试密钥
```

### 3.2 测试数据库初始化

```sql
-- 创建测试数据库
CREATE DATABASE IF NOT EXISTS did_service_test CHARACTER SET utf8mb4;

-- 使用测试数据库
USE did_service_test;

-- 表结构由GORM AutoMigrate自动创建
```

### 3.3 测试工具准备

| 工具 | 用途 |
|------|------|
| `go test` | 执行单元和集成测试 |
| `kitex` | 生成测试客户端代码 |
| `testify` | 测试断言库 |
| `gomock` | Mock框架（可选） |
| `docker` | 启动测试MySQL（可选） |

## 4. 测试用例设计

### 4.1 CreateDID 测试用例

#### 正常场景

| 用例ID | 用例名称 | 输入 | 预期输出 | 验证点 |
|--------|---------|------|---------|--------|
| TC-C01 | 创建Agent类型DID | UserType=AGENT | DID生成成功，返回did:solana:xxx格式 | DID格式正确、公私钥生成成功 |
| TC-C02 | 创建Developer类型DID | UserType=DEVELOPER | DID生成成功 | UserType正确存储 |
| TC-C03 | 带元数据创建 | Metadata={"name":"test"} | 成功，元数据保存 | 元数据完整性 |

#### 异常场景

| 用例ID | 用例名称 | 输入 | 预期输出 | 验证点 |
|--------|---------|------|---------|--------|
| TC-C04 | 重复创建相同DID | 同一公钥二次创建 | 返回"already exists"错误 | 唯一性约束 |
| TC-C05 | 数据库连接失败 | MySQL断开 | 返回数据库错误 | 错误处理 |

### 4.2 GetDID 测试用例

#### 正常场景

| 用例ID | 用例名称 | 输入 | 预期输出 | 验证点 |
|--------|---------|------|---------|--------|
| TC-G01 | 查询存在的DID | 有效的did字符串 | 返回完整DID信息 | 数据准确性 |
| TC-G02 | 查询不同状态DID | active/disabled/revoked | 状态字段正确 | 状态一致性 |

#### 异常场景

| 用例ID | 用例名称 | 输入 | 预期输出 | 验证点 |
|--------|---------|------|---------|--------|
| TC-G03 | 查询不存在的DID | 随机did字符串 | 返回"not found"错误 | 空值处理 |
| TC-G04 | 无效DID格式 | "invalid:did" | 返回"invalid did"错误 | 格式校验 |

### 4.3 VerifySignature 测试用例

#### 正常场景

| 用例ID | 用例名称 | 输入 | 预期输出 | 验证点 |
|--------|---------|------|---------|--------|
| TC-V01 | 验证有效签名 | 正确私钥签名+当前时间戳+新nonce | Valid=true | Ed25519验证成功 |
| TC-V02 | 验证不同消息签名 | message="test2" | Valid=true | 消息绑定正确 |

#### 异常场景（安全测试）

| 用例ID | 用例名称 | 输入 | 预期输出 | 验证点 |
|--------|---------|------|---------|--------|
| TC-V03 | 无效签名（篡改消息） | 签名与消息不匹配 | Valid=false | 防篡改 |
| TC-V04 | 过期时间戳 | timestamp=10分钟前 | Valid=false | 时间窗口保护 |
| TC-V05 | 重复使用nonce | 相同nonce二次验证 | Valid=false | 重放攻击防护 |
| TC-V06 | 伪造公钥验证 | 使用其他DID的公钥 | Valid=false | 身份绑定 |
| TC-V07 | 无效DID状态 | DID状态=disabled | Valid=false | 状态检查 |
| TC-V08 | 畸形签名数据 | signature="invalid" | Valid=false | 输入校验 |
| TC-V09 | 畸形公钥数据 | 公钥长度≠32字节 | Valid=false | 公钥校验 |

## 5. 测试代码结构

```
app/
├── did_app_service.go
├── did_app_service_test.go          # 单元测试（Mock）
└── did_app_service_integration_test.go  # 集成测试（真实DB）

cmd/server/
└── e2e_test.go                      # 端到端测试（完整服务）

test/
├── helper.go                        # 测试工具函数
├── fixtures/                        # 测试数据
│   ├── dids.json
│   └── keys.json
└── docker-compose.yml               # 测试环境MySQL
```

## 6. 关键测试代码示例

### 6.1 单元测试（Mock仓储）

```go
// did_app_service_test.go
func TestDIDAppService_CreateDID_Success(t *testing.T) {
    // 1. Mock仓储
    mockRepo := new(mockDIDRepository)
    mockRepo.On("Exists", mock.Anything, mock.Anything).Return(false, nil)
    mockRepo.On("Save", mock.Anything, mock.Anything).Return(nil)
    
    // 2. 创建服务
    svc, _ := NewDIDAppService(mockRepo, "test_key_32_bytes_for_aes_256!!")
    
    // 3. 执行
    result, err := svc.CreateDID(context.Background(), &CreateDIDCmd{
        UserType: UserTypeAgent,
    })
    
    // 4. 验证
    assert.NoError(t, err)
    assert.NotEmpty(t, result.DIDString)
    assert.True(t, strings.HasPrefix(result.DIDString, "did:solana:"))
}
```

### 6.2 集成测试（真实数据库）

```go
// did_app_service_integration_test.go
func TestDIDAppServiceIntegration_VerifySignature(t *testing.T) {
    // 1. 连接测试数据库
    db, _ := gorm.Open(mysql.Open(testDSN), &gorm.Config{})
    repo := repository.NewDBDIDRepository(db)
    svc, _ := NewDIDAppService(repo, testKey)
    
    // 2. 创建测试DID
    createResult, _ := svc.CreateDID(context.Background(), &CreateDIDCmd{
        UserType: UserTypeAgent,
    })
    
    // 3. 获取私钥（解密后）
    privateKey, _ := svc.GetPrivateKey(context.Background(), createResult.DIDString)
    
    // 4. 构造签名
    message := "test message"
    timestamp := time.Now().Format(time.RFC3339)
    nonce := "random_nonce_123"
    signData := fmt.Sprintf("%s%s%s", message, timestamp, nonce)
    signature := ed25519Sign(privateKey, signData) // 测试辅助函数
    
    // 5. 验证签名
    result, err := svc.VerifySignature(context.Background(), &VerifySignatureCmd{
        DIDString: createResult.DIDString,
        Message:   message,
        Signature: signature,
        Timestamp: timestamp,
        Nonce:     nonce,
    })
    
    // 6. 验证
    assert.NoError(t, err)
    assert.True(t, result.Valid)
}
```

### 6.3 端到端测试（完整服务）

```go
// e2e_test.go
func TestE2E_CreateAndGetDID(t *testing.T) {
    // 1. 启动服务（或使用已启动的服务）
    client, _ := didservice.NewClient("did-service", client.WithHostPorts("127.0.0.1:18081"))
    
    // 2. 创建DID
    createResp, err := client.CreateDID(context.Background(), &did_service.CreateDIDRequest{
        UserType: did_service.UserType_AGENT,
    })
    assert.NoError(t, err)
    assert.Equal(t, int32(0), createResp.Base.Code)
    
    // 3. 查询DID
    getResp, err := client.GetDID(context.Background(), &did_service.GetDIDRequest{
        Did: createResp.Did,
    })
    assert.NoError(t, err)
    assert.Equal(t, createResp.Did, getResp.Did)
    assert.Equal(t, createResp.PublicKey, getResp.PublicKey)
}
```

### 6.4 安全测试（重放攻击）

```go
func TestVerifySignature_ReplayAttack(t *testing.T) {
    svc, _ := NewDIDAppService(repo, testKey)
    
    // 创建DID并获取私钥...
    
    // 第一次验证
    result1, _ := svc.VerifySignature(ctx, &VerifySignatureCmd{...})
    assert.True(t, result1.Valid)
    
    // 使用相同nonce再次验证（重放攻击）
    result2, _ := svc.VerifySignature(ctx, &VerifySignatureCmd{...})
    assert.False(t, result2.Valid) // 应被拒绝
}
```

## 7. 测试执行命令

```bash
# 1. 运行单元测试（快速，无外部依赖）
go test ./app/... -v -run "TestDIDAppService_" -short

# 2. 运行集成测试（需要MySQL）
# 先启动测试数据库
docker-compose -f test/docker-compose.yml up -d
go test ./app/... -v -run "TestDIDAppServiceIntegration_" 

# 3. 运行端到端测试（需要完整服务）
# 先启动服务
go run ./cmd/server/main.go
go test ./cmd/server/... -v -run "TestE2E_"

# 4. 运行全部测试
go test ./... -v

# 5. 生成测试覆盖率报告
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

## 8. 测试数据清理策略

```go
// test/helper.go
func CleanupTestData(db *gorm.DB) {
    // 清理测试产生的数据
    db.Exec("DELETE FROM did_identities WHERE did LIKE 'did:solana:test_%'")
}

// 每个测试后清理
defer CleanupTestData(db)
```

## 9. CI/CD集成建议

```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    services:
      mysql:
        image: mysql:8.0
        env:
          MYSQL_ROOT_PASSWORD: password
          MYSQL_DATABASE: did_service_test
    steps:
      - uses: actions/checkout@v2
      - uses: actions/setup-go@v2
      - run: go test ./... -v
```

## 10. 测试通过标准

| 指标 | 目标值 |
|------|--------|
| 单元测试覆盖率 | ≥ 70% |
| 集成测试覆盖率 | ≥ 60% |
| 关键路径测试 | 100% |
| 安全场景测试 | 全部通过 |
| 测试执行时间 | < 2分钟 |

---

**下一步行动**：
1. [ ] 创建测试配置文件
2. [ ] 编写单元测试代码
3. [ ] 编写集成测试代码
4. [ ] 编写端到端测试代码
5. [ ] 配置CI/CD流水线

是否需要我立即实现测试代码？
