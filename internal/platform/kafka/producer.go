// Package kafka provides a thin producer over segmentio/kafka-go. It is pure Go
// (no cgo), so it builds into the distroless image cleanly. The consumer side is
// added in Phase 3.
package kafka

import (
	"context"
	"fmt"
	"time"

	kgo "github.com/segmentio/kafka-go"
)

type Config struct {
	Brokers                []string
	RequiredAcks           int  // 0 none, 1 leader, -1 all
	AllowAutoTopicCreation bool // convenient for local dev; disable in production
	BatchTimeout           time.Duration
}

// Header is a message header (metadata travels here, not in the value).
type Header struct {
	Key   string
	Value []byte
}

// Message is a broker-agnostic message. Key drives partitioning (tenant id), so
// a tenant's records keep their relative order within a partition.
type Message struct {
	Topic   string
	Key     []byte
	Value   []byte
	Headers []Header
}

// Producer writes messages with hash-based partitioning by key.
type Producer struct {
	w *kgo.Writer
}

func NewProducer(cfg Config) *Producer {
	acks := kgo.RequireAll
	switch cfg.RequiredAcks {
	case 0:
		acks = kgo.RequireNone
	case 1:
		acks = kgo.RequireOne
	}
	return &Producer{
		w: &kgo.Writer{
			Addr:                   kgo.TCP(cfg.Brokers...),
			Balancer:               &kgo.Hash{},
			RequiredAcks:           acks,
			AllowAutoTopicCreation: cfg.AllowAutoTopicCreation,
			BatchTimeout:           orDefault(cfg.BatchTimeout, 50*time.Millisecond),
			Async:                  false,
		},
	}
}

// Produce writes a single message synchronously, returning only after the broker
// acknowledges per the configured acks level.
func (p *Producer) Produce(ctx context.Context, msg Message) error {
	headers := make([]kgo.Header, len(msg.Headers))
	for i, h := range msg.Headers {
		headers[i] = kgo.Header{Key: h.Key, Value: h.Value}
	}
	err := p.w.WriteMessages(ctx, kgo.Message{
		Topic:   msg.Topic,
		Key:     msg.Key,
		Value:   msg.Value,
		Headers: headers,
	})
	if err != nil {
		return fmt.Errorf("kafka write: %w", err)
	}
	return nil
}

func (p *Producer) Close() error { return p.w.Close() }

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}
