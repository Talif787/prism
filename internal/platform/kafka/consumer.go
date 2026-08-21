package kafka

import (
	"context"
	"time"

	kgo "github.com/segmentio/kafka-go"
)

// ReaderConfig configures a consumer-group reader for a single topic.
type ReaderConfig struct {
	Brokers  []string
	GroupID  string
	Topic    string
	MinBytes int
	MaxBytes int
	MaxWait  time.Duration
}

// FetchedMessage is a message pulled from the broker. It carries the raw
// underlying message so offsets can be committed after processing.
type FetchedMessage struct {
	Topic   string
	Key     []byte
	Value   []byte
	Headers []Header
	raw     kgo.Message
}

// Reader is a thin wrapper over a segmentio consumer-group reader that fetches
// without auto-committing, so callers commit only after a successful write
// (at-least-once).
type Reader struct {
	r *kgo.Reader
}

func NewReader(cfg ReaderConfig) *Reader {
	return &Reader{
		r: kgo.NewReader(kgo.ReaderConfig{
			Brokers:     cfg.Brokers,
			GroupID:     cfg.GroupID,
			Topic:       cfg.Topic,
			MinBytes:    orInt(cfg.MinBytes, 1),
			MaxBytes:    orInt(cfg.MaxBytes, 10*1024*1024),
			MaxWait:     orDur(cfg.MaxWait, 500*time.Millisecond),
			StartOffset: kgo.FirstOffset,
		}),
	}
}

// Fetch blocks until a message is available or ctx is done. It does not commit.
func (r *Reader) Fetch(ctx context.Context) (FetchedMessage, error) {
	m, err := r.r.FetchMessage(ctx)
	if err != nil {
		return FetchedMessage{}, err
	}
	headers := make([]Header, len(m.Headers))
	for i, h := range m.Headers {
		headers[i] = Header{Key: h.Key, Value: h.Value}
	}
	return FetchedMessage{Topic: m.Topic, Key: m.Key, Value: m.Value, Headers: headers, raw: m}, nil
}

// Commit advances the group offsets for the given messages.
func (r *Reader) Commit(ctx context.Context, msgs ...FetchedMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	raw := make([]kgo.Message, len(msgs))
	for i, m := range msgs {
		raw[i] = m.raw
	}
	return r.r.CommitMessages(ctx, raw...)
}

func (r *Reader) Close() error { return r.r.Close() }

func orInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func orDur(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}
