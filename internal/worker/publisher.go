package worker

import (
	"context"
	"encoding/json"
	"log"

	"go-core-ledger/internal/outbox"
)

// LogPublisher implements outbox.Publisher by writing each event to stdout.
// Replace with a Kafka or HTTP implementation when an external broker is available.
type LogPublisher struct{}

// Publish logs the event to stdout and always returns nil.
func (LogPublisher) Publish(_ context.Context, ev outbox.Event) error {
	b, err := json.Marshal(ev.Payload)
	if err != nil {
		log.Printf("[outbox] event %s type=%s aggregate=%s/%s payload=<marshal error: %v>",
			ev.ID, ev.EventType, ev.AggregateType, ev.AggregateID, err)
		return nil
	}
	log.Printf("[outbox] event %s type=%s aggregate=%s/%s payload=%s",
		ev.ID, ev.EventType, ev.AggregateType, ev.AggregateID, b)
	return nil
}
