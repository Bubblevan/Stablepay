# 我在 StablePay 里踩过的 RocketMQ 坑：从 HTTP 402 到购买记录入库的全链路拆解

## 写在前面

说实话，当我第一次听说要在支付系统里引入 RocketMQ 的时候，我内心是抗拒的。

"我们不是已经有 HTTP 接口了吗？Payment Service 直接调 Verification Service 不就完了？为什么还要多塞一个消息队列在中间？"

直到我自己亲手写了那段代码，亲眼看到支付成功后购买记录是怎么异步入库的，我才真正理解：**有些架构设计，不亲手撸一遍代码是感受不到的。**

这篇笔记，我想用第一人称视角，带你从零开始搭建一个 MVP，然后结合 StablePay 真实的微服务代码，把 HTTP 402、Payment Service、RocketMQ、Verification Service 这条链路彻底讲清楚。

---

## 第一章：先搞清楚问题——为什么我需要消息队列？

### 1.1 没有 MQ 的世界：同步调用的痛苦

想象一下，如果我不使用 RocketMQ，Payment Service 在支付成功后要做什么？

```
用户发起支付
    ↓
Payment Service 验签 → 检查余额 → 调用链上交易
    ↓
同步调用 Verification Service 更新购买记录
    ↓
同步调用 Query Service 刷新缓存
    ↓
同步调用 Audit Service 写审计日志
    ↓
返回支付结果给用户
```

**问题在哪里？**

- Verification Service 慢了，用户就得等
- Verification Service 挂了，支付接口也跟着受影响
- 每增加一个下游服务，支付接口就多一个依赖点

### 1.2 引入 MQ 后的世界：发布-订阅模式

有了 RocketMQ 之后，流程变成：

```
用户发起支付
    ↓
Payment Service 验签 → 检查余额 → 调用链上交易 → 发送 MQ 消息
    ↓
立即返回支付结果给用户
    ↓
Verification Service 消费消息 → 更新购买记录
Query Service 消费消息 → 刷新缓存（未来）
Audit Service 消费消息 → 写审计日志（未来）
```

**好处显而易见：**

- Payment Service 不需要等 Verification Service 完成
- 下游服务挂了不影响支付主链路
- 新增服务只需要订阅消息，不需要改 Payment Service

这就是**解耦**的力量。

---

## 第二章：MVP 实战——从零搭建一个最小可用示例

在看我项目的真实代码之前，我们先动手搭一个最简单的 MVP，让 Producer 能发消息，Consumer 能收消息。

### 2.1 启动 RocketMQ

```bash
# 用 Docker 快速启动
mkdir -p ~/rocketmq/data/namesrv/logs ~/rocketmq/data/namesrv/store
mkdir -p ~/rocketmq/data/broker/logs ~/rocketmq/data/broker/store

# 启动 NameServer
docker run -d \
  --name rocketmq-namesrv \
  -p 9876:9876 \
  -v ~/rocketmq/data/namesrv/logs:/root/logs \
  -v ~/rocketmq/data/namesrv/store:/root/store \
  apache/rocketmq:5.1.4 sh mqnamesrv

# 启动 Broker
docker run -d \
  --name rocketmq-broker \
  --link rocketmq-namesrv \
  -p 10911:10911 -p 10909:10909 \
  -v ~/rocketmq/data/broker/logs:/root/logs \
  -v ~/rocketmq/data/broker/store:/root/store \
  apache/rocketmq:5.1.4 sh mqbroker -n rocketmq-namesrv:9876
```

### 2.2 Producer：发送支付成功消息

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    "time"

    "github.com/apache/rocketmq-client-go/v2"
    "github.com/apache/rocketmq-client-go/v2/primitive"
    "github.com/apache/rocketmq-client-go/v2/producer"
)

type PaymentSuccessEvent struct {
    TxID     string `json:"tx_id"`
    AgentDID string `json:"agent_did"`
    SkillDID string `json:"skill_did"`
    Amount   int64  `json:"amount"`
    Currency string `json:"currency"`
}

