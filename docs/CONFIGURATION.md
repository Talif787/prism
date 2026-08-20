# Configuration

All configuration is read from the environment at startup and validated before the service
starts. Invalid configuration fails fast with a list of every problem found. See
`.env.example` for a complete template.

| Variable | Default | Description |
| --- | --- | --- |
| APP_ENV | local | One of local, dev, staging, production. Production forbids AUTH_MODE=dev. |
| HTTP_HOST | 0.0.0.0 | Bind address. |
| HTTP_PORT | 8080 | Bind port. |
| HTTP_READ_TIMEOUT | 15s | Request read timeout. |
| HTTP_WRITE_TIMEOUT | 30s | Response write timeout. |
| HTTP_SHUTDOWN_TIMEOUT | 20s | Grace period for draining on shutdown. |
| LOG_LEVEL | info | debug, info, warn, or error. |
| LOG_FORMAT | json | json or text. |
| DATABASE_URL | (required) | PostgreSQL DSN. |
| DATABASE_MAX_CONNS | 20 | Pool maximum connections. |
| DATABASE_MIN_CONNS | 2 | Pool minimum connections. |
| DATABASE_CONN_MAX_LIFETIME | 30m | Maximum connection lifetime. |
| AUTH_MODE | dev | dev (HS256) or oidc. |
| AUTH_DEV_HS256_SECRET | (required in dev) | Shared secret for local token verification. |
| AUTH_OIDC_ISSUER | (required in oidc) | OIDC issuer URL for JWKS discovery. |
| AUTH_OIDC_AUDIENCE | prism-console | Expected token audience. |
| INTERNAL_API_TOKEN | (required) | Service-to-service token for internal endpoints. |
| OTEL_ENABLED | true | Enable OpenTelemetry tracing. |
| OTEL_SERVICE_NAME | prism-controlplane | Resource service name. |
| OTEL_EXPORTER_OTLP_ENDPOINT | localhost:4317 | OTLP gRPC collector endpoint. |
| OTEL_TRACES_SAMPLER_RATIO | 1.0 | Parent-based ratio sampler, 0.0 to 1.0. |

## Secrets

No secrets are baked into images or committed. In production, inject DATABASE_URL,
INTERNAL_API_TOKEN, and OIDC settings from a secret manager. The container runs as nonroot
with a read-only-friendly distroless base.
