-- ClickHouse telemetry schema. Design notes:
--   * LowCardinality(String) for label-like columns keeps dictionaries small.
--   * Map(...) stores arbitrary resource and data-point attributes.
--   * DateTime64(9) preserves OTLP nanosecond timestamps.
--   * PARTITION BY day bounds part sizes and makes TTL drops cheap.
--   * ORDER BY is chosen for query locality and to give ReplacingMergeTree a
--     natural dedup key, so at-least-once redelivery collapses on merge.
--   * TTL implements the 15-day hot tier; warm and cold tiers arrive with
--     tiered storage in a later phase.

CREATE TABLE IF NOT EXISTS metrics_numeric
(
    tenant_id           String,
    metric_name         LowCardinality(String),
    metric_type         LowCardinality(String),
    timestamp           DateTime64(9),
    value               Float64,
    attributes          Map(LowCardinality(String), String),
    resource_attributes Map(LowCardinality(String), String),
    scope_name          LowCardinality(String),
    series_fingerprint  UInt64,
    inserted_at         DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(inserted_at)
PARTITION BY toDate(timestamp)
ORDER BY (tenant_id, metric_name, series_fingerprint, timestamp)
TTL toDateTime(timestamp) + INTERVAL 15 DAY
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS logs
(
    tenant_id           String,
    timestamp           DateTime64(9),
    observed_timestamp  DateTime64(9),
    severity_number     Int32,
    severity_text       LowCardinality(String),
    body                String,
    trace_id            String,
    span_id             String,
    attributes          Map(LowCardinality(String), String),
    resource_attributes Map(LowCardinality(String), String),
    scope_name          LowCardinality(String),
    log_id              UInt64,
    inserted_at         DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(inserted_at)
PARTITION BY toDate(timestamp)
ORDER BY (tenant_id, timestamp, log_id)
TTL toDateTime(timestamp) + INTERVAL 15 DAY
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS spans
(
    tenant_id           String,
    trace_id            String,
    span_id             String,
    parent_span_id      String,
    name                LowCardinality(String),
    kind                LowCardinality(String),
    start_time          DateTime64(9),
    end_time            DateTime64(9),
    duration_ns         UInt64,
    status_code         LowCardinality(String),
    status_message      String,
    attributes          Map(LowCardinality(String), String),
    resource_attributes Map(LowCardinality(String), String),
    scope_name          LowCardinality(String),
    inserted_at         DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(inserted_at)
PARTITION BY toDate(start_time)
ORDER BY (tenant_id, trace_id, span_id)
TTL toDateTime(start_time) + INTERVAL 15 DAY
SETTINGS index_granularity = 8192;