func main() {
    // 创建 Producer
    p, err := rocketmq.NewProducer(
        producer.WithNameServer([]string{"127.0.0.1:9876"}),
        producer.WithGroupName("payment_producer_group"),
        producer.WithRetry(3),
    )
    if err != nil {
        log.Fatal("创建 Producer 失败:", err)
    }

    // 启动 Producer
    if err := p.Start(); err != nil {
        log.Fatal("启动 Producer 失败:", err)
    }
    defer p.Shutdown()

    // 构造消息
    event := PaymentSuccessEvent{
        TxID:     "pay_123456",
        AgentDID: "did:solana:agent123",
        SkillDID: "did:solana:dev456",
        Amount:   5000000, // 5 USDC (6位小数)
        Currency: "USDC",
    }

    data, _ := json.Marshal(event)
    msg := primitive.NewMessage("payment_events", data).
        WithTag("payment_succeeded").
        WithKeys([]string{event.TxID})

    // 同步发送
    res, err := p.SendSync(context.Background(), msg)
    if err != nil {
        log.Fatal("发送失败:", err)
    }

    log.Printf("消息发送成功! MsgID: %s, Status: %s", res.MsgID, res.Status)
    time.Sleep(1 * time.Second) // 等待消息发送完成
}
```

### 2.3 Consumer：消费消息并入库

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    "time"

    "github.com/apache/rocketmq-client-go/v2"
    "github.com/apache/rocketmq-client-go/v2/consumer"
    "github.com/apache/rocketmq-client-go/v2/primitive"
)

type PaymentSuccessEvent struct {
    TxID     string `json:"tx_id"`
    AgentDID string `json:"agent_did"`
    SkillDID string `json:"skill_did"`
    Amount   int64  `json:"amount"`
    Currency string `json:"currency"`
}

type PurchaseRecord struct {
    ID        uint      `gorm:"primaryKey"`
    AgentDID  string    `gorm:"index"`
    SkillDID  string    `gorm:"index"`
    TxID      string    `gorm:"uniqueIndex"`
    CreatedAt time.Time
}

func main() {
    // 创建 PushConsumer
    c, err := rocketmq.NewPushConsumer(
        consumer.WithNameServer([]string{"127.0.0.1:9876"}),
        consumer.WithGroupName("verification_group"),
    )
    if err != nil {
        log.Fatal("创建 Consumer 失败:", err)
    }

    // 订阅 Topic
    err = c.Subscribe("payment_events", consumer.MessageSelector{},
        func(ctx context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
            for _, msg := range msgs {
                log.Printf("收到消息: %s", string(msg.Body))

                var event PaymentSuccessEvent
                if err := json.Unmarshal(msg.Body, &event); err != nil {
                    log.Printf("解析失败: %v", err)
                    continue
                }

                // 模拟入库操作
                record := PurchaseRecord{
                    AgentDID: event.AgentDID,
                    SkillDID: event.SkillDID,
                    TxID:     event.TxID,
                }
                log.Printf("购买记录已创建: %+v", record)
            }
            return consumer.ConsumeSuccess, nil
        })
    if err != nil {
        log.Fatal("订阅失败:", err)
    }

    // 启动 Consumer
    if err := c.Start(); err != nil {
        log.Fatal("启动失败:", err)
    }

    log.Println("Consumer 启动成功，等待消息...")
    select {} // 阻塞主线程
}
```

### 2.4 运行 MVP 验证

```bash
# 终端 1：启动 Consumer
go run consumer.go

# 终端 2：运行 Producer
go run producer.go
```

如果一切正常，你会看到：
- Producer 端输出：`消息发送成功! MsgID: xxx`
- Consumer 端输出：`收到消息: {...}` 和 `购买记录已创建: {...}`

**这就是最基础的发布-订阅模式。** 但真实的微服务场景远比这个复杂，接下来让我带你看看 StablePay 里的真实代码。

---

## 第三章：深入 StablePay——真实的支付链路长什么样？

### 3.1 整体架构图

