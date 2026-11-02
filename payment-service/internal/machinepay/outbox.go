package machinepay

import (
	"context"
	"log"
	"time"
)

type EventPublisher interface {
	PublishMachinePayEvent(context.Context, SettledEvent) error
}

func DispatchPendingEvents(ctx context.Context, store Store, publisher EventPublisher) error {
	events, err := store.PendingEvents(ctx, 100)
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := publisher.PublishMachinePayEvent(ctx, event); err != nil {
			return err
		}
		if err := store.MarkEventPublished(ctx, event.EventID); err != nil {
			return err
		}
	}
	return nil
}

func RunOutboxDispatcher(ctx context.Context, store Store, publisher EventPublisher) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := DispatchPendingEvents(ctx, store, publisher); err != nil {
			log.Printf("Machine Pay outbox dispatch failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
