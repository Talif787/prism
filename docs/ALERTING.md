# Alerting service (Phase 5)

The alerting service (`cmd/alerter`, port 8093) lets a tenant define threshold
rules over their metrics, evaluates those rules continuously against ClickHouse,
and delivers webhook notifications when an alert fires or resolves. It reuses the
Phase 4 read path for evaluation, the control plane for authentication, and the
existing Postgres database for rule and instance storage.

## Design at a glance

The service has two responsibilities that run in one process:

1. A tenant-scoped HTTP API for rule CRUD and for listing current alerts.
2. A background evaluation loop that, on a fixed tick, evaluates every rule whose
   interval has elapsed, advances a per-series state machine, persists the active
   alert instances, and fires notifications on transitions.

Storage lives in Postgres (`alert_rules`, `alert_instances`), created by the
control plane migrator as migration `0003`. The alerter itself runs no
migrations. Evaluation reads metrics by reusing the query service's ClickHouse
store, so a rule sees exactly the numbers a dashboard would.

```
client --HTTP(admin key)--> alerter API --> Postgres (rules, instances)
                                  ^
                                  | eval loop (every ALERTER_EVAL_INTERVAL)
                                  v
                          ClickHouse (via query read path) --> webhook
```

## The rule model

A rule is a structured threshold condition over one metric. It is intentionally
not a query language: the fields map directly onto the Phase 4 range query plus a
comparison.

| Field         | Meaning                                                            |
|---------------|-------------------------------------------------------------------|
| `metric`      | metric name to evaluate                                           |
| `agg`         | aggregation over the window: avg, sum, min, max, count, last      |
| `group_by`    | attributes that split the metric into series (max 8)             |
| `filters`     | attribute equality filters applied before aggregation             |
| `window`      | trailing window aggregated into one value per series              |
| `operator`    | gt, gte, lt, or lte                                               |
| `threshold`   | the value compared against                                        |
| `for`         | how long the condition must hold before firing (0 fires at once)  |
| `interval`    | how often the rule is evaluated                                   |
| `severity`    | free-form label carried into the notification (default warning)   |
| `labels`      | static labels attached to the alert                              |
| `annotations` | static annotations (for example a runbook link) sent to webhooks  |
| `webhook`     | destination URL for firing and resolved notifications             |
| `enabled`     | whether the rule is evaluated (default true)                     |

Durations are Go duration strings in the API (`5m`, `30s`, `1h`) and are stored
as integer seconds.

## Evaluation semantics

On each evaluation the rule becomes a single-bucket range query over
`[now - window, now]` with the bucket width equal to the window, so each series
collapses to one aggregated value. That value is compared against the threshold
with the operator. A series is identified by an order-independent fingerprint of
its group-by label set, so the same series maps to the same alert instance across
evaluations.

### State machine

Each series moves through three states. Only pending and firing are stored;
resolving deletes the instance.

- inactive to pending: the condition first breaches. If `for` is zero, the
  instance goes straight to firing and a firing notification is sent.
- pending to firing: the condition has held continuously for at least `for`. A
  firing notification is sent.
- firing or pending to resolved: the condition no longer breaches, or the series
  stops appearing in results. The instance is deleted. A resolved notification is
  sent only if it had reached firing.

A single rule failing to evaluate is logged and does not stop the cycle. The base
tick is `ALERTER_EVAL_INTERVAL`; a rule is only evaluated when its own `interval`
has elapsed since its last evaluation, tracked by `last_evaluated_at`.

## API

All routes require an API key with the `admin` scope, presented as
`Authorization: Bearer <key>` or `X-Prism-Key: <key>`. The authenticated key
determines the tenant; there is no cross-tenant access. Keys are verified against
the control plane and cached in Redis, so the common path is a single Redis read.

| Method | Path             | Purpose                          |
|--------|------------------|----------------------------------|
| POST   | `/v1/rules`      | create a rule                    |
| GET    | `/v1/rules`      | list the tenant's rules          |
| GET    | `/v1/rules/{id}` | fetch one rule                   |
| PUT    | `/v1/rules/{id}` | replace a rule                   |
| DELETE | `/v1/rules/{id}` | delete a rule and its instances  |
| GET    | `/v1/alerts`     | list current pending and firing alerts |

Errors use the shared problem+JSON shape: 400 for an invalid rule or body, 401
for a missing or invalid key, 403 for a key without the admin scope, 404 for an
unknown rule, and 500 otherwise.

### Create example

```json
POST /v1/rules
{
  "name": "grpc latency high",
  "metric": "demo.grpc",
  "agg": "avg",
  "group_by": ["route"],
  "window": "1m",
  "operator": "gt",
  "threshold": 100,
  "for": "0s",
  "interval": "30s",
  "severity": "warning",
  "annotations": {"runbook": "https://runbooks.example/grpc"},
  "webhook": "https://webhook.site/your-endpoint"
}
```

## Webhook payload

On firing and resolved transitions the service POSTs JSON to the rule's webhook:

```json
{
  "event": "firing",
  "state": "firing",
  "rule_id": "…",
  "rule_name": "grpc latency high",
  "severity": "warning",
  "tenant_id": "…",
  "metric": "demo.grpc",
  "operator": "gt",
  "threshold": 100,
  "value": 142.7,
  "labels": {"route": "/checkout"},
  "annotations": {"runbook": "https://runbooks.example/grpc"},
  "active_since": "…",
  "fired_at": "…"
}
```

A non-2xx or non-3xx webhook response is logged and does not change the alert
state. Delivery is best-effort and not retried in this phase.

## Configuration

Reuses `DATABASE_URL`, the `CLICKHOUSE_*` variables, `REDIS_ADDR`,
`CONTROL_PLANE_VERIFY_URL`, and `INTERNAL_API_TOKEN`. Alerting-specific:

- `HTTP_PORT` (default 8093)
- `ALERTER_EVAL_INTERVAL` (default 15s): base evaluation tick
- `ALERTER_WEBHOOK_TIMEOUT` (default 5s): per-notification HTTP timeout
- `ALERTER_MAX_EXECUTION_SECONDS` (default 15): ClickHouse execution guard

## What is deferred

- Alertmanager-style grouping, inhibition, and silencing.
- Notification channels beyond webhook. Email, Slack, and PagerDuty are adapters
  on the same notifier port.
- An expression language. Rules are structured conditions, consistent with the
  query service.
- Log-based and trace-based alerts. This phase alerts on metrics only.
- High-availability evaluation. A single instance evaluates; running more than
  one would double-evaluate, so leader election is a later concern.
- Read access for query-scoped keys. Dashboards that want to render alerts with a
  query key will need the read endpoints broadened; today all routes require
  admin.
