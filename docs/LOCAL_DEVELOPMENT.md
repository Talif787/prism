# Local Development

## Prerequisites

Go 1.23+, Docker, and (optionally) golangci-lint and goimports.

## First run

```bash
cp .env.example .env
make tidy         # resolves and locks dependencies (needs network, run once)
make up           # postgres + jaeger + control plane via docker compose
```

Or run the service directly against a local Postgres:

```bash
export $(grep -v '^#' .env | xargs)
make run
```

Migrations run automatically on startup.

## Minting a dev bearer token

In `AUTH_MODE=dev` the service accepts an HS256 JWT signed with `AUTH_DEV_HS256_SECRET`,
with an `aud` of `prism-console`, an `exp` in the future, and an `email` that matches a
member of the tenant you are calling. Any small script or jwt.io can produce one. Example
claims:

```json
{ "sub": "user-123", "email": "owner@acme.io", "aud": "prism-console", "exp": 1893456000 }
```

Then call an authenticated endpoint:

```bash
curl -s http://localhost:8080/v1/tenants/<TENANT_ID> \
  -H "Authorization: Bearer <TOKEN>"
```

## Tests

```bash
make test                # unit tests (no Docker)
make test-integration    # spins up Postgres via testcontainers
make cover               # coverage summary
```

## Common issues

If readiness returns 503, the database is not reachable; check `DATABASE_URL` and that the
Postgres container is healthy. If console calls return 403, the token's email is not a member
of that tenant or lacks the required role.
