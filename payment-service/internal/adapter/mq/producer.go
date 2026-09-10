// Package mq MQ producer implementation.
package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
	"github.com/stablepay/payment-service/internal/application/dto"
	"github.com/stablepay/payment-service/pkg/constants"
	"github.com/stablepay/payment-service/pkg/errors"
	"go.uber.org/zap"
)

// PaymentEventProducer publishes payment events to RocketMQ.
type PaymentEventProducer struct {
	producer rocketmq.Producer
	topic    string
	logger   *zap.Logger
}

func NewPaymentEventProducer(nameServers []string, producerGroup string, topic string, logger *zap.Logger) (*PaymentEventProducer, error) {
	if strings.TrimSpace(topic) != constants.MQTopicPaymentEvents {
		return nil, fmt.Errorf("payment event producer topic must be %q, got %q", constants.MQTopicPaymentEvents, topic)
	}
	resolvedNameServers := resolveNameServers(nameServers)

	p, err := rocketmq.NewProducer(
		producer.WithNameServer(resolvedNameServers),
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

func (p *PaymentEventProducer) PublishPaymentEvent(ctx context.Context, event *dto.MQPaymentEvent) error {
	if err := event.Validate(); err != nil {
		return errors.Wrap(errors.INVALID_PARAMETERS, err, "invalid payment event")
	}
	data, err := json.Marshal(event)
	if err != nil {
		return errors.Wrap(errors.INTERNAL_SERVER_ERROR, err, "failed to marshal event")
	}

	tag := eventTagForEvent(event)
	if tag == "" {
		p.logger.Warn("unknown event status, skip publishing", zap.String("status", event.Status))
		return nil
	}

	msg := primitive.NewMessage(p.topic, data).
		WithTag(tag).
		WithKeys([]string{event.TxID})

	res, err := p.producer.SendSync(ctx, msg)
	if err != nil {
		p.logger.Error("failed to send mq message", zap.Error(err), zap.String("tx_id", event.TxID))
		return errors.Wrap(errors.INTERNAL_SERVER_ERROR, err, "failed to publish event")
	}

	if res.Status != primitive.SendOK {
		statusStr := fmt.Sprintf("%d", res.Status)
		p.logger.Error("mq send not ok", zap.String("status", statusStr), zap.String("tx_id", event.TxID))
		return errors.Newf(errors.INTERNAL_SERVER_ERROR, "mq send failed with status: %s", statusStr)
	}

	p.logger.Info("payment event published",
		zap.String("event_id", event.EventID),
		zap.String("tx_id", event.TxID),
		zap.String("status", event.Status),
		zap.String("tag", tag),
		zap.String("msg_id", res.MsgID),
	)

	return nil
}

func (p *PaymentEventProducer) Shutdown() error {
	if err := p.producer.Shutdown(); err != nil {
		return fmt.Errorf("failed to shutdown mq producer: %w", err)
	}
	return nil
}

type LocalMessageTable struct{}

type TransactionMessage struct {
	ID         string
	Topic      string
	Tag        string
	Keys       []string
	Body       []byte
	Status     int
	RetryCount int
	CreatedAt  int64
	UpdatedAt  int64
}

var _ interface {
	PublishPaymentEvent(ctx context.Context, event *dto.MQPaymentEvent) error
} = (*PaymentEventProducer)(nil)

func eventTagFromStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "CONFIRMED", "COMPLETED":
		return dto.ToMQEventTag(3)
	case "FAILED", "CANCELLED":
		return dto.ToMQEventTag(4)
	default:
		return ""
	}
}

// eventTagForEvent uses event_type as the RocketMQ tag. The physical topic is
// always payment_events; the tag is only the event kind inside that topic.
func eventTagForEvent(event *dto.MQPaymentEvent) string {
	if event.EventType != "" {
		return event.EventType
	}
	return eventTagFromStatus(event.Status)
}

func resolveNameServers(nameServers []string) []string {
	out := make([]string, 0, len(nameServers))
	for _, addr := range nameServers {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		out = append(out, resolveOneNameServer(addr))
	}

	if len(out) == 0 {
		return []string{"127.0.0.1:9876"}
	}

	return out
}

func resolveOneNameServer(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.Count(addr, ":") == 0 {
			addr = net.JoinHostPort(addr, "9876")
			host, port = addr, "9876"
			if strings.Count(addr, ":") > 0 {
				host, port, err = net.SplitHostPort(addr)
			}
		}
		if err != nil {
			return addr
		}
	}

	ips, err := net.LookupHost(host)
	if err != nil || len(ips) == 0 {
		return addr
	}

	for _, ip := range ips {
		if net.ParseIP(ip).To4() != nil {
			return net.JoinHostPort(ip, port)
		}
	}

	return net.JoinHostPort(ips[0], port)
}
