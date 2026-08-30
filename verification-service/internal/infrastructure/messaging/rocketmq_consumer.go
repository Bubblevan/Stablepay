package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/stablepay/verification-service/internal/domain"
	"github.com/stablepay/verification-service/internal/domain/entity"
)

type PaymentEvent struct {
	TxID        string `json:"tx_id"`
	AgentDID    string `json:"agent_did"`
	SkillDID    string `json:"skill_did"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	TxHash      string `json:"tx_hash"`
	Status      string `json:"status"`
	Timestamp   int64  `json:"timestamp"`
	EventType   string `json:"event_type"`
}

type Consumer struct {
	nameServers []string
	group       string
	topic       string
	repository  domain.PurchaseRepository
	consumer    rocketmq.PushConsumer
}

func NewConsumer(nameServers []string, group, topic string, repository domain.PurchaseRepository) *Consumer {
	if group == "" {
		group = "verification_group"
	}
	if topic == "" {
		topic = "payment_events"
	}
	return &Consumer{nameServers: resolveNameServers(nameServers), group: group, topic: topic, repository: repository}
}

func (c *Consumer) Start(ctx context.Context) error {
	if c.repository == nil {
		return fmt.Errorf("purchase repository is required")
	}
	client, err := rocketmq.NewPushConsumer(consumer.WithGroupName(c.group), consumer.WithNameServer(c.nameServers))
	if err != nil {
		return fmt.Errorf("create RocketMQ consumer: %w", err)
	}
	c.consumer = client
	if err := client.Subscribe(c.topic, consumer.MessageSelector{}, c.consume); err != nil {
		return fmt.Errorf("subscribe payment events: %w", err)
	}
	if err := client.Start(); err != nil {
		return fmt.Errorf("start RocketMQ consumer: %w", err)
	}
	go func() { <-ctx.Done(); _ = c.Close() }()
	return nil
}

func (c *Consumer) Close() error {
	if c.consumer == nil {
		return nil
	}
	err := c.consumer.Shutdown()
	c.consumer = nil
	return err
}

func (c *Consumer) consume(ctx context.Context, messages ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
	for _, message := range messages {
		var event PaymentEvent
		if err := json.Unmarshal(message.Body, &event); err != nil {
			return consumer.ConsumeRetryLater, fmt.Errorf("decode payment event: %w", err)
		}
		if !isSuccessful(message.GetTags(), event) {
			continue
		}
		if event.AgentDID == "" || event.SkillDID == "" || event.TxID == "" {
			return consumer.ConsumeRetryLater, fmt.Errorf("payment event missing agent_did, skill_did or tx_id")
		}
		createdAt := timeFromUnix(event.Timestamp)
		err := c.repository.Create(ctx, &entity.PurchaseRecord{AgentDID: event.AgentDID, SkillDID: event.SkillDID, TxID: event.TxID, AmountMinor: event.AmountMinor, Currency: currencyCode(event.Currency), TxHash: event.TxHash, CreatedAt: createdAt})
		if err != nil {
			// The unique (agent_did, skill_did) constraint makes redelivery safe.
			if existing, findErr := c.repository.Find(ctx, event.AgentDID, event.SkillDID); findErr == nil && existing != nil && existing.TxID == event.TxID {
				continue
			}
			return consumer.ConsumeRetryLater, fmt.Errorf("persist purchase record: %w", err)
		}
	}
	return consumer.ConsumeSuccess, nil
}

func isSuccessful(tag string, event PaymentEvent) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "payment_succeeded" || tag == "reward_granted" {
		return true
	}
	status := strings.ToUpper(strings.TrimSpace(event.Status))
	return status == "CONFIRMED" || status == "COMPLETED"
}
func currencyCode(currency string) int32 {
	if strings.EqualFold(strings.TrimSpace(currency), "USDT") || strings.TrimSpace(currency) == "2" {
		return 2
	}
	return 1
}
func timeFromUnix(timestamp int64) time.Time {
	if timestamp <= 0 {
		return time.Now().UTC()
	}
	return time.Unix(timestamp, 0).UTC()
}
func resolveNameServers(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !strings.Contains(value, ":") {
			value = net.JoinHostPort(value, "9876")
		}
		result = append(result, value)
	}
	if len(result) == 0 {
		return []string{"127.0.0.1:9876"}
	}
	return result
}
