// Package mq MQ 生产者实现
package mq

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
	"github.com/stablepay/payment-service/internal/application/dto"
	"github.com/stablepay/payment-service/pkg/constants"
	"github.com/stablepay/payment-service/pkg/errors"
	"go.uber.org/zap"
)

// PaymentEventProducer 支付事件生产者
type PaymentEventProducer struct {
	producer   rocketmq.Producer
	topic      string
	logger     *zap.Logger
}

// NewPaymentEventProducer 创建事件生产者
func NewPaymentEventProducer(nameServers []string, producerGroup string, topic string, logger *zap.Logger) (*PaymentEventProducer, error) {
	p, err := rocketmq.NewProducer(
		producer.WithNameServer(nameServers),
		producer.WithGroupName(producerGroup),
		producer.WithRetry(3),
	)
	if err != nil {
		return nil, errors.Wrap(errors.INTERNAL_SERVER_ERROR, err, "failed to create mq producer")
	}

	if err := p.Start(); err != nil {
		return nil, errors.Wrap(errors.INTERNAL_SERVER_ERROR, err, "failed to start mq producer")
	}

	return &PaymentEventProducer{
		producer: p,
		topic:    topic,
		logger:   logger,
	}, nil
}

// PublishPaymentEvent 发布支付事件
func (p *PaymentEventProducer) PublishPaymentEvent(ctx context.Context, event *dto.MQPaymentEvent) error {
	// 序列化事件
	data, err := json.Marshal(event)
	if err != nil {
		return errors.Wrap(errors.INTERNAL_SERVER_ERROR, err, "failed to marshal event")
	}

	// 确定 Tag
	tag := dto.ToMQEventTag(constants.PaymentStatus(event.Status))
	if tag == "" {
		p.logger.Warn("unknown event status, skip publishing", zap.String("status", event.Status))
		return nil
	}

	// 构建消息
	msg := primitive.NewMessage(p.topic, data).
		WithTag(tag).
		WithKeys([]string{event.TxID})

	// 发送消息
	res, err := p.producer.SendSync(ctx, msg)
	if err != nil {
		p.logger.Error("failed to send mq message", zap.Error(err), zap.String("tx_id", event.TxID))
		return errors.Wrap(errors.INTERNAL_SERVER_ERROR, err, "failed to publish event")
	}

	if res.Status != primitive.SendOK {
		p.logger.Error("mq send not ok", zap.String("status", res.Status.String()), zap.String("tx_id", event.TxID))
		return errors.Newf(errors.INTERNAL_SERVER_ERROR, "mq send failed with status: %s", res.Status.String())
	}

	p.logger.Info("payment event published",
		zap.String("tx_id", event.TxID),
		zap.String("status", event.Status),
		zap.String("tag", tag),
		zap.String("msg_id", res.MsgID),
	)

	return nil
}

// Shutdown 关闭生产者
func (p *PaymentEventProducer) Shutdown() error {
	if err := p.producer.Shutdown(); err != nil {
		return fmt.Errorf("failed to shutdown mq producer: %w", err)
	}
	return nil
}

// LocalMessageTable 本地消息表（用于事务消息）
type LocalMessageTable struct {
	// 用于存储待发送的消息
	// 实际实现需要持久化到数据库
}

// TransactionMessage 事务消息
type TransactionMessage struct {
	ID        string
	Topic     string
	Tag       string
	Keys      []string
	Body      []byte
	Status    int // 0=pending, 1=sent, 2=failed
	RetryCount int
	CreatedAt int64
	UpdatedAt int64
}

// Ensure PaymentEventProducer 实现 EventPublisher 接口
var _ interface {
	PublishPaymentEvent(ctx context.Context, event *dto.MQPaymentEvent) error
} = (*PaymentEventProducer)(nil)
