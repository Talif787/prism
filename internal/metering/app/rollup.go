package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/Talif787/prism/internal/metering/domain"
)

// RollupService meters accepted telemetry by rolling up closed windows from the
// usage source into the store, one window at a time, tracked by a per-signal
// watermark so each closed window is counted exactly once.
type RollupService struct {
	source   UsageSource
	store    UsageStore
	interval time.Duration
	backfill time.Duration
	logger   *slog.Logger
}

func NewRollupService(source UsageSource, store UsageStore, interval, backfill time.Duration, logger *slog.Logger) *RollupService {
	return &RollupService{source: source, store: store, interval: interval, backfill: backfill, logger: logger}
}

// RunOnce rolls up every signal up to the current closed window boundary. A single
// signal failing is logged and does not stop the others.
func (s *RollupService) RunOnce(ctx context.Context, now time.Time) error {
	boundary := now.Truncate(s.interval)
	var firstErr error
	for _, sig := range domain.AllSignals {
		if err := s.rollupSignal(ctx, sig, boundary); err != nil {
			s.logger.ErrorContext(ctx, "rollup failed", slog.String("signal", string(sig)), slog.Any("error", err))
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (s *RollupService) rollupSignal(ctx context.Context, sig domain.Signal, boundary time.Time) error {
	wm, err := s.store.GetWatermark(ctx, sig)
	if err != nil {
		return err
	}
	var from time.Time
	if wm == nil {
		from = boundary.Add(-s.backfill)
	} else {
		from = *wm
	}
	if !from.Before(boundary) {
		return nil // nothing has closed since the last rollup
	}

	records, err := s.source.CountUsage(ctx, sig, from, boundary, int(s.interval/time.Second))
	if err != nil {
		return err
	}
	for i := range records {
		if err := s.store.UpsertRollup(ctx, records[i]); err != nil {
			return err
		}
	}
	return s.store.SetWatermark(ctx, sig, boundary)
}
