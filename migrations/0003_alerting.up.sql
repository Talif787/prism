-- Phase 5: alerting. Rules define a threshold condition over one metric; instances
-- track the current state of each firing or pending series for a rule.

CREATE TABLE alert_rules (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    metric           TEXT NOT NULL,
    agg              TEXT NOT NULL,
    group_by         TEXT[] NOT NULL DEFAULT '{}',
    filters          JSONB NOT NULL DEFAULT '{}',
    window_seconds   INTEGER NOT NULL,
    operator         TEXT NOT NULL,
    threshold        DOUBLE PRECISION NOT NULL,
    for_seconds      INTEGER NOT NULL DEFAULT 0,
    interval_seconds INTEGER NOT NULL DEFAULT 30,
    severity         TEXT NOT NULL DEFAULT 'warning',
    labels           JSONB NOT NULL DEFAULT '{}',
    annotations      JSONB NOT NULL DEFAULT '{}',
    notify_webhook   TEXT NOT NULL DEFAULT '',
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    last_evaluated_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX idx_alert_rules_due ON alert_rules (enabled, last_evaluated_at);

CREATE TABLE alert_instances (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id            UUID NOT NULL REFERENCES alert_rules (id) ON DELETE CASCADE,
    tenant_id          UUID NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    series_fingerprint TEXT NOT NULL,
    labels             JSONB NOT NULL DEFAULT '{}',
    state              TEXT NOT NULL,
    value              DOUBLE PRECISION NOT NULL,
    active_since       TIMESTAMPTZ NOT NULL,
    fired_at           TIMESTAMPTZ,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (rule_id, series_fingerprint)
);

CREATE INDEX idx_alert_instances_tenant ON alert_instances (tenant_id, state);
