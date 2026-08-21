# Phase 3 apply guide: stream consumer and ClickHouse writer

This delta adds the consumer that drains Kafka telemetry into ClickHouse. It is
additive to Phases 1 and 2. Apply it over your existing repo tree.

## 1. Unzip over the repo root

From the repository root (the directory that contains go.mod):

    unzip -o prism-phase3.zip -d .

New files added: internal/consumer/**, internal/platform/clickhouse/**,
internal/platform/kafka/consumer.go, migrations/clickhouse/**,
cmd/consumer/main.go, docs/STORAGE.md, test/integration/clickhouse_test.go.
Changed files: go.mod, Makefile, README.md, .env.example,
deployments/docker-compose.yaml, internal/platform/config/config_services.go.

## 2. Normalize the module path (idempotent)

The drop uses the placeholder module path. Rewrite it to yours. This is safe to
run repeatedly and leaves already-correct files untouched:

    grep -rl 'github.com/prism-obs/prism' --include='*.go' . \
      | xargs -r sed -i 's#github.com/prism-obs/prism#github.com/Talif787/prism#g'
    sed -i 's#^module github.com/prism-obs/prism#module github.com/Talif787/prism#' go.mod

## 3. Resolve dependencies (needs network)

    go mod tidy

This pulls the ClickHouse driver (github.com/ClickHouse/clickhouse-go/v2) and the
testcontainers ClickHouse module, and locks the graph in go.sum.

## 4. Build and unit test

    make build      # builds controlplane, gateway, relay, and now consumer
    make test       # unit tests, including the OTLP transform tests

## 5. Integration test (optional, needs Docker)

    go test -tags=integration ./test/integration/...

The new clickhouse_test.go starts a ClickHouse container, applies the schema,
writes metric, log, and span rows through the real writer, reads them back, and
verifies ReplacingMergeTree dedup on a redelivered span.

## 6. Run the full stack

    make up

Note: the stack is now nine containers (postgres, redis, redpanda, jaeger,
controlplane, gateway, relay, clickhouse, consumer). That is heavy for Cloud
Shell. If it strains memory, options in order of preference:

- Run detached and start only what you need. For example, to exercise storage in
  isolation you can bring up clickhouse and consumer plus redpanda, then produce
  a few messages, rather than the whole pipeline.
- Stop jaeger (tracing backend) while testing storage; the services degrade
  gracefully with OTEL disabled or the collector absent.
- Bring the stack up in stages: docker compose up -d postgres redis redpanda
  clickhouse, wait for healthy, then the app services.

If the image tag clickhouse/clickhouse-server:24.8-alpine fails to pull in your
environment, drop the -alpine suffix (use 24.8); it is larger but always present.

## What this phase does and does not do

Does: consumes telemetry.metrics, telemetry.logs, and telemetry.traces under one
consumer group; transforms OTLP to columnar rows; batches and writes to three
ClickHouse tables (metrics_numeric, logs, spans) with dedup and 15-day TTL;
commits Kafka offsets only after a successful write (at-least-once). See
docs/STORAGE.md.

Deferred, by design and stated up front:
- Histogram, exponential histogram, and summary metric types. Gauge and sum are
  implemented now (what dashboards chart first); the others need bucket-aware
  tables and land in a later drop.
- OTLP/gRPC ingestion (Phase 3b). It is additive to the gateway and reuses the
  Phase 2 pipeline unchanged. OTLP/HTTP already ingests all three signals, so
  gRPC is not on the path to storage.

## Likely first-build friction

These are the spots most sensitive to exact third-party API shapes across
versions; if go build flags anything, it will most likely be here, and the fix is
small:
- clickhouse-go/v2 option and batch API (Open options, PrepareBatch, Append).
- the testcontainers ClickHouse module (Run, With* options, ConnectionHost).
Report the exact compiler output and I will patch it surgically, as in prior
phases.
