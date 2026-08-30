package tcc

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// PaymentParticipant 支付参与者示例
type PaymentParticipant struct {
	name string
}

// NewPaymentParticipant 创建支付参与者
func NewPaymentParticipant(name string) *PaymentParticipant {
	return &PaymentParticipant{name: name}
}

// GetName 返回参与者名称
func (p *PaymentParticipant) GetName() string {
	return p.name
}

// Try 执行尝试操作（冻结资金）
func (p *PaymentParticipant) Try(ctx context.Context, transactionID string, context map[string]interface{}) error {
	// 模拟冻结资金操作
	fmt.Printf("参与者 %s 执行Try操作，事务ID: %s\n", p.name, transactionID)

	// 模拟网络延迟
	time.Sleep(100 * time.Millisecond)

	// 模拟可能的失败
	if p.name == "failing_participant" {
		return fmt.Errorf("参与者 %s Try操作失败", p.name)
	}

	fmt.Printf("参与者 %s Try操作成功\n", p.name)
	return nil
}

// Confirm 执行确认操作（扣款）
func (p *PaymentParticipant) Confirm(ctx context.Context, transactionID string, context map[string]interface{}) error {
	// 模拟扣款操作
	fmt.Printf("参与者 %s 执行Confirm操作，事务ID: %s\n", p.name, transactionID)

	// 模拟网络延迟
	time.Sleep(100 * time.Millisecond)

	// 模拟可能的失败
	if p.name == "failing_confirm_participant" {
		return fmt.Errorf("参与者 %s Confirm操作失败", p.name)
	}

	fmt.Printf("参与者 %s Confirm操作成功\n", p.name)
	return nil
}

// Cancel 执行取消操作（解冻资金）
func (p *PaymentParticipant) Cancel(ctx context.Context, transactionID string, context map[string]interface{}) error {
	// 模拟解冻资金操作
	fmt.Printf("参与者 %s 执行Cancel操作，事务ID: %s\n", p.name, transactionID)

	// 模拟网络延迟
	time.Sleep(100 * time.Millisecond)

	fmt.Printf("参与者 %s Cancel操作成功\n", p.name)
	return nil
}

// ExampleUsage 示例用法
func ExampleUsage() {
	// 创建数据库连接
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("连接数据库失败: " + err.Error())
	}

	// 创建存储
	storage := NewGormStorage(db)

	// 创建日志
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	// 创建事务管理器
	manager := NewTransactionManager(storage, logger)

	// 创建协调器
	coordinator := NewCoordinator(manager, logger)

	// 创建参与者
	participants := []TccParticipant{
		NewPaymentParticipant("account_service"),
		NewPaymentParticipant("payment_service"),
		NewPaymentParticipant("notification_service"),
	}

	// 创建上下文
	ctx := context.Background()
	transactionContext := map[string]interface{}{
		"amount":    100.0,
		"from_user": "user123",
		"to_user":   "user456",
		"currency":  "USD",
	}

	// 执行TCC事务
	fmt.Println("=== 开始执行TCC事务 ===")
	err = coordinator.ExecuteTransaction(ctx, participants, transactionContext)
	if err != nil {
		fmt.Printf("TCC事务执行失败: %v\n", err)
	} else {
		fmt.Println("TCC事务执行成功")
	}

	// 测试失败场景
	fmt.Println("\n=== 测试Try阶段失败场景 ===")
	failingParticipants := []TccParticipant{
		NewPaymentParticipant("account_service"),
		NewPaymentParticipant("failing_participant"), // 这个参与者会失败
		NewPaymentParticipant("notification_service"),
	}

	err = coordinator.ExecuteTransaction(ctx, failingParticipants, transactionContext)
	if err != nil {
		fmt.Printf("TCC事务执行失败（预期）: %v\n", err)
	}

	// 测试Confirm阶段失败场景
	fmt.Println("\n=== 测试Confirm阶段失败场景 ===")
	confirmFailingParticipants := []TccParticipant{
		NewPaymentParticipant("account_service"),
		NewPaymentParticipant("failing_confirm_participant"), // 这个参与者在Confirm阶段会失败
		NewPaymentParticipant("notification_service"),
	}

	err = coordinator.ExecuteTransaction(ctx, confirmFailingParticipants, transactionContext)
	if err != nil {
		fmt.Printf("TCC事务执行失败（预期）: %v\n", err)
	}
}

// RunExample 运行示例
func RunExample() {
	fmt.Println("TCC分布式事务示例")
	fmt.Println("==================")
	ExampleUsage()
}
