package tcc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// MockParticipant 模拟参与者
type MockParticipant struct {
	name          string
	tryResult     error
	confirmResult error
	cancelResult  error
}

func (m *MockParticipant) GetName() string {
	return m.name
}

func (m *MockParticipant) Try(ctx context.Context, transactionID string, context map[string]interface{}) error {
	return m.tryResult
}

func (m *MockParticipant) Confirm(ctx context.Context, transactionID string, context map[string]interface{}) error {
	return m.confirmResult
}

func (m *MockParticipant) Cancel(ctx context.Context, transactionID string, context map[string]interface{}) error {
	return m.cancelResult
}

func TestNewTransaction(t *testing.T) {
	participants := []TccParticipant{
		&MockParticipant{name: "participant1"},
		&MockParticipant{name: "participant2"},
	}
	context := map[string]interface{}{
		"test": "value",
	}
	timeout := 5 * time.Minute

	transaction := NewTransaction(participants, context, timeout)

	assert.NotEmpty(t, transaction.ID)
	assert.Equal(t, StatusPending, transaction.Status)
	assert.Len(t, transaction.Participants, 2)
	assert.Equal(t, "participant1", transaction.Participants[0].Name)
	assert.Equal(t, "participant2", transaction.Participants[1].Name)
	// Context现在是JSON字符串
	expectedContextJSON, _ := json.Marshal(context)
	assert.Equal(t, string(expectedContextJSON), transaction.Context)
}

func TestTransactionManager(t *testing.T) {
	// 创建内存数据库
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	// 创建存储
	storage := NewGormStorage(db)

	// 创建事务管理器
	manager := NewTransactionManager(storage, nil)

	ctx := context.Background()

	// 创建参与者
	participants := []TccParticipant{
		&MockParticipant{name: "participant1"},
		&MockParticipant{name: "participant2"},
	}

	context := map[string]interface{}{
		"test": "value",
	}

	// 测试开始事务
	transaction, err := manager.Begin(ctx, participants, context)
	assert.NoError(t, err)
	assert.NotNil(t, transaction)
	assert.Equal(t, StatusPending, transaction.Status)

	// 测试获取事务
	retrievedTransaction, err := manager.GetTransaction(ctx, transaction.ID)
	assert.NoError(t, err)
	assert.Equal(t, transaction.ID, retrievedTransaction.ID)

	// 测试列出事务
	transactions, err := manager.ListTransactions(ctx, "", 10, 0)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
}

func TestCoordinator(t *testing.T) {
	// 创建内存数据库
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	// 创建存储
	storage := NewGormStorage(db)

	// 创建事务管理器
	manager := NewTransactionManager(storage, nil)

	// 创建协调器
	coordinator := NewCoordinator(manager, nil)

	ctx := context.Background()

	// 创建模拟参与者
	mockParticipant1 := &MockParticipant{
		name:          "participant1",
		tryResult:     nil,
		confirmResult: nil,
		cancelResult:  nil,
	}
	mockParticipant2 := &MockParticipant{
		name:          "participant2",
		tryResult:     nil,
		confirmResult: nil,
		cancelResult:  nil,
	}

	participants := []TccParticipant{mockParticipant1, mockParticipant2}
	context := map[string]interface{}{
		"test": "value",
	}

	// 测试成功场景
	err = coordinator.ExecuteTransaction(ctx, participants, context)
	assert.NoError(t, err)

	// 测试成功，无需额外验证
}

func TestCoordinatorTryFailure(t *testing.T) {
	// 创建内存数据库
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	// 创建存储
	storage := NewGormStorage(db)

	// 创建事务管理器
	manager := NewTransactionManager(storage, nil)

	// 创建协调器
	coordinator := NewCoordinator(manager, nil)

	ctx := context.Background()

	// 创建模拟参与者
	mockParticipant1 := &MockParticipant{
		name:          "participant1",
		tryResult:     nil,
		confirmResult: nil,
		cancelResult:  nil,
	}
	mockParticipant2 := &MockParticipant{
		name:          "participant2",
		tryResult:     assert.AnError, // Try阶段失败
		confirmResult: nil,
		cancelResult:  nil,
	}

	participants := []TccParticipant{mockParticipant1, mockParticipant2}
	context := map[string]interface{}{
		"test": "value",
	}

	// 测试Try阶段失败场景
	err = coordinator.ExecuteTransaction(ctx, participants, context)
	assert.Error(t, err)

	// 测试失败场景，无需额外验证
}

func TestStorage(t *testing.T) {
	// 创建内存数据库
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	// 创建存储
	storage := NewGormStorage(db)

	ctx := context.Background()

	// 创建测试事务
	transaction := &Transaction{
		ID:        "test-transaction",
		Status:    StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
		Context:   `{"test": "value"}`,
	}

	// 测试保存事务
	err = storage.SaveTransaction(ctx, transaction)
	assert.NoError(t, err)

	// 测试获取事务
	retrievedTransaction, err := storage.GetTransaction(ctx, transaction.ID)
	assert.NoError(t, err)
	assert.Equal(t, transaction.ID, retrievedTransaction.ID)
	assert.Equal(t, transaction.Status, retrievedTransaction.Status)

	// 测试更新事务
	transaction.Status = StatusConfirmed
	transaction.UpdatedAt = time.Now()
	err = storage.UpdateTransaction(ctx, transaction)
	assert.NoError(t, err)

	// 验证更新
	updatedTransaction, err := storage.GetTransaction(ctx, transaction.ID)
	assert.NoError(t, err)
	assert.Equal(t, StatusConfirmed, updatedTransaction.Status)
}
