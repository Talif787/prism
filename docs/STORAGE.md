# Storage: stream consumer and ClickHouse

Phase 3 closes the write path. The Phase 2 gateway produces validated OTLP to
Kafka; the consumer in this phase drains those topics, turns OTLP into columnar
rows, and batch-writes them to ClickHouse. That makes telemetry queryable, which
is what the Phase 4 query service and the frontend build on.

## Pipeline

```
Kafka (telemetry.metrics / telemetry.logs / telemetry.traces)
    -> consumer (one loop per signal, shared consumer group)
        -> decode OTLP protobuf
        -> transform to rows
        -> batch (size or time)
        -> ClickHouse INSERT (prepared batch)
        -> commit Kafka offsets
```

The Kafka message value is the collector-level OTLP request
(`ExportMetricsServiceRequest` and friends) exactly as the gateway stored it. The
message key is the tenant id, so tenant attribution comes from the key rather
than re-parsing the payload.

## Schema (migrations/clickhouse/0001_init.sql)

Three tables, one per signal. Shared design choices:

- `LowCardinality(String)` for label-like columns (metric name, severity, span
  kind, status). These columns have few distinct values, so dictionary encoding
  keeps them small and fast to filter.
- `Map(LowCardinality(String), String)` for arbitrary attributes and resource
  attributes. This keeps the schema stable as instrumentation changes, at the
  cost of slightly more expensive attribute filters (addressed later with
  materialized columns or skip indexes if needed).
- `DateTime64(9)` preserves OTLP nanosecond precision.
- `PARTITION BY toDate(...)` bounds part sizes to a day and makes TTL drops a
  cheap partition operation.
- `ENGINE = ReplacingMergeTree(inserted_at)` collapses duplicate rows (same
  ORDER BY tuple) during background merges, keeping the row with the latest
  `inserted_at`. This is what makes at-least-once delivery safe (see below).
- `TTL toDateTime(...) + INTERVAL 15 DAY` implements the hot tier. Warm and cold
  tiers (tiered storage, `TO VOLUME`) arrive with the infrastructure phase.

Table specifics:

`metrics_numeric` holds gauge and sum data points. `ORDER BY (tenant_id,
metric_name, series_fingerprint, timestamp)` gives strong locality for the common
query shape (one tenant, one metric, one series, a time range) and doubles as the
dedup key. `series_fingerprint` is a 64-bit hash of the resource attributes,
metric name, and data-point attributes, so each unique time series has a stable
id.

`logs` holds log records. Logs have no natural unique key, so a `log_id` (a hash
of timestamp, body, trace and span ids, and attributes) provides one. `ORDER BY
(tenant_id, timestamp, log_id)` supports time-range scans per tenant and enables
dedup of redelivered records.

`spans` holds spans. `ORDER BY (tenant_id, trace_id, span_id)` supports trace
lookups and gives natural dedup, since a span id is unique within a trace.

## Delivery and dedup

The consumer commits Kafka offsets only after a successful ClickHouse write, so
delivery is at-least-once: a crash between write and commit re-delivers the
batch. Because every table is a `ReplacingMergeTree` keyed on a natural (metrics,
spans) or synthetic (logs) identity, redelivered rows collapse on merge. In
practice that yields effectively-once storage for metrics and spans, and
near-once for logs.

Merges are asynchronous, so duplicates can be briefly visible before a merge.
Queries that must not see them can use `FINAL` or aggregate over the ORDER BY
key; the query service will apply the appropriate strategy per query.

## Batching

Each signal loop accumulates rows until it reaches `CONSUMER_BATCH_MAX_ROWS`
(default 5000) or `CONSUMER_BATCH_MAX_WAIT` elapses (default 1s), then writes one
prepared batch. Batched inserts are the efficient path for ClickHouse: many small
inserts create many small parts and heavy merge pressure, which batching avoids.

## Failure handling

- A write or commit error returns from the loop (fail fast). The process exits
  and the orchestrator restarts it; uncommitted messages are reprocessed and
  dedup absorbs any overlap.
- A decode or transform error skips only the offending message (it is committed
  so it is not retried forever), since a malformed payload will never succeed.

## Configuration

```
CLICKHOUSE_ADDR=localhost:9000     # native protocol
CLICKHOUSE_DATABASE=default
CLICKHOUSE_USERNAME=default
CLICKHOUSE_PASSWORD=
KAFKA_CONSUMER_GROUP=prism-consumer
CONSUMER_BATCH_MAX_ROWS=5000
CONSUMER_BATCH_MAX_WAIT=1s
```

In docker-compose the ClickHouse service auto-creates database, user, and
password all set to `prism`, and the consumer connects with those values.

## Querying examples

Latest value per series for a metric:

```sql
SELECT series_fingerprint, argMax(value, timestamp) AS latest
FROM metrics_numeric
WHERE tenant_id = 't1' AND metric_name = 'http.server.duration'
  AND timestamp >= now() - INTERVAL 1 HOUR
GROUP BY series_fingerprint;
```

Error logs in a window:

```sql
SELECT timestamp, body, attributes
FROM logs
WHERE tenant_id = 't1' AND severity_number >= 17
  AND timestamp >= now() - INTERVAL 15 MINUTE
ORDER BY timestamp DESC
LIMIT 100;
```

Slowest spans by operation:

```sql
SELECT name, quantile(0.99)(duration_ns) AS p99_ns, count() AS n
FROM spans
WHERE tenant_id = 't1' AND start_time >= now() - INTERVAL 1 HOUR
GROUP BY name
ORDER BY p99_ns DESC
LIMIT 20;
```

## Deferred

- Histogram, exponential histogram, and summary metric types. These need
  bucket-aware tables and are transformed in a later drop; gauge and sum cover
  the dashboards the frontend starts with.
- OTLP/gRPC ingestion (Phase 3b). It is additive to the gateway and reuses the
  Phase 2 pipeline unchanged; OTLP/HTTP already ingests all three signals, so
  gRPC is not on the path to storage.
