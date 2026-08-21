# Metering and billing service (Phase 6)

The metering service (`cmd/metering`, port 8094) turns accepted telemetry into
usage, cost, and invoices. It rolls up counts from ClickHouse into Postgres,
exposes a tenant-scoped usage and billing API, and prices usage against a rate
card with per-plan quotas.

## Design at a glance

Two responsibilities run in one process:

1. A background rollup loop that, on a fixed tick, meters accepted telemetry by
   counting rows in the ClickHouse tables of record, grouped by tenant and by
   window, and writing per-signal usage rollups to Postgres. A per-signal
   watermark ensures each closed window is counted exactly once.
2. A tenant-scoped HTTP API for usage, a billing-period summary with quota and
   cost, and invoice listing, retrieval, and closing.

```
ClickHouse (metrics_numeric, logs, spans)
      | rollup loop (every ROLLUP_TICK, windows of ROLLUP_INTERVAL)
      v
Postgres usage_rollups  --->  usage / summary / invoices API (admin key)
                                        |
                                   rate card + plan quotas
```

Storage lives in Postgres (`usage_rollups`, `metering_state`, `invoices`,
`invoice_line_items`), created by the control plane migrator as migration `0004`.
The metering service runs no migrations.

## What is metered

Usage is the count of accepted data points per signal: metric samples in
`metrics_numeric`, log records in `logs`, and spans in `spans`. Each signal keys
on its own time column (metrics and logs on `timestamp`, traces on `start_time`).
The rollup counts raw inserted rows, which is a close proxy for accepted ingest
volume. This is a deliberate tradeoff: it is simple and reuses ClickHouse as the
source of truth, but because it counts stored rows rather than metering at the
gateway, a consumer retry that re-inserts a row can be double counted. A precise
alternative, a dedicated usage-events consumer on the ingest stream, is noted
under what is deferred.

## Rollup semantics

The loop runs every `METERING_ROLLUP_TICK`. For each signal it computes the
current closed-window boundary (the present time truncated to
`METERING_ROLLUP_INTERVAL`), reads the signal's watermark, and rolls up every
window in `[watermark, boundary)` in one grouped ClickHouse query. Each
`(tenant, signal, window_start)` count is upserted into `usage_rollups`, then the
watermark advances to the boundary. Re-running the same window overwrites with the
same count, so the rollup is idempotent. On first run, with no watermark, it backs
up by `METERING_BACKFILL` so recent history is captured.

The interval is the metering granularity, not the billing period. Production would
use one hour; the compose stack uses one minute so usage appears within a minute
during a demo. Reporting and invoices sum rollups across whatever period is
requested, independent of the granularity.

## API

All routes require an API key with the `admin` scope, presented as
`Authorization: Bearer <key>` or `X-Prism-Key: <key>`. The authenticated key
determines the tenant.

| Method | Path                   | Purpose                                             |
|--------|------------------------|-----------------------------------------------------|
| GET    | `/v1/usage`            | per-signal counts over a period                     |
| GET    | `/v1/usage/summary`    | current month usage, quota, and estimated cost      |
| GET    | `/v1/invoices`         | list the tenant's invoices                          |
| GET    | `/v1/invoices/{id}`    | one invoice with line items                          |
| POST   | `/v1/invoices/close`   | price a period and persist it as a closed invoice   |

`from` and `to` are RFC3339 query parameters. For `/v1/usage` and
`/v1/invoices/close` they default to the current calendar month through the
present moment. Errors use problem+JSON: 400 for an invalid period (`from` not
before `to`), 401 for a missing or invalid key, 403 for a key without the admin
scope, 404 for an unknown invoice, and 500 otherwise.

### Summary example

```json
GET /v1/usage/summary
{
  "period_start": "2026-08-01T00:00:00Z",
  "period_end":   "2026-08-21T09:00:00Z",
  "usage":        {"metrics": 2000000, "logs": 1000000, "traces": 500000},
  "total_points": 3500000,
  "quota": {"plan": "free", "included": 10000000, "used": 3500000, "remaining": 6500000, "over": false},
  "cost":  {"line_items": [
             {"signal": "metrics", "quantity": 2000000, "unit_price_per_million": 0.10, "amount": 0.20},
             {"signal": "logs",    "quantity": 1000000, "unit_price_per_million": 0.50, "amount": 0.50},
             {"signal": "traces",  "quantity": 500000,  "unit_price_per_million": 0.20, "amount": 0.10}],
           "total": 0.80, "currency": "USD"}
}
```

## Pricing and quotas

The rate card prices each signal per one million points, from configuration. Cost
is a pure function of usage and the rate card, computed in a deterministic signal
order. Quotas are a per-plan monthly point allowance, also from configuration,
looked up by the tenant's plan (read from the tenants table). A negative allowance
means unlimited, in which case the tenant is never over quota. An unknown plan is
treated as unlimited so metering never reports a false over-quota.

Amounts are stored and returned as floating point for simplicity; a production
system would use integer minor units or a fixed-precision numeric type.

## Configuration

Reuses `DATABASE_URL`, the `CLICKHOUSE_*` variables, `REDIS_ADDR`,
`CONTROL_PLANE_VERIFY_URL`, and `INTERNAL_API_TOKEN`. Metering-specific:

- `HTTP_PORT` (default 8094)
- `METERING_ROLLUP_INTERVAL` (default 1h): metering granularity
- `METERING_ROLLUP_TICK` (default 5m): how often the loop runs
- `METERING_BACKFILL` (default 168h): first-run lookback
- `METERING_MAX_EXECUTION_SECONDS` (default 30): ClickHouse execution guard
- `METERING_CURRENCY` (default USD)
- `METERING_PRICE_METRICS_PER_M`, `METERING_PRICE_LOGS_PER_M`, `METERING_PRICE_TRACES_PER_M`
- `METERING_QUOTA_FREE`, `METERING_QUOTA_TEAM`, `METERING_QUOTA_ENTERPRISE` (points per month; -1 is unlimited)

## What is deferred

- A real payment provider. Invoices are generated and stored; charging through a
  provider such as Stripe is an adapter on top of the invoice model.
- Gateway-time metering via a usage-events stream consumer, which would count
  ingested points precisely rather than counting stored rows.
- Per-tenant custom pricing, discounts, commitments, and tiered or graduated
  rates. The rate card is a flat per-unit price per signal.
- Automatic scheduled period close. Closing is an API action; a monthly scheduler
  is a later concern.
- Quota enforcement. Over-quota status is surfaced for FinOps visibility; blocking
  or throttling ingest on breach is left to the gateway and a future policy.
- Cardinality-based and storage-based billing, and spend budgets with alerts
  (which could reuse the alerting service).