```
┌─────────────────┐
│   API Gateway   │
└────────┬────────┘
         │ GET /api/v1/pay/require
         │ (检查是否已购买，返回 HTTP 402)
         ▼
┌─────────────────────────┐
│    Payment Service      │
│  ┌───────────────────┐  │
│  │  PaymentHandler   │  │
│  └─────────┬─────────┘  │
│            │
│  ┌─────────▼─────────┐  │
│  │ PaymentApplication│  │
│  │     Service       │  │
│  └─────────┬─────────┘  │
│            │
│  ┌─────────▼─────────┐  │
│  │ PaymentEvent      │  │
│  │   Producer        │──┼──► RocketMQ (payment_events topic)
│  └───────────────────┘  │
└─────────────────────────┘
                           │
                           ▼
┌─────────────────────────┐
│  Verification Service   │
│  ┌───────────────────┐  │
│  │  MQ Consumer      │  │
│  └─────────┬─────────┘  │
│            │
│  ┌─────────▼─────────┐  │
│  │ PurchaseRecord    │  │
│  │    (MySQL)        │  │
│  └───────────────────┘  │
└─────────────────────────┘
```

### 3.2 HTTP 402 支付流程入口

在我的 StablePay 项目里，支付流程通常是这样触发的：

**Step 1: Agent 访问 Skill 的 API**

```
GET /api/v1/skill/some-feature
Authorization: Bearer <agent_token>
```

**Step 2: API Gateway 发现未购买，返回 HTTP 402**

```http
HTTP/1.1 402 Payment Required
Content-Type: application/json

{
    "code": 402,
    "message": "Payment Required",
    "data": {
        "skill_did": "did:solana:dev456",
        "skill_name": "OpenClaw Skill",
        "price": "5.00",
        "currency": "USDC",
        "message": "Payment required to access this skill",
        "payment_endpoint": "/api/v1/pay"
    }
}
```

这段逻辑在 `payment-service/internal/adapter/http/handler/payment_handler.go` 里：

```go
// GetPaymentRequirement 获取支付要求（HTTP 402）
func (h *PaymentHandler) GetPaymentRequirement(ctx context.Context, c *app.RequestContext) {
    var req dto.GetPaymentRequirementRequest
    if err := c.BindAndValidate(&req); err != nil {
        h.respondError(c, errors.New(errors.INVALID_PARAMETERS, err.Error()))
        return
    }

    resp, alreadyPurchased, err := h.paymentService.GetPaymentRequirement(ctx, &req)
    if err != nil {
        h.respondError(c, err)
        return
    }

    if alreadyPurchased {
        // 已购买，返回 200
        h.respondWithCode(c, http.StatusOK, 0, "Already purchased", ...)
        return
    }

    // 未购买，返回 HTTP 402
    h.respondWithCode(c, http.StatusPaymentRequired, 402, "Payment Required", resp)
}
```

**Step 3: Agent 发起支付请求**

```http
POST /api/v1/pay
X-Idempotency-Key: unique-key-123
Content-Type: application/json

{
    "agent_did": "did:solana:agent123",
    "skill_did": "did:solana:dev456",
    "amount": "5.00",
    "currency": "USDC",
    "signature": "base64_encoded_signature",
    "timestamp": 1704067200,
    "nonce": "random_nonce",
    "signed_tx_base64": "base64_encoded_partially_signed_tx"
}
```

### 3.3 Payment Service 的核心逻辑

支付服务的核心在 `payment-service/internal/application/service/payment_service.go`，让我把关键逻辑抽出来讲：

```go
// InitiatePayment 发起支付
func (s *PaymentApplicationService) InitiatePayment(ctx context.Context, req *dto.InitiatePaymentRequest) (*dto.InitiatePaymentResponse, error) {
    // 1. 参数校验：金额格式、签名、余额检查
    // ...

    // 2. 创建支付记录（MySQL）
    payment, _ := entity.NewPayment(txID, req.AgentDID, req.SkillDID, ...)
    s.paymentRepo.Create(ctx, payment)

    // 3. 执行链上交易（同步调用 blockchain-adapter）
    txHash, err := s.blockchainExec.ExecuteTransfer(ctx, fromWallet, toWallet, ...)
    if err != nil {
        payment.MarkAsFailed("BLOCKCHAIN_ERROR", err.Error())
        s.paymentRepo.Update(ctx, payment)
        s.publishEvent(ctx, payment)  // 发送失败事件
        return nil, err
    }

    // 4. 更新为交易中状态
    payment.MarkAsPending(txHash)
    s.paymentRepo.Update(ctx, payment)

    // 5. 启动后台 goroutine 轮询链上状态
    go s.pollTxStatus(payment.TxID, txHash)

    return s.toResponse(payment), nil
}
```

**关键点：为什么链上交易要异步轮询？**

