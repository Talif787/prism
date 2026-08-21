// Package pgstore is the Postgres adapter for alerting: rule CRUD, due-rule
// loading, and alert-instance persistence. It mirrors the tenancy repositories'
// pgx patterns. JSONB columns are written with explicit casts and read back into
// maps; id comparisons use ::text so a malformed id is a not-found, not an error.
package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Talif787/prism/internal/alerting/domain"
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const ruleColumns = `id, tenant_id, name, metric, agg, group_by, filters, window_seconds,
	operator, threshold, for_seconds, interval_seconds, severity, labels, annotations,
	notify_webhook, enabled, last_evaluated_at, created_at, updated_at`

func (s *Store) CreateRule(ctx context.Context, r *domain.Rule) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO alert_rules
		  (tenant_id, name, metric, agg, group_by, filters, window_seconds, operator, threshold,
		   for_seconds, interval_seconds, severity, labels, annotations, notify_webhook, enabled)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11,$12,$13::jsonb,$14::jsonb,$15,$16)
		RETURNING id, created_at, updated_at`,
		r.TenantID, r.Name, r.Metric, r.Agg, orEmptySlice(r.GroupBy), mapJSON(r.Filters),
		seconds(r.Window), string(r.Operator), r.Threshold,
		seconds(r.For), seconds(r.Interval), r.Severity,
		mapJSON(r.Labels), mapJSON(r.Annotations), r.Webhook, r.Enabled,
	).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
}

func (s *Store) GetRule(ctx context.Context, tenantID, id string) (*domain.Rule, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+ruleColumns+`
		FROM alert_rules WHERE id::text = $1 AND tenant_id::text = $2`, id, tenantID)
	return scanRule(row)
}

func (s *Store) ListRules(ctx context.Context, tenantID string) ([]domain.Rule, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+ruleColumns+`
		FROM alert_rules WHERE tenant_id::text = $1 ORDER BY name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRules(rows)
}

func (s *Store) UpdateRule(ctx context.Context, r *domain.Rule) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE alert_rules SET
		  name=$3, metric=$4, agg=$5, group_by=$6, filters=$7::jsonb, window_seconds=$8,
		  operator=$9, threshold=$10, for_seconds=$11, interval_seconds=$12, severity=$13,
		  labels=$14::jsonb, annotations=$15::jsonb, notify_webhook=$16, enabled=$17, updated_at=now()
		WHERE id::text = $1 AND tenant_id::text = $2`,
		r.ID, r.TenantID, r.Name, r.Metric, r.Agg, orEmptySlice(r.GroupBy), mapJSON(r.Filters),
		seconds(r.Window), string(r.Operator), r.Threshold, seconds(r.For), seconds(r.Interval),
		r.Severity, mapJSON(r.Labels), mapJSON(r.Annotations), r.Webhook, r.Enabled,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) DeleteRule(ctx context.Context, tenantID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM alert_rules WHERE id::text = $1 AND tenant_id::text = $2`, id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) LoadDueRules(ctx context.Context, now time.Time) ([]domain.Rule, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+ruleColumns+`
		FROM alert_rules
		WHERE enabled = TRUE
		  AND (last_evaluated_at IS NULL OR last_evaluated_at <= $1::timestamptz - make_interval(secs => interval_seconds))`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRules(rows)
}

func (s *Store) MarkEvaluated(ctx context.Context, ruleID string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE alert_rules SET last_evaluated_at = $2 WHERE id::text = $1`, ruleID, at)
	return err
}

const instanceColumns = `id, rule_id, tenant_id, series_fingerprint, labels, state, value,
	active_since, fired_at, updated_at`

func (s *Store) ListInstances(ctx context.Context, ruleID string) ([]domain.Instance, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+instanceColumns+`
		FROM alert_instances WHERE rule_id::text = $1`, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectInstances(rows)
}

func (s *Store) ListTenantInstances(ctx context.Context, tenantID string) ([]domain.Instance, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+instanceColumns+`
		FROM alert_instances WHERE tenant_id::text = $1 ORDER BY updated_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectInstances(rows)
}

func (s *Store) UpsertInstance(ctx context.Context, i *domain.Instance) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO alert_instances
		  (rule_id, tenant_id, series_fingerprint, labels, state, value, active_since, fired_at, updated_at)
		VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9)
		ON CONFLICT (rule_id, series_fingerprint) DO UPDATE SET
		  labels = EXCLUDED.labels, state = EXCLUDED.state, value = EXCLUDED.value,
		  fired_at = EXCLUDED.fired_at, updated_at = EXCLUDED.updated_at`,
		i.RuleID, i.TenantID, i.Fingerprint, mapJSON(i.Labels), string(i.State), i.Value,
		i.ActiveSince, i.FiredAt, i.UpdatedAt,
	)
	return err
}

func (s *Store) DeleteInstance(ctx context.Context, ruleID, fingerprint string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM alert_instances WHERE rule_id::text = $1 AND series_fingerprint = $2`, ruleID, fingerprint)
	return err
}

// --- scanning helpers ---

type scannable interface {
	Scan(dest ...any) error
}

func scanRule(row scannable) (*domain.Rule, error) {
	var (
		r                        domain.Rule
		gb                       []string
		filters, labels, anns    []byte
		windowS, forS, intervalS int
		op                       string
		lastEval                 *time.Time
	)
	err := row.Scan(&r.ID, &r.TenantID, &r.Name, &r.Metric, &r.Agg, &gb, &filters, &windowS,
		&op, &r.Threshold, &forS, &intervalS, &r.Severity, &labels, &anns,
		&r.Webhook, &r.Enabled, &lastEval, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.GroupBy = gb
	r.Filters = jsonMap(filters)
	r.Labels = jsonMap(labels)
	r.Annotations = jsonMap(anns)
	r.Window = time.Duration(windowS) * time.Second
	r.For = time.Duration(forS) * time.Second
	r.Interval = time.Duration(intervalS) * time.Second
	r.Operator = domain.Operator(op)
	r.LastEval = lastEval
	return &r, nil
}

func collectRules(rows pgx.Rows) ([]domain.Rule, error) {
	out := make([]domain.Rule, 0)
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func scanInstance(row scannable) (*domain.Instance, error) {
	var (
		i       domain.Instance
		labels  []byte
		state   string
		firedAt *time.Time
	)
	if err := row.Scan(&i.ID, &i.RuleID, &i.TenantID, &i.Fingerprint, &labels, &state, &i.Value,
		&i.ActiveSince, &firedAt, &i.UpdatedAt); err != nil {
		return nil, err
	}
	i.Labels = jsonMap(labels)
	i.State = domain.State(state)
	i.FiredAt = firedAt
	return &i, nil
}

func collectInstances(rows pgx.Rows) ([]domain.Instance, error) {
	out := make([]domain.Instance, 0)
	for rows.Next() {
		inst, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inst)
	}
	return out, rows.Err()
}

// --- value helpers ---

func seconds(d time.Duration) int { return int(d / time.Second) }

func orEmptySlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func mapJSON(m map[string]string) string {
	if m == nil {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func jsonMap(b []byte) map[string]string {
	out := map[string]string{}
	if len(b) == 0 {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}
