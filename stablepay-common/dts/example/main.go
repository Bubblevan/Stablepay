package main

import (
	"context"
	"fmt"
	"log"

	"code.wenfu.cn/stablepay/stablepay-common/dts"
	"github.com/apache/rocketmq-clients/golang/v5"
)

// ==================== 业务实体定义 ====================

// PaymentOrder 支付订单实体
type PaymentOrder struct {
	PaymentID string  `json:"payment_id"`
	Amount    float64 `json:"amount"`
	Status    int     `json:"status"`
	PayerID   string  `json:"payer_id"`
	PayeeID   string  `json:"payee_id"`
	CreatedAt string  `json:"created_at"`
}

// Account 账户实体
type Account struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
	Status  int    `json:"status"`
}

// ==================== 业务处理器实现 ====================

// PaymentOrderHandler 支付订单处理器
type PaymentOrderHandler struct {
	repo *PaymentOrderRepository
}

func NewPaymentOrderHandler(repo *PaymentOrderRepository) *PaymentOrderHandler {
	return &PaymentOrderHandler{repo: repo}
}

// Handle 实现 dts.Handler 接口
func (h *PaymentOrderHandler) Handle(ctx context.Context, event *dts.DTSEventAdapter) error {
	switch event.GetType() {
	case dts.OperationInsert, dts.OperationUpdate:
		after := event.GetAfterImage()
		if after == nil {
			return fmt.Errorf("after image is nil")
		}

		// 数据映射
		order, err := h.mapToPaymentOrder(after)
		if err != nil {
			// 返回数据映射错误，让上层跳过而不是重试
			return dts.NewDataMappingError("payment_order", "", "map failed", after, err)
		}

		// 保存到本地数据库
		if err := h.repo.Save(ctx, order); err != nil {
			return fmt.Errorf("failed to save payment order: %w", err)
		}

		fmt.Printf("[PaymentOrder] Saved: ID=%s, Amount=%.2f\n", order.PaymentID, order.Amount)

	case dts.OperationDelete:
		before := event.GetBeforeImage()
		if before == nil {
			return fmt.Errorf("before image is nil")
		}

		paymentID, ok := dts.GetStringValue(before, "payment_id")
		if !ok {
			return dts.NewDataMappingError("payment_order", "payment_id", "field not found", before, nil)
		}

		if err := h.repo.Delete(ctx, paymentID); err != nil {
			return fmt.Errorf("failed to delete payment order: %w", err)
		}

		fmt.Printf("[PaymentOrder] Deleted: ID=%s\n", paymentID)
	}

	return nil
}

func (h *PaymentOrderHandler) mapToPaymentOrder(data map[string]any) (*PaymentOrder, error) {
	// 预处理数值字段（DTS传输的数字可能是字符串）
	data = dts.PreprocessNumericFields(data, []string{"amount", "status", "balance"})

	// 使用类型断言或JSON序列化映射数据
	order := &PaymentOrder{}

	if v, ok := dts.GetStringValue(data, "payment_id"); ok {
		order.PaymentID = v
	}

	if v, ok := dts.GetFloat64Value(data, "amount"); ok {
		order.Amount = v
	}

	if v, ok := dts.GetInt64Value(data, "status"); ok {
		order.Status = int(v)
	}

	// ... 其他字段映射

	return order, nil
}

// AccountHandler 账户处理器
type AccountHandler struct {
	repo *AccountRepository
}

func NewAccountHandler(repo *AccountRepository) *AccountHandler {
	return &AccountHandler{repo: repo}
}

func (h *AccountHandler) Handle(ctx context.Context, event *dts.DTSEventAdapter) error {
	switch event.GetType() {
	case dts.OperationInsert, dts.OperationUpdate:
		after := event.GetAfterImage()
		if after == nil {
			return nil
		}

		account := &Account{}
		if v, ok := dts.GetStringValue(after, "id"); ok {
			account.ID = v
		}
		if v, ok := dts.GetInt64Value(after, "balance"); ok {
			account.Balance = v
		}
		if v, ok := dts.GetInt64Value(after, "status"); ok {
			account.Status = int(v)
		}

		if err := h.repo.Save(ctx, account); err != nil {
			return fmt.Errorf("failed to save account: %w", err)
		}

		fmt.Printf("[Account] Saved: ID=%s, Balance=%d\n", account.ID, account.Balance)

	case dts.OperationDelete:
		before := event.GetBeforeImage()
		if before == nil {
			return nil
		}

		id, _ := dts.GetStringValue(before, "id")
		if err := h.repo.Delete(ctx, id); err != nil {
			return fmt.Errorf("failed to delete account: %w", err)
		}

		fmt.Printf("[Account] Deleted: ID=%s\n", id)
	}

	return nil
}