因为区块链交易不是立即确认的。我发起一笔转账后，需要等待区块确认。所以在 `pollTxStatus` 里，我会每隔几秒查询一次交易状态：

```go
func (s *PaymentApplicationService) pollTxStatus(txID, txHash string) {
    for i := 0; i < s.config.MaxPollCount; i++ {
        time.Sleep(time.Duration(s.config.PollIntervalSeconds) * time.Second)

        status, confirmedAt, err := s.blockchainExec.QueryTxStatus(ctx, txHash)
        
        switch status {
        case constants.PaymentStatusConfirmed:
            // 交易确认成功
            payment.MarkAsConfirmed()
            s.paymentRepo.Update(ctx, payment)
            
            // 标记为完成
            payment.MarkAsCompleted()
            s.paymentRepo.Update(ctx, payment)
            
            // 发送 MQ 消息！
            s.publishEvent(ctx, payment)
            return
            
        case constants.PaymentStatusFailed:
            payment.MarkAsFailed("BLOCKCHAIN_FAILED", "...")
            s.paymentRepo.Update(ctx, payment)
            s.publishEvent(ctx, payment)
            return
        }
    }
}
```

### 3.4 发送 MQ 消息：PaymentEventProducer

`payment-service/internal/adapter/mq/producer.go`：

```go
type PaymentEventProducer struct {
    producer rocketmq.Producer
    topic    string
    logger   *zap.Logger
}

func (p *PaymentEventProducer) PublishPaymentEvent(ctx context.Context, event *dto.MQPaymentEvent) error {
    data, err := json.Marshal(event)
    if err != nil {
        return errors.Wrap(errors.INTERNAL_SERVER_ERROR, err, "failed to marshal event")
    }

    // 根据状态选择 Tag
    tag := eventTagFromStatus(event.Status)
    
    msg := primitive.NewMessage(p.topic, data).
        WithTag(tag).
        WithKeys([]string{event.TxID})  // 用 TxID 作为消息 Key，便于追踪

    // 同步发送，确保消息发送成功
    res, err := p.producer.SendSync(ctx, msg)
    if err != nil {
        p.logger.Error("failed to send mq message", zap.Error(err), zap.String("tx_id", event.TxID))
        return err
    }

    if res.Status != primitive.SendOK {
        return errors.Newf(errors.INTERNAL_SERVER_ERROR, "mq send failed with status: %s", res.Status)
    }

    p.logger.Info("payment event published",
        zap.String("tx_id", event.TxID),
        zap.String("status", event.Status),
        zap.String("msg_id", res.MsgID),
    )
    return nil
}
```

**注意 Tag 的设计：**

```go
func eventTagFromStatus(status string) string {
    switch strings.ToUpper(strings.TrimSpace(status)) {
    case "CONFIRMED", "COMPLETED":
        return "payment_succeeded"
    case "FAILED", "CANCELLED":
        return "payment_failed"
    default:
        return ""
    }
}
```

这样 Consumer 可以根据 Tag 过滤消息，只处理自己关心的类型。

### 3.5 消费 MQ 消息：Verification Service

`verification-service/consumer.go`：

```go
type PaymentSuccessEvent struct {
    AgentDid string `json:"agent_did"`
    SkillDid string `json:"skill_did"`
    TxId     string `json:"tx_id"`
}

func StartMQConsumer() {
    c, err := rocketmq.NewPushConsumer(
        consumer.WithGroupName("verification_group"),
        consumer.WithNameServer([]string{"127.0.0.1:9876"}),
    )
    if err != nil {
        log.Printf("⚠️ 创建 MQ 消费者失败: %v", err)
        return
    }

    err = c.Subscribe("payment_events", consumer.MessageSelector{}, 
        func(ctx context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
            for _, msg := range msgs {
                log.Printf("📥 收到支付消息: %s", msg.Body)

                var event PaymentSuccessEvent
                if err := json.Unmarshal(msg.Body, &event); err != nil {
                    log.Printf("解析消息失败: %v", err)
                    continue
                }

                // 写入数据库
                record := PurchaseRecord{
                    AgentDid: event.AgentDid,
                    SkillDid: event.SkillDid,
                    TxId:     event.TxId,
                }
                
                if err := DB.Create(&record).Error; err != nil {
                    log.Printf("写入数据库失败: %v", err)
                    return consumer.ConsumeRetryLater, err  // 告诉 MQ 重试
                }

                log.Printf("成功消费支付事件，购买关系已入库！流水号: %s", event.TxId)
            }
            return consumer.ConsumeSuccess, nil
        })

    c.Start()
    log.Println("🎧 RocketMQ 消费者启动成功...")
}
```

