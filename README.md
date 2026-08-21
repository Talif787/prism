# Prism Control Plane

The control plane is the identity and access core of Prism, a multi-tenant
observability platform. It owns tenants, users, memberships (RBAC), and API keys,
and exposes an internal endpoint that the ingest and query gateways call to verify
credentials. This repository is being built in phases; Phases 1 through 4 plus 3b are in place.

## What is in Phase 1

A production-shaped control-plane service:

- Tenant, user, membership, and API-key management over a versioned REST API.
- RBAC with owner, admin, editor, and viewer roles, enforced at a single choke point.
- API keys minted as high-entropy tokens, stored only as SHA-256 hashes, with scopes
  (ingest, query, admin), expiry, and revocation.
- Clean Architecture: domain, application, infrastructure, and transport layers with
  dependency inversion, so business rules are testable without a database.
- Transactional outbox for domain events, ready for the Phase 2 Kafka relay.
- Structured logging, OpenTelemetry tracing, health and readiness probes, RFC 7807
  error responses, graceful shutdown, and twelve-factor configuration.
- Unit tests (domain and application) and Docker-based integration tests.

## Architecture at a glance

Requests enter the transport layer (`internal/tenancy/api/rest`), which maps them to
use cases in the application layer (`internal/tenancy/app`). Use cases orchestrate the
domain (`internal/tenancy/domain`) through repository ports and run inside a unit of
work. The PostgreSQL adapters (`internal/tenancy/infra/pgstore`) implement those ports.
Cross-cutting platform concerns (config, logging, tracing, HTTP server, database pool,
token verification) live under `internal/platform`. Dependencies point inward: nothing
in the domain imports infrastructure or transport.

See `docs/ARCHITECTURE.md` for the full picture and `api/openapi.yaml` for the API.

## Quick start

```bash
cp .env.example .env
make up            # starts postgres, jaeger, and the control plane
```

The service listens on `:8080`. Traces appear in Jaeger at http://localhost:16686.

Create a tenant (internal token, since self-service onboarding arrives with billing):

```bash
curl -s -X POST http://localhost:8080/v1/tenants \
  -H "X-Internal-Token: local-internal-token-change-me" \
  -H "Content-Type: application/json" \
  -d '{"name":"Acme","slug":"acme","plan":"team","owner_email":"owner@acme.io","owner_name":"Owner"}'
```

Console calls use a bearer token. In local dev (`AUTH_MODE=dev`) the service accepts an
HS256 token signed with `AUTH_DEV_HS256_SECRET`; see `docs/LOCAL_DEVELOPMENT.md` for how
to mint one and call the authenticated endpoints.

## Development

```bash
make tidy    # resolve dependencies (first run, needs network)
make test    # unit tests
make lint    # golangci-lint
make test-integration   # requires Docker
```

## Roadmap

Phase 1: control plane (tenancy, keys, RBAC).
Phase 2: ingest gateway (OTLP/HTTP receiver for metrics, logs, and traces,
cached API-key auth, per-tenant Redis rate limiting, Redis HyperLogLog cardinality guard,
Kafka production) plus the Kafka outbox relay. See `docs/INGEST.md`.
Phase 3: stream consumer and ClickHouse writer. Consumes telemetry from Kafka,
transforms OTLP into columnar rows, and batch-writes metrics (gauge and sum), logs, and
spans to ClickHouse with dedup and retention TTLs. See `docs/STORAGE.md`.
Phase 4: query service. A tenant-scoped read API over ClickHouse for metric
discovery, time-bucketed range queries, log search, and traces, with API-key auth, cost
guards, and a discovery cache. See `docs/QUERY.md`.
Phase 3b: OTLP/gRPC receiver on the gateway (port 4317), sharing the
same pipeline as OTLP/HTTP. Most OpenTelemetry SDKs and Collectors default to gRPC.
Phase 5 (current): alerting. A tenant-scoped service (port 8093) with a rule CRUD API,
a background engine that evaluates threshold rules against ClickHouse through the
pending, firing, and resolved state machine (honoring a "for" duration), and webhook
notifications. Admin-scoped API keys. See `docs/ALERTING.md`.
Phase 6: metering and FinOps. Phase 7: Kubernetes, Helm, Terraform,
CI/CD, and load and chaos testing.

Deferred: histogram and summary metric types (bucket-aware tables, a later phase); and, within Phase 4, a PromQL expression language and
console-token auth for direct browser access (query keys are the machine path for now).

## Note on dependency locking

`go.sum` is intentionally not committed in this snapshot. Run `make tidy` once (it needs
network access) to resolve and lock the module graph before the first build.
