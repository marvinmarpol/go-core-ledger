// Package outbox implements the transactional outbox relay worker.
// The relay polls for unpublished outbox_events rows, delivers each to a
// Publisher, and marks it published — guaranteeing at-least-once delivery.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"

	store "go-core-ledger/internal/store/pg"
)

// Event is the decoded form of an outbox_events row passed to a Publisher.
type Event struct {
	ID            string
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       json.RawMessage
}

// Publisher delivers a single outbox event to an external system (Kafka, HTTP, etc.).
// Implementations must be idempotent: the same event may be delivered more than
// once if the process crashes between Publish and MarkPublished.
type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

// Relay fetches unpublished events and delivers them via Publisher.
type Relay struct {
	store     *store.Store
	publisher Publisher
	batchSize int32
}

// NewRelay creates a Relay. batchSize controls how many events are fetched per
// call to Process; a value <= 0 defaults to 100.
func NewRelay(st *store.Store, pub Publisher, batchSize int32) *Relay {
	if batchSize <= 0 {
		batchSize = 100
	}
	return &Relay{store: st, publisher: pub, batchSize: batchSize}
}

// Process fetches up to batchSize unpublished events, publishes each one, and
// marks it published. Returns the number of events successfully processed.
// A publish failure stops the batch immediately and returns the error.
func (r *Relay) Process(ctx context.Context) (int, error) {
	rows, err := r.store.GetUnpublishedOutboxEvents(ctx, r.batchSize)
	if err != nil {
		return 0, fmt.Errorf("fetch unpublished events: %w", err)
	}

	var processed int
	for _, row := range rows {
		ev := Event{
			ID:            store.OutboxEventIDString(row.ID),
			AggregateType: row.AggregateType,
			AggregateID:   row.AggregateID,
			EventType:     row.EventType,
			Payload:       row.Payload,
		}

		if err := r.publisher.Publish(ctx, ev); err != nil {
			return processed, fmt.Errorf("publish event %s: %w", ev.ID, err)
		}

		if _, err := r.store.MarkOutboxEventPublished(ctx, ev.ID); err != nil {
			return processed, fmt.Errorf("mark event %s published: %w", ev.ID, err)
		}

		processed++
	}

	return processed, nil
}