### 3.6 验证购买记录

当 Agent 再次访问 Skill 时，API Gateway 会调用 Verification Service 的 RPC 接口：

`verification-service/handler.go`：

```go
func (s *VerificationServiceImpl) VerifyPurchase(ctx context.Context, req *verification_service.VerifyPurchaseRequest) (*verification_service.VerifyPurchaseResponse, error) {
    log.Printf("👉 收到验证请求: AgentDid='%s', SkillDid='%s'", req.AgentDid, req.SkillDid)

    resp := verification_service.NewVerifyPurchaseResponse()
    var record PurchaseRecord

    // 查询数据库
    result := DB.Where("agent_did = ? AND skill_did = ?", req.AgentDid, req.SkillDid).First(&record)

    if result.Error == nil {
        resp.Purchased = true
        resp.PurchaseTime = strPtr("2026-03-12T15:00:00Z")
        resp.TxId = &record.TxId
        resp.Base = &common.BaseResp{Code: 0, Message: "success"}
    } else {
        resp.Purchased = false
        resp.Base = &common.BaseResp{Code: 10001, Message: "record not found"}
    }

    return resp, nil
}
```

---

## 第四章：踩坑实录——我在实现过程中遇到的问题

### 4.1 坑 1：消息发送成功但消费端收不到

**现象：** Producer 日志显示发送成功，但 Consumer 端没有任何反应。

**排查过程：**
1. 检查 Topic 名称是否一致（大小写敏感！）
2. 检查 Consumer Group 是否被其他消费者占用了
3. 检查 NameServer 地址是否正确

**解决方案：**
```go
// 错误示例：用了不同的 Topic 名
producer: topic = "payment-events"   // 带横线
consumer: topic = "payment_events"   // 带下划线

// 正确做法：统一命名规范，全用小写+下划线
const TopicPaymentEvents = "payment_events"
```

### 4.2 坑 2：消息重复消费

**现象：** 同一条支付记录被插入了两次。

**原因：** RocketMQ 的 At-Least-Once 投递语义决定了消息可能被重复投递。

**解决方案：幂等性设计**

在数据库层面加唯一索引：

```go
type PurchaseRecord struct {
    gorm.Model
    AgentDid string `gorm:"index"`
    SkillDid string `gorm:"index"`
    TxId     string `gorm:"uniqueIndex"`  // 唯一索引，防止重复
}
```

在代码层面先查后插：

```go
func (s *VerificationServiceImpl) handlePaymentEvent(event *PaymentSuccessEvent) error {
    // 先查询，已存在则直接返回
    var existing PurchaseRecord
    result := DB.Where("tx_id = ?", event.TxId).First(&existing)
    if result.Error == nil {
        log.Printf("记录已存在，跳过处理: %s", event.TxId)
        return nil  // 幂等：已经处理过了
    }

    // 不存在则插入
    record := PurchaseRecord{...}
    return DB.Create(&record).Error
}
```

### 4.3 坑 3：消费失败没有重试

**现象：** 数据库连接断开后，消费端没有重试，消息丢失了。

**解决方案：** 正确处理消费返回值

```go
func (s *Consumer) Consume(ctx context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
    for _, msg := range msgs {
        if err := s.process(msg); err != nil {
            // 处理失败，告诉 MQ 稍后重试
            return consumer.ConsumeRetryLater, err
        }
    }
    // 全部成功
    return consumer.ConsumeSuccess, nil
}
```

RocketMQ 默认会重试 16 次，间隔时间逐渐拉长：1s, 5s, 10s, 30s, 1m, 2m, 3m, 4m, 5m, 6m, 7m, 8m, 9m, 10m, 20m, 30m。

### 4.4 坑 4：消息积压导致消费延迟

**现象：** 支付成功几分钟后，购买记录才入库。

**原因：** 单线程消费处理不过来。

**解决方案：**

1. 增加 Consumer 实例（同一个 Consumer Group）
2. 使用并发消费：