// ==================== 模拟仓储层 ====================

type PaymentOrderRepository struct{}

func (r *PaymentOrderRepository) Save(ctx context.Context, order *PaymentOrder) error {
	// 实际实现：写入本地数据库
	return nil
}

func (r *PaymentOrderRepository) Delete(ctx context.Context, paymentID string) error {
	// 实际实现：从本地数据库删除
	return nil
}

type AccountRepository struct{}

func (r *AccountRepository) Save(ctx context.Context, account *Account) error {
	return nil
}

func (r *AccountRepository) Delete(ctx context.Context, id string) error {
	return nil
}

// ==================== 表路由 ====================

// TableRouter 创建表路由函数
func TableRouter(
	paymentHandler *PaymentOrderHandler,
	accountHandler *AccountHandler,
) dts.TableRouter {
	return func(tableName string) (dts.Handler, bool) {
		switch tableName {
		case "payment_order":
			return paymentHandler, true
		case "accounts":
			return accountHandler, true
		default:
			return nil, false
		}
	}
}

// ==================== 自定义日志 ====================

// SimpleLogger 简单的日志实现
type SimpleLogger struct{}

func (l *SimpleLogger) Info(ctx context.Context, msg string, fields map[string]interface{}) {
	log.Printf("[INFO] %s %+v\n", msg, fields)
}

func (l *SimpleLogger) Error(ctx context.Context, msg string, fields map[string]interface{}) {
	log.Printf("[ERROR] %s %+v\n", msg, fields)
}

func (l *SimpleLogger) Warn(ctx context.Context, msg string, fields map[string]interface{}) {
	log.Printf("[WARN] %s %+v\n", msg, fields)
}

func (l *SimpleLogger) Debug(ctx context.Context, msg string, fields map[string]interface{}) {
	log.Printf("[DEBUG] %s %+v\n", msg, fields)
}

// ==================== 主函数 ====================

func main() {
	// 1. 初始化仓储层
	paymentRepo := &PaymentOrderRepository{}
	accountRepo := &AccountRepository{}

	// 2. 创建业务处理器
	paymentHandler := NewPaymentOrderHandler(paymentRepo)
	accountHandler := NewAccountHandler(accountRepo)

	// 3. 创建表路由
	router := TableRouter(paymentHandler, accountHandler)

	// 4. 创建日志（可选，传 nil 则不记录日志）
	logger := &SimpleLogger{}

	// 5. 创建 DTS 消息处理器
	handler := dts.NewMessageHandler(router, logger)

	// 6. 集成到你的 MQ 消费者
	// 示例：RocketMQ 消费者
	consumeFunc := func(ctx context.Context, messages ...*golang.MessageView) error {
		for _, msg := range messages {
			if err := handler.HandleMessage(ctx, msg); err != nil {
				// 返回错误触发 MQ 重试
				return err
			}
		}
		return nil
	}

	// 实际使用时，将 consumeFunc 注册到你的 MQ 客户端
	_ = consumeFunc

	fmt.Println("DTS Consumer initialized. Example:")
	fmt.Println()
	fmt.Println("  handler := dts.NewMessageHandler(router, logger)")
	fmt.Println("  err := handler.HandleMessage(ctx, messageView)")
	fmt.Println()
	fmt.Println("Usage steps:")
	fmt.Println("  1. Create handlers for each table you want to sync")
	fmt.Println("  2. Define a TableRouter to route events to handlers")
	fmt.Println("  3. Create a MessageHandler with the router and logger")
	fmt.Println("  4. Call HandleMessage when receiving MQ messages")
}
