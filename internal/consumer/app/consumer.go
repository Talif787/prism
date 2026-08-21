package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Talif787/prism/internal/platform/kafka"
)

// BatchConfig controls how rows accumulate before a flush.
type BatchConfig struct {
	MaxRows int
	MaxWait time.Duration
}

// Consume runs one signal's consume loop: fetch, transform, batch, and flush to
// the writer, committing offsets only after a successful write (at-least-once).
// It is generic over the row type T so metrics, logs, and traces share one loop.
//
// Delivery and failure model:
//   - Offsets commit only after write succeeds, so a crash re-delivers; the
//     ClickHouse tables are ReplacingMergeTree, so redelivered rows collapse.
//   - A write or commit error returns from the loop (fail fast). The process
//     exits and the orchestrator restarts it, reprocessing uncommitted messages.
//   - A transform error skips just that message (it is committed so it is not
//     retried forever), since a malformed payload will never succeed.
func Consume[T any](
	ctx context.Context,
	reader *kafka.Reader,
	signal string,
	transform func(kafka.FetchedMessage) ([]T, error),
	write func(context.Context, []T) error,
	cfg BatchConfig,
	logger *slog.Logger,
) error {
	buf := make([]T, 0, cfg.MaxRows)
	pending := make([]kafka.FetchedMessage, 0, cfg.MaxRows)
	lastFlush := time.Now()

	flush := func(fctx context.Context) error {
		if len(pending) == 0 {
			lastFlush = time.Now()
			return nil
		}
		if len(buf) > 0 {
			if err := write(fctx, buf); err != nil {
				return err
			}
		}
		if err := reader.Commit(fctx, pending...); err != nil {
			return err
		}
		logger.Debug("consumer flush",
			slog.String("signal", signal),
			slog.Int("rows", len(buf)),
			slog.Int("messages", len(pending)),
		)
		buf = buf[:0]
		pending = pending[:0]
		lastFlush = time.Now()
		return nil
	}

	// finalFlush drains on shutdown using a fresh context, since the loop context
	// is already cancelled.
	finalFlush := func() error {
		if len(pending) == 0 {
			return nil
		}
		fctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return flush(fctx)
	}

	for {
		if ctx.Err() != nil {
			return finalFlush()
		}

		fctx, cancel := context.WithTimeout(ctx, cfg.MaxWait)
		msg, err := reader.Fetch(fctx)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return finalFlush()
			}
			if errors.Is(err, context.DeadlineExceeded) {
				// Periodic wake: flush anything waiting.
				if ferr := flush(ctx); ferr != nil {
					return ferr
				}
				continue
			}
			logger.Error("consumer fetch error", slog.String("signal", signal), slog.Any("error", err))
			time.Sleep(time.Second)
			continue
		}

		rows, terr := transform(msg)
		pending = append(pending, msg) // advance past the message either way
		if terr != nil {
			logger.Error("consumer transform failed; skipping message",
				slog.String("signal", signal), slog.Any("error", terr))
		} else {
			buf = append(buf, rows...)
		}

		if len(buf) >= cfg.MaxRows || time.Since(lastFlush) >= cfg.MaxWait {
			if ferr := flush(ctx); ferr != nil {
				return ferr
			}
		}
	}
}
