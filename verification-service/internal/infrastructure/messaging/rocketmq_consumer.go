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
	EventID        string `json:"event_id"`
	EventType      string `json:"event_type"`
	SchemaVersion  int    `json:"schema_version"`
	IdempotencyKey string `json:"idempotency_key"`
	TxID           string `json:"tx_id"`
	AgentDID       string `json:"agent_did"`
	SkillDID       string `json:"skill_did"`
	AmountMinor    int64  `json:"amount_minor"`
	Currency       string `json:"currency"`
	TxHash         string `json:"tx_hash"`
	Status         string `json:"status"`
	OccurredAt     string `json:"occurred_at"`
	ConfirmedAt    string `json:"confirmed_at"`
	Timestamp      int64  `json:"timestamp"` // legacy alias
	RequestID      string `json:"request_id"`
	TraceID        string `json:"trace_id"`
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
	if strings.TrimSpace(c.topic) != "payment_events" {
		return fmt.Errorf("verification consumer topic must be %q, got %q", "payment_events", c.topic)
	}
	if err := checkNameServers(c.nameServers); err != nil {
		return err
	}
	client, err := rocketmq.NewPushConsumer(consumer.WithGroupName(c.group), consumer.WithNameServer(c.nameServers))
	if err != nil {
		return fmt.Errorf("create RocketMQ consumer: %w", err)
	}
	c.consumer = client
	if err := client.Subscribe(c.topic, consumer.MessageSelector{}, c.consume); err != nil {
		_ = client.Shutdown()
		return fmt.Errorf("subscribe payment events: %w", err)
	}
	if err := client.Start(); err != nil {
		_ = client.Shutdown()
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
		if err := event.Validate(); err != nil {
			return consumer.ConsumeRetryLater, err
		}
		if !isSuccessful(message.GetTags(), event) {
			continue
		}
		if err := c.persistEvent(ctx, event); err != nil {
			return consumer.ConsumeRetryLater, err
		}
	}
	return consumer.ConsumeSuccess, nil
}

func (c *Consumer) persistEvent(ctx context.Context, event PaymentEvent) error {
	if existing, err := c.repository.FindByEventID(ctx, event.EventID); err == nil && existing != nil {
		return nil
	}
	createdAt := eventTime(event)
	err := c.repository.Create(ctx, &entity.PurchaseRecord{
		EventID: event.EventID, AgentDID: event.AgentDID, SkillDID: event.SkillDID,
		TxID: event.TxID, AmountMinor: event.AmountMinor, Currency: currencyCode(event.Currency),
		TxHash: event.TxHash, CreatedAt: createdAt,
	})
	if err == nil {
		return nil
	}
	// A concurrent redelivery may race between FindByEventID and Create. Treat
	// an already recorded event as success; other database errors are retried.
	if existing, findErr := c.repository.FindByEventID(ctx, event.EventID); findErr == nil && existing != nil {
		return nil
	}
	// Preserve the business-level one-purchase-per-agent/skill invariant for
	// legacy rows created before event_id was introduced.
	if existing, findErr := c.repository.Find(ctx, event.AgentDID, event.SkillDID); findErr == nil && existing != nil && existing.TxID == event.TxID {
		return nil
	}
	return fmt.Errorf("persist purchase record: %w", err)
}

func (e PaymentEvent) Validate() error {
	if e.EventID == "" || e.IdempotencyKey == "" || e.TxID == "" || e.AgentDID == "" || e.SkillDID == "" {
		return fmt.Errorf("payment event missing stable identity")
	}
	if e.SchemaVersion != 1 {
		return fmt.Errorf("unsupported payment event schema_version: %d", e.SchemaVersion)
	}
	if e.EventType != "payment.success" && e.EventType != "payment.failed" {
		return fmt.Errorf("unsupported payment event_type: %s", e.EventType)
	}
	if e.AmountMinor <= 0 {
		return fmt.Errorf("payment event amount_minor must be positive")
	}
	if currency := strings.ToUpper(strings.TrimSpace(e.Currency)); currency != "USDC" && currency != "USDT" {
		return fmt.Errorf("unsupported payment event currency: %s", e.Currency)
	}
	if _, err := time.Parse(time.RFC3339, e.OccurredAt); err != nil && e.Timestamp <= 0 {
		return fmt.Errorf("payment event occurred_at is invalid")
	}
	return nil
}

func isSuccessful(tag string, event PaymentEvent) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "payment.success" || tag == "payment_succeeded" || tag == "payment.reward.granted" || tag == "reward_granted" {
		return true
	}
	if event.EventType == "payment.success" {
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

func eventTime(event PaymentEvent) time.Time {
	if event.OccurredAt != "" {
		if parsed, err := time.Parse(time.RFC3339, event.OccurredAt); err == nil {
			return parsed.UTC()
		}
	}
	return timeFromUnix(event.Timestamp)
}

func checkNameServers(values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("RocketMQ consumer has no nameserver configured")
	}
	var lastErr error
	for _, value := range values {
		conn, err := net.DialTimeout("tcp", value, 2*time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("RocketMQ nameserver is not reachable; verification-service is not ready: %w", lastErr)
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
