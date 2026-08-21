# Query service (read path)

Phase 4 turns the ClickHouse tables into a tenant-scoped HTTP API. It is the read
side of the system: the control plane manages tenancy, the gateway ingests, the
consumer stores, and the query service reads. A frontend calls these endpoints to
plot metrics and browse logs and traces.

## Authentication and tenancy

Every request carries a query-scoped API key, either as `Authorization: Bearer
<key>` or `X-Prism-Key: <key>`. The service verifies the key against the control
plane's internal endpoint (scope `query`) and caches the result in Redis, so the
common case is a single Redis GET. The verified tenant id, not any request
parameter, scopes every query, so one tenant can never read another's data. Keys
are issued exactly like ingest keys, with `scopes: ["query"]`.

## Endpoints

All paths are under `/v1`. Time parameters are RFC3339; `from` and `to` default
to the last hour when omitted.

`GET /metrics/names` returns the distinct metric names for the tenant in the
window. Served from a short per-tenant cache.

`GET /metrics/query` runs a time-bucketed aggregation over one metric:
- `name` (required), `from`, `to`, `step` (bucket width, default `1m`).
- `agg` one of avg, sum, min, max, count, last (default avg).
- `group_by` a comma-separated list of attribute keys; each distinct combination
  becomes a series with those labels.
- `filter` repeated, each `key=value`, restricting to matching attributes.

The response is a set of series, each with its label set and an ordered list of
`{t, v}` points.

`GET /logs` searches log records: `from`, `to`, `min_severity` (numeric floor),
`q` (case-insensitive body substring), `limit`. Newest first.

`GET /traces` lists root spans (trace entry points): `from`, `to`, `service`,
`limit`. Newest first.

`GET /traces/{trace_id}` returns every span for one trace, oldest first, for a
waterfall view.

## Cost guards

A shared analytical store needs protection from expensive queries. The service
enforces, before touching ClickHouse: a maximum time range (31 days), a maximum
bucket count per range query (11000, so the step must widen for long windows), a
cap on group-by attributes, and a hard cap on returned rows for logs and traces
(1000). On top of that, the ClickHouse connection sets `max_execution_time`, so a
query that slips past the pre-checks still cannot run unbounded. Violations return
`400` with a message naming the guard.

## Design notes

The store builds SQL with positional parameters for every user-supplied value
(metric names, attribute keys and values, search text), so the read path is
injection-safe. Range queries bucket with `toStartOfInterval` and group by the
selected attributes via SELECT aliases, which keeps each attribute key a single
bound parameter. Metric-name discovery is cached briefly per tenant because the
set of names changes slowly; result caching for fixed windows is a later addition.

## Example

```
# discover metrics
curl -s "$Q/v1/metrics/names" -H "X-Prism-Key: $QUERY_KEY"

# 5-minute average of a metric over the last hour, split by route
curl -s -G "$Q/v1/metrics/query" -H "X-Prism-Key: $QUERY_KEY" \
  --data-urlencode "name=http.server.active_requests" \
  --data-urlencode "step=5m" \
  --data-urlencode "agg=avg" \
  --data-urlencode "group_by=route"

# error logs containing a substring
curl -s -G "$Q/v1/logs" -H "X-Prism-Key: $QUERY_KEY" \
  --data-urlencode "min_severity=17" --data-urlencode "q=timeout"
```

## Deferred

- A PromQL expression language. This slice takes structured parameters; a PromQL
  or SQL front end can be layered on the same store later.
- Console-token (OIDC) authentication for direct browser use. Query keys are the
  machine path; the browser path lands with the frontend.
- Histogram and summary metrics, still deferred from Phase 3.
