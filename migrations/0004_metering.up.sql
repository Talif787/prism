-- Phase 6: metering and billing. usage_rollups holds accepted-telemetry counts per
-- tenant, per signal, per window, rolled up from ClickHouse. metering_state tracks
-- how far each signal has been rolled up. invoices and invoice_line_items are the
-- billing artifacts produced by closing a period.

CREATE TABLE usage_rollups (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    signal       TEXT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    point_count  BIGINT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, signal, window_start)
);

CREATE INDEX idx_usage_rollups_tenant_window ON usage_rollups (tenant_id, window_start);

CREATE TABLE metering_state (
    signal       TEXT PRIMARY KEY,
    watermark    TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE invoices (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    period_start TIMESTAMPTZ NOT NULL,
    period_end   TIMESTAMPTZ NOT NULL,
    status       TEXT NOT NULL DEFAULT 'closed',
    currency     TEXT NOT NULL,
    total        DOUBLE PRECISION NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_invoices_tenant ON invoices (tenant_id, created_at);

CREATE TABLE invoice_line_items (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id             UUID NOT NULL REFERENCES invoices (id) ON DELETE CASCADE,
    signal                 TEXT NOT NULL,
    quantity               BIGINT NOT NULL,
    unit_price_per_million DOUBLE PRECISION NOT NULL,
    amount                 DOUBLE PRECISION NOT NULL
);

CREATE INDEX idx_invoice_line_items_invoice ON invoice_line_items (invoice_id);
