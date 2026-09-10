// replay-payment-event publishes an already captured canonical payment event
// to the real payment_events topic. It is intentionally a small operational
// tool for the deterministic E2E idempotency case, not a fake blockchain path.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
)

type paymentEvent struct {
	EventID        string `json:"event_id"`
	EventType      string `json:"event_type"`
	SchemaVersion  int    `json:"schema_version"`
	IdempotencyKey string `json:"idempotency_key"`
}

func main() {
	nameServer := flag.String("nameserver", "127.0.0.1:9876", "RocketMQ nameserver")
	topic := flag.String("topic", "payment_events", "RocketMQ physical topic")
	group := flag.String("group", "stablepay-e2e-replayer", "producer group")
	file := flag.String("file", "", "canonical event JSON file")
	flag.Parse()
	if strings.TrimSpace(*file) == "" {
		fatalf("event file is required")
	}
	if *topic != "payment_events" {
		fatalf("topic must be payment_events")
	}
	body, err := os.ReadFile(*file)
	if err != nil {
		fatalf("read event file: %v", err)
	}
	var event paymentEvent
	if err := json.Unmarshal(body, &event); err != nil {
		fatalf("decode event file: %v", err)
	}
	if event.EventID == "" || event.IdempotencyKey == "" || event.SchemaVersion != 1 || (event.EventType != "payment.success" && event.EventType != "payment.failed") {
		fatalf("event file is not a canonical payment event")
	}

	p, err := rocketmq.NewProducer(producer.WithNameServer([]string{*nameServer}), producer.WithGroupName(*group), producer.WithRetry(0))
	if err != nil {
		fatalf("create producer: %v", err)
	}
	if err := p.Start(); err != nil {
		fatalf("start producer: %v", err)
	}
	defer p.Shutdown()
	message := primitive.NewMessage(*topic, body).WithTag(event.EventType).WithKeys([]string{event.IdempotencyKey})
	result, err := p.SendSync(context.Background(), message)
	if err != nil {
		fatalf("replay event: %v", err)
	}
	if result.Status != primitive.SendOK {
		fatalf("replay event returned status %d", result.Status)
	}
	fmt.Printf("replayed event_id=%s msg_id=%s at=%s\n", event.EventID, result.MsgID, time.Now().UTC().Format(time.RFC3339))
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
