// Package kafkaprod adapts the platform Kafka producer to the ingest app.Producer
// port, translating between the app's broker-agnostic Message and the platform type.
package kafkaprod

import (
	"context"

	"github.com/Talif787/prism/internal/ingest/app"
	"github.com/Talif787/prism/internal/platform/kafka"
)

// Adapter implements app.Producer.
type Adapter struct {
	producer *kafka.Producer
}

func New(p *kafka.Producer) *Adapter { return &Adapter{producer: p} }

func (a *Adapter) Produce(ctx context.Context, msg app.Message) error {
	headers := make([]kafka.Header, len(msg.Headers))
	for i, h := range msg.Headers {
		headers[i] = kafka.Header{Key: h.Key, Value: h.Value}
	}
	return a.producer.Produce(ctx, kafka.Message{
		Topic:   msg.Topic,
		Key:     msg.Key,
		Value:   msg.Value,
		Headers: headers,
	})
}
