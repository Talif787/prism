package pgstore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Talif787/prism/internal/tenancy/domain"
)

// OutboxPublisher writes domain events into an outbox table within the same
// transaction as the state change. A separate relay process (Phase 2) reads the
// outbox and publishes to Kafka, guaranteeing at-least-once delivery without
// distributed transactions (the transactional outbox pattern).
type OutboxPublisher struct{ q querier }

func NewOutboxPublisher(q querier) *OutboxPublisher { return &OutboxPublisher{q: q} }

type outboxPayload struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

func (p *OutboxPublisher) Publish(ctx context.Context, events ...domain.Event) error {
	for _, ev := range events {
		data, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(outboxPayload{Name: ev.EventName(), Data: data})
		if err != nil {
			return err
		}
		if _, err := p.q.Exec(ctx, `
			INSERT INTO outbox (id, event_name, payload, occurred_at, created_at)
			VALUES ($1, $2, $3, $4, $5)`,
			uuid.NewString(), ev.EventName(), payload, ev.OccurredAt(), time.Now().UTC(),
		); err != nil {
			return err
		}
	}
	return nil
}
