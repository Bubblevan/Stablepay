package fiat

import (
	"context"
	"log"
	"time"

	"github.com/stablepay/payment-service/internal/application/dto"
)

type EventPublisher interface {
	PublishPaymentEvent(context.Context, *dto.MQPaymentEvent) error
}
type eventOutbox interface {
	PendingEvents(context.Context, int) ([]dto.MQPaymentEvent, error)
	MarkEventPublished(context.Context, string) error
}

func DispatchPendingEvents(ctx context.Context, store eventOutbox, publisher EventPublisher) error {
	events, err := store.PendingEvents(ctx, 100)
	if err != nil {
		return err
	}
	for i := range events {
		event := events[i]
		if err := publisher.PublishPaymentEvent(ctx, &event); err != nil {
			return err
		}
		if err := store.MarkEventPublished(ctx, event.EventID); err != nil {
			return err
		}
	}
	return nil
}
func RunOutboxDispatcher(ctx context.Context, store eventOutbox, publisher EventPublisher) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := DispatchPendingEvents(ctx, store, publisher); err != nil {
			log.Printf("fiat payment outbox dispatch failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