```go
c, _ := rocketmq.NewPushConsumer(
    consumer.WithNameServer([]string{"127.0.0.1:9876"}),
    consumer.WithGroupName("verification_group"),
    consumer.WithConsumeMessageBatchMaxSize(10),  // 一次拉取 10 条
    consumer.WithConsumeConcurrentlyMaxSpan(20),  // 并发数
)
```

---

## 第五章：进阶话题——事务消息与最终一致性

### 5.1 为什么要用事务消息？

考虑这个场景：
1. 支付记录更新为 "CONFIRMED"
2. 发送 MQ 消息

如果在第 1 步和第 2 步之间服务宕机，就会出现：**数据库显示支付成功，但 MQ 消息没发出去。**

### 5.2 RocketMQ 事务消息原理

```
Producer                    Broker                    Consumer
   |                          |                          |
   |--- 1. 发送半消息 -------->|                          |
   |                          |                          |
   |<-- 2. 半消息发送成功 -----|                          |
   |                          |                          |
   |--- 3. 执行本地事务 -------|                          |
   |    (更新支付状态)         |                          |
   |                          |                          |
   |--- 4. 提交/回滚 --------->|                          |
   |                          |                          |
   |                          |---- 5. 投递消息 --------->|
   |                          |                          |
   |<-- 6. 回查请求 -----------|                          | (如果第4步没响应)
   |                          |                          |
   |--- 7. 返回事务状态 ------>|
```

### 5.3 StablePay 中的事务消息实践

在我的项目中，为了简化实现，我采用了**本地消息表**模式：

```go
// 1. 支付表增加消息发送状态字段
type Payment struct {
    TxID           string
    Status         int8
    EventPublished bool  // 消息是否已发送
    EventPublishTime *time.Time
}

// 2. 定时任务补偿
func (s *PaymentApplicationService) compensateUnpublishedEvents() {
    payments := s.paymentRepo.GetUnpublishedEvents(100)
    
    for _, payment := range payments {
        err := s.publishEvent(ctx, payment)
        if err == nil {
            s.paymentRepo.MarkEventPublished(payment.TxID)
        }
    }
}
```

虽然没有用 RocketMQ 的原生事务消息，但通过本地消息表 + 定时补偿，也能实现最终一致性。

---

## 第六章：总结与展望

### 6.1 核心知识点回顾

| 概念 | 说明 |
|------|------|
| Topic | 消息主题，一类消息的集合 |
| Tag | 消息标签，用于 Consumer 过滤 |
| Producer Group | 生产者组，同一组的 Producer 逻辑一致 |
| Consumer Group | 消费者组，组内消费者负载均衡消费 |
| 幂等性 | 同一消息消费多次，结果一致 |
| 最终一致性 | 允许短暂不一致，最终达到一致 |

### 6.2 我学到的经验

1. **不要过度设计**：MVP 阶段先用最简单的发布-订阅，等真有事务需求再上事务消息
2. **监控很重要**：消息堆积、消费延迟、失败重试次数都需要监控
3. **幂等性是必选项**：Consumer 端一定要做幂等处理
4. **日志要详细**：TxID、MsgID、消费时间都要记录，便于排查问题

### 6.3 未来优化方向

1. **延时消息**：用于超时取消订单
2. **顺序消息**：确保同一个用户的支付按顺序处理
3. **消息轨迹**：追踪消息从发送到消费的全链路
4. **死信队列**：处理一直消费失败的消息

---

## 写在最后

从最开始抗拒引入 RocketMQ，到现在主动思考还能用它解决什么问题，这个转变让我深刻体会到：**技术选型不是拍脑袋决定的，而是要在具体业务场景中去感受。**

如果你也在学习消息队列，我建议：
1. 先动手跑通一个 MVP
2. 再结合真实业务场景看代码
3. 最后自己写一遍，踩一遍坑

希望这篇笔记对你有帮助。有问题欢迎交流！

---

**参考代码：**
- Payment Service Producer: `payment-service/internal/adapter/mq/producer.go`
- Payment Service Handler: `payment-service/internal/adapter/http/handler/payment_handler.go`
- Payment Service Core: `payment-service/internal/application/service/payment_service.go`
- Verification Service Consumer: `verification-service/consumer.go`
- Verification Service Handler: `verification-service/handler.go`
