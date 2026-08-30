package tcc

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// TransactionStatus 表示TCC事务的状态
type TransactionStatus string

const (
	StatusPending    TransactionStatus = "PENDING"    // 待处理
	StatusTrying     TransactionStatus = "TRYING"     // 尝试中
	StatusConfirming TransactionStatus = "CONFIRMING" // 确认中
	StatusCancelling TransactionStatus = "CANCELLING" // 取消中
	StatusConfirmed  TransactionStatus = "CONFIRMED"  // 已确认
	StatusCancelled  TransactionStatus = "CANCELLED"  // 已取消
	StatusFailed     TransactionStatus = "FAILED"     // 失败
)

// Transaction 表示一个TCC事务
type Transaction struct {
	ID           string            `json:"id" gorm:"primaryKey"`
	Status       TransactionStatus `json:"status"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	ExpiresAt    time.Time         `json:"expires_at"`
	Context      string            `json:"context" gorm:"type:text"` // 存储为JSON字符串
	Participants []Participant     `json:"participants" gorm:"foreignKey:TransactionID"`
}

// Participant 表示TCC参与者
type Participant struct {
	ID            string            `json:"id" gorm:"primaryKey"`
	TransactionID string            `json:"transaction_id"`
	Name          string            `json:"name"`
	TryURL        string            `json:"try_url"`
	ConfirmURL    string            `json:"confirm_url"`
	CancelURL     string            `json:"cancel_url"`
	Status        TransactionStatus `json:"status"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	Context       string            `json:"context" gorm:"type:text"` // 存储为JSON字符串
	Error         string            `json:"error"`
}

// TccParticipant 定义TCC参与者必须实现的接口
type TccParticipant interface {
	// Try 执行尝试操作
	Try(ctx context.Context, transactionID string, context map[string]interface{}) error

	// Confirm 提交操作
	Confirm(ctx context.Context, transactionID string, context map[string]interface{}) error

	// Cancel 回滚操作
	Cancel(ctx context.Context, transactionID string, context map[string]interface{}) error

	// GetName 返回参与者名称
	GetName() string
}

// TransactionManager 定义TCC事务管理器的接口
type TransactionManager interface {
	// Begin 开始一个新的TCC事务
	Begin(ctx context.Context, participants []TccParticipant, context map[string]interface{}) (*Transaction, error)

	// Commit 提交TCC事务
	Commit(ctx context.Context, transactionID string) error

	// Cancel 取消TCC事务
	Cancel(ctx context.Context, transactionID string) error

	// GetTransaction 根据ID获取事务
	GetTransaction(ctx context.Context, transactionID string) (*Transaction, error)

	// ListTransactions 列出事务（可选过滤）
	ListTransactions(ctx context.Context, status TransactionStatus, limit int, offset int) ([]*Transaction, error)
}

// Storage 定义事务存储的接口
type Storage interface {
	// SaveTransaction 保存事务
	SaveTransaction(ctx context.Context, transaction *Transaction) error

	// UpdateTransaction 更新事务
	UpdateTransaction(ctx context.Context, transaction *Transaction) error

	// GetTransaction 根据ID获取事务
	GetTransaction(ctx context.Context, transactionID string) (*Transaction, error)

	// ListTransactions 列出事务（带过滤）
	ListTransactions(ctx context.Context, status TransactionStatus, limit int, offset int) ([]*Transaction, error)

	// SaveParticipant 保存参与者
	SaveParticipant(ctx context.Context, participant *Participant) error

	// UpdateParticipant 更新参与者
	UpdateParticipant(ctx context.Context, participant *Participant) error

	// GetParticipantsByTransactionID 获取事务的参与者
	GetParticipantsByTransactionID(ctx context.Context, transactionID string) ([]*Participant, error)
}

// NewTransaction 创建新事务
func NewTransaction(participants []TccParticipant, context map[string]interface{}, timeout time.Duration) *Transaction {
	now := time.Now()

	// 序列化Context
	contextJSON, _ := json.Marshal(context)
	contextStr := string(contextJSON)

	transaction := &Transaction{
		ID:           uuid.New().String(),
		Status:       StatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
		ExpiresAt:    now.Add(timeout),
		Context:      contextStr,
		Participants: make([]Participant, 0, len(participants)),
	}

	for _, p := range participants {
		participant := Participant{
			ID:            uuid.New().String(),
			TransactionID: transaction.ID,
			Name:          p.GetName(),
			Status:        StatusPending,
			CreatedAt:     now,
			UpdatedAt:     now,
			Context:       contextStr,
		}
		transaction.Participants = append(transaction.Participants, participant)
	}

	return transaction
}
