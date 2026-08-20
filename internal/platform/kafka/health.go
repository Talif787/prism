package kafka

import (
	"context"
	"errors"
	"time"

	kgo "github.com/segmentio/kafka-go"
)

// Ping verifies broker reachability by dialing the first broker and reading
// cluster metadata. Used by readiness checks.
func Ping(ctx context.Context, brokers []string) error {
	if len(brokers) == 0 {
		return errors.New("no kafka brokers configured")
	}
	d := &kgo.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ReadPartitions(); err != nil {
		return err
	}
	return nil
}
