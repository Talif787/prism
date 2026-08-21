package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/Talif787/prism/internal/alerting/domain"
)

// Evaluator runs due rules against the metrics store, advances the per-series
// state machine (pending, firing, resolved), persists instances, and notifies on
// transitions.
type Evaluator struct {
	store    RuleStore
	reader   MetricsReader
	notifier Notifier
	logger   *slog.Logger
}

func NewEvaluator(store RuleStore, reader MetricsReader, notifier Notifier, logger *slog.Logger) *Evaluator {
	return &Evaluator{store: store, reader: reader, notifier: notifier, logger: logger}
}

// EvaluateDue evaluates every rule whose interval has elapsed. A single rule
// failing is logged and does not stop the cycle.
func (e *Evaluator) EvaluateDue(ctx context.Context, now time.Time) error {
	rules, err := e.store.LoadDueRules(ctx, now)
	if err != nil {
		return err
	}
	for i := range rules {
		rule := &rules[i]
		if err := e.evaluateRule(ctx, rule, now); err != nil {
			e.logger.ErrorContext(ctx, "rule evaluation failed",
				slog.String("rule_id", rule.ID), slog.String("tenant_id", rule.TenantID), slog.Any("error", err))
		}
		if err := e.store.MarkEvaluated(ctx, rule.ID, now); err != nil {
			e.logger.ErrorContext(ctx, "mark evaluated failed", slog.String("rule_id", rule.ID), slog.Any("error", err))
		}
	}
	return nil
}

func (e *Evaluator) evaluateRule(ctx context.Context, rule *domain.Rule, now time.Time) error {
	values, err := e.reader.Read(ctx, rule.TenantID, Condition{
		Metric: rule.Metric, Agg: rule.Agg, GroupBy: rule.GroupBy, Filters: rule.Filters, Window: rule.Window,
	})
	if err != nil {
		return err
	}

	existing, err := e.store.ListInstances(ctx, rule.ID)
	if err != nil {
		return err
	}
	byFP := make(map[string]*domain.Instance, len(existing))
	for i := range existing {
		byFP[existing[i].Fingerprint] = &existing[i]
	}

	seen := make(map[string]bool, len(values))
	for _, sv := range values {
		fp := domain.Fingerprint(sv.Labels)
		seen[fp] = true
		inst := byFP[fp]

		if rule.Breached(sv.Value) {
			if inst == nil {
				ni := &domain.Instance{
					RuleID: rule.ID, TenantID: rule.TenantID, Fingerprint: fp, Labels: sv.Labels,
					State: domain.StatePending, Value: sv.Value, ActiveSince: now, UpdatedAt: now,
				}
				if rule.For == 0 {
					firedAt := now
					ni.State = domain.StateFiring
					ni.FiredAt = &firedAt
					e.notify(ctx, rule, ni, "firing")
				}
				if err := e.store.UpsertInstance(ctx, ni); err != nil {
					return err
				}
				continue
			}
			inst.Value = sv.Value
			inst.UpdatedAt = now
			if inst.State == domain.StatePending && now.Sub(inst.ActiveSince) >= rule.For {
				firedAt := now
				inst.State = domain.StateFiring
				inst.FiredAt = &firedAt
				e.notify(ctx, rule, inst, "firing")
			}
			if err := e.store.UpsertInstance(ctx, inst); err != nil {
				return err
			}
			continue
		}

		// Not breached: resolve any existing instance for this series.
		if inst != nil {
			if inst.State == domain.StateFiring {
				e.notify(ctx, rule, inst, "resolved")
			}
			if err := e.store.DeleteInstance(ctx, rule.ID, fp); err != nil {
				return err
			}
		}
	}

	// Series that no longer appear in the query result are resolved.
	for fp, inst := range byFP {
		if seen[fp] {
			continue
		}
		if inst.State == domain.StateFiring {
			e.notify(ctx, rule, inst, "resolved")
		}
		if err := e.store.DeleteInstance(ctx, rule.ID, fp); err != nil {
			return err
		}
	}
	return nil
}

func (e *Evaluator) notify(ctx context.Context, rule *domain.Rule, inst *domain.Instance, event string) {
	if rule.Webhook == "" {
		return
	}
	if err := e.notifier.Notify(ctx, rule.Webhook, Notification{Event: event, Rule: *rule, Instance: *inst}); err != nil {
		e.logger.WarnContext(ctx, "notification failed",
			slog.String("rule_id", rule.ID), slog.String("event", event), slog.Any("error", err))
	}
}
