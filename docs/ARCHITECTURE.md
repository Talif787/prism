# Architecture: Control Plane (Phase 1)

## Purpose and boundaries

The control plane is the tenancy bounded context. It is the source of truth for who can
do what: tenants, users, memberships, roles, and API keys. It deliberately does not touch
telemetry data. Telemetry lives in the data plane (later phases), which treats the control
plane as its authority for credential verification.

## Layering (Clean Architecture)

Four layers, with dependencies pointing inward:

1. Domain (`internal/tenancy/domain`): entities, value objects, and invariants. No imports
   of infrastructure or transport. The API-key hashing, scope logic, role-to-permission
   mapping, and the last-owner invariant live here and are unit-tested in isolation.
2. Application (`internal/tenancy/app`): use cases that orchestrate the domain through
   ports (interfaces) it defines. The unit of work makes each use case atomic. Authorization
   is a use case here, not scattered across handlers.
3. Infrastructure (`internal/tenancy/infra/pgstore`): PostgreSQL implementations of the
   ports, plus the transactional outbox. This is the only layer that knows SQL.
4. Transport (`internal/tenancy/api/rest`): HTTP handlers, request and response DTOs, auth
   middleware, and routing. It maps errors to RFC 7807 problem responses at one place.

Platform packages (`internal/platform/*`) are shared, service-agnostic building blocks:
configuration, logging, tracing, the HTTP server, the database pool and migrator, and token
verification.

## Key design decisions

Modular monolith. Phase 1 is a single deployable. The system splits out the ingest gateway
and stream writer in later phases because those tiers have genuinely different scaling and
failure profiles; the rest stays cohesive.

Ports and adapters. The application layer depends on interfaces, so the domain and use cases
are tested with in-memory fakes, and the database is swappable and tested separately with
testcontainers.

Unit of work. A single transaction wraps each multi-step use case (for example, create a
tenant and its first owner and emit events), so partial writes cannot occur. The hot-path key
verification uses a pool-bound repository instead, since it is a single indexed read.

Transactional outbox. Domain events are written to an outbox table in the same transaction as
the state change. A relay (Phase 2) publishes them to Kafka. This gives at-least-once delivery
without distributed transactions.

API-key security. Keys are random 256-bit-class tokens. Only the SHA-256 hash and a public
prefix are stored. Authentication looks the key up by its indexed prefix, then compares hashes
in constant time, then checks status, expiry, and scope. SHA-256 (not bcrypt) is correct here
because the tokens are high-entropy, so a fast hash does not weaken them and keeps the hot path
fast.

Authentication strategy. Console tokens are verified by a `TokenVerifier` with two
implementations selected by config: an OIDC verifier for real environments and an HS256 dev
verifier for local use. `AUTH_MODE=dev` is rejected in production by config validation.

## Request lifecycle

A console request passes through request-id assignment, tracing, panic recovery, and access
logging, then the route's auth middleware verifies the bearer token and binds the principal.
The handler resolves the tenant from the path and calls the authorization use case, which
resolves the principal to a membership and checks the required permission. Only then does the
handler invoke the business use case. Errors are translated to problem responses centrally.

## Data model

`tenants`, `users`, `memberships` (composite key tenant plus user, with a partial index on
owners), `api_keys` (unique prefix for O(1) auth lookup), and `outbox`. Constraints enforce
valid enums at the database boundary as a backstop to domain validation.

## Observability

Every request is traced with OpenTelemetry and logged as structured JSON with the trace and
span ids attached automatically, so logs and traces correlate. Liveness is trivial; readiness
checks the database. The service emits its own telemetry through the same OTLP pipeline the
product uses, which is how Prism dogfoods itself.
