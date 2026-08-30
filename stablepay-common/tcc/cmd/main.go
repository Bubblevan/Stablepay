package main

import (
	"context"
	"fmt"
	"log"

	"code.wenfu.cn/stablepay/stablepay-common/tcc"

	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func main() {
	fmt.Println("TCC分布式事务模块演示")
	fmt.Println("====================")

	// 创建数据库连接
	db, err := gorm.Open(sqlite.Open("tcc.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("连接数据库失败:", err)
	}

	// 创建存储
	storage := tcc.NewGormStorage(db)

	// 创建日志
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	// 创建事务管理器
	manager := tcc.NewTransactionManager(storage, logger)

	// 创建协调器
	coordinator := tcc.NewCoordinator(manager, logger)

	// 运行示例
	tcc.RunExample()

	// 演示实际使用
	demonstrateUsage(coordinator)
}

func demonstrateUsage(coordinator *tcc.Coordinator) {
	fmt.Println("\n=== 实际使用演示 ===")

	ctx := context.Background()

	// 创建参与者
	participants := []tcc.TccParticipant{
		tcc.NewPaymentParticipant("account_service"),
		tcc.NewPaymentParticipant("payment_service"),
		tcc.NewPaymentParticipant("notification_service"),
	}

	// 创建事务上下文
	transactionContext := map[string]interface{}{
		"amount":      500.0,
		"from_user":   "alice",
		"to_user":     "bob",
		"currency":    "USD",
		"description": "转账",
	}

	// 执行TCC事务
	fmt.Println("开始执行转账事务...")
	err := coordinator.ExecuteTransaction(ctx, participants, transactionContext)
	if err != nil {
		fmt.Printf("转账事务执行失败: %v\n", err)
	} else {
		fmt.Println("转账事务执行成功！")
	}

	// 查询事务状态
	fmt.Println("\n=== 查询事务状态 ===")
	transactions, err := coordinator.ListTransactions(ctx, "", 10, 0)
	if err != nil {
		fmt.Printf("查询事务失败: %v\n", err)
	} else {
		fmt.Printf("找到 %d 个事务:\n", len(transactions))
		for _, tx := range transactions {
			fmt.Printf("- 事务ID: %s, 状态: %s, 创建时间: %s\n",
				tx.ID, tx.Status, tx.CreatedAt.Format("2006-01-02 15:04:05"))
		}
	}
}
