# Ingest Pipeline (Phase 2)

The ingest gateway is the write path's front door. It accepts OpenTelemetry data
over OTLP/HTTP, authenticates the tenant, enforces per-tenant limits, and hands
validated telemetry to Kafka, where the Phase 3 consumer will read it and write to
ClickHouse. A companion process, the outbox relay, drains the control plane's
transactional outbox to Kafka so domain events (tenant created, key revoked, and
so on) reach the rest of the system reliably.

## Services

- `cmd/gateway`: the OTLP/HTTP receiver and ingest pipeline. Default port 8090.
- `cmd/relay`: the outbox relay. No inbound port; it polls Postgres and produces to Kafka.

## Endpoints

The gateway serves the standard OTLP/HTTP paths, so an OpenTelemetry SDK or
Collector pointed at the gateway with no path override works out of the box.

- `POST /v1/metrics`
- `POST /v1/traces`
- `POST /v1/logs`
- `GET /healthz` (liveness), `GET /readyz` (readiness: Redis and Kafka reachable)

Both `application/x-protobuf` (the OTLP default) and `application/json` request
bodies are accepted. Regardless of what the client sends, the gateway places
protobuf bytes on Kafka so the consumer only ever decodes one format.

## Authentication

Every request must carry a tenant API key (minted by the control plane, scope
`ingest`). The key travels in one of two headers, both settable through
`OTEL_EXPORTER_OTLP_HEADERS`:

- `Authorization: Bearer <api-key>`
- `X-Prism-Key: <api-key>`

The gateway does not hold the key database. It verifies keys against the control
plane's internal endpoint (`POST /internal/v1/keys/verify`) and caches the result
in Redis, keyed by the SHA-256 of the key so plaintext credentials never sit in
the cache. The common path is a single Redis lookup. Cache entries expire after
`AUTH_CACHE_TTL` (default 60s), which bounds revocation latency: a revoked key
keeps working until its cache entry expires, so the TTL is kept short. Invalid
keys are negatively cached for `AUTH_NEGATIVE_CACHE_TTL` to blunt credential
stuffing. Verification failures fail closed (the request is rejected).

## Rate limiting

Each tenant has a token bucket in Redis. Tokens are data points: a metrics
request costs one token per data point, a traces request one per span, a logs
request one per log record. The refill-and-take step runs as a single Lua script,
so the limit is atomic and shared across gateway replicas: three gateways enforce
one true per-tenant limit, not three separate ones. Over-limit requests receive
`429 Too Many Requests` with a `Retry-After` header and are not produced to Kafka.

Defaults: `RATE_LIMIT_PER_SECOND=50000`, `RATE_LIMIT_BURST=100000`.

## Cardinality guard

Runaway label cardinality is the most common way an observability bill explodes,
so the gateway tracks the approximate number of distinct metric series per tenant
using a Redis HyperLogLog. A series is identified by its resource attributes,
metric name, and data point attributes. HLL keeps this to roughly 12KB per tenant
and is idempotent, so established series are free to keep flowing; only growth
past the budget is affected.

The guard defaults to observe-only (`CARDINALITY_ENFORCE=false`): it records the
estimate and logs when a tenant exceeds budget, but admits the data. Setting
`CARDINALITY_ENFORCE=true` rejects new series past `CARDINALITY_BUDGET` with a
`429`. The estimate resets after `CARDINALITY_WINDOW` of inactivity.

## Kafka message format

Telemetry is produced to one topic per signal (`telemetry.metrics`,
`telemetry.logs`, `telemetry.traces`), keyed by tenant id so a tenant's records
share a partition and keep their relative order. Metadata travels in Kafka
headers; the message value is the raw OTLP protobuf payload.

Headers: `tenant_id`, `signal`, `received_at_unix_nano`, `content_type`,
`schema_version`, `point_count`.

## Outbox relay

The control plane writes domain events to an `outbox` table in the same
transaction that changes tenancy state, which guarantees an event exists if and
only if its state change committed. The relay completes the pattern: it polls the
table, publishes unpublished rows to `tenancy.events` (keyed by tenant id), and
marks them published. Rows are claimed with `FOR UPDATE SKIP LOCKED`, so multiple
relay instances can run without double-processing a row.

Delivery is at-least-once: a crash between publishing and marking published will
re-send a row, so consumers must be idempotent. Failed publishes increment an
`attempts` counter and record `last_error`; rows that exhaust `RELAY_MAX_ATTEMPTS`
stop being polled and remain for operator inspection. A full dead-letter topic is
a later enhancement. The relay produces while holding the row lock; batches are
small, so lock duration stays short.

## Local usage

Bring up the full stack (Postgres, Redis, Redpanda, Jaeger, control plane,
gateway, relay):

```bash
make up
```

Create a tenant and mint an ingest key (via the control plane), then point an
OpenTelemetry exporter at the gateway:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:8090
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer <your-ingest-key>"
```

A quick manual check with an empty (but valid) protobuf body returns `200`:

```bash
curl -i -X POST http://localhost:8090/v1/metrics \
  -H "Authorization: Bearer <your-ingest-key>" \
  -H "Content-Type: application/x-protobuf" \
  --data-binary @/dev/null
```

Consume produced records with any Kafka tool against `localhost:9092`, for example
Redpanda's `rpk topic consume telemetry.metrics`.

## Configuration

See the Phase 2 section of `.env.example` for the full list. Key variables:
`REDIS_ADDR`, `KAFKA_BROKERS`, `CONTROL_PLANE_VERIFY_URL`, `INTERNAL_API_TOKEN`,
`RATE_LIMIT_PER_SECOND`, `RATE_LIMIT_BURST`, `CARDINALITY_BUDGET`,
`CARDINALITY_ENFORCE`, `MAX_BODY_BYTES`, and the `RELAY_*` settings.

## Deferred to Phase 3

OTLP/gRPC ingestion is deferred. It reuses this exact pipeline (authenticate,
rate limit, cardinality, produce); only the transport differs, so it is purely
additive. It lands alongside the stream consumer and ClickHouse writer.

## OTLP/gRPC (Phase 3b)

The gateway serves OTLP/gRPC on port 4317 alongside OTLP/HTTP, for the many
OpenTelemetry SDKs and Collectors that default to gRPC. Both transports share one
pipeline: authenticate the API key, enforce the per-tenant rate and cardinality
limits, and produce protobuf to Kafka. The gRPC path adds no new pipeline logic;
it marshals the received export request to protobuf and reuses the same domain
point-counting and fingerprinting as the HTTP path, so a metric ingested over gRPC
is indistinguishable downstream from the same metric ingested over HTTP.

Services implemented: the standard OTLP MetricsService, LogsService, and
TraceService Export methods. Gzip-compressed requests are accepted (the gzip
encoding is registered on the server). The maximum received message size matches
the HTTP body limit.

Authentication uses gRPC metadata, either `authorization: Bearer <key>` or
`x-prism-key: <key>`, both settable through `OTEL_EXPORTER_OTLP_HEADERS`. Pipeline
outcomes map to gRPC status codes: an invalid key is Unauthenticated, a key
without the ingest scope is PermissionDenied, and rate-limit or cardinality
rejection is ResourceExhausted. A successful export returns an empty response
(full success); partial-success reporting is a later addition.

The transport can be turned off with `GATEWAY_GRPC_ENABLED=false`.

Deferred: TLS and mTLS on the listener (plaintext locally, like the HTTP path;
termination belongs at the ingress in Phase 7), gRPC health and reflection
services, and partial-success reporting of rejected points.

### Sending OTLP/gRPC

Any OpenTelemetry SDK or the Collector's otlp exporter works. Point it at the
gateway and pass the key as a header:

```
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
OTEL_EXPORTER_OTLP_PROTOCOL=grpc
OTEL_EXPORTER_OTLP_HEADERS=x-prism-key=<your ingest key>
```
