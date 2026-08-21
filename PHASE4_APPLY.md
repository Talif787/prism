# Applying the Phase 4 delta (query service)

This delta adds the read path: a tenant-scoped query service over ClickHouse.
No new module dependencies are introduced.

## 1. Unzip over the repo root
From the repository root:

    unzip -o prism-phase4.zip -d .

New files: internal/query/** , cmd/query/main.go, docs/QUERY.md,
test/integration/query_test.go.
Changed files: internal/platform/clickhouse/clickhouse.go (adds a Settings
field), internal/platform/config/config_services.go (adds LoadQuery),
deployments/docker-compose.yaml, Makefile, README.md, .env.example.

## 2. Normalize the module path to your fork
The zip uses the placeholder module path. Rewrite it to yours:

    grep -rl 'github.com/prism-obs/prism' --include='*.go' . \
      | xargs -r sed -i 's#github.com/prism-obs/prism#github.com/Talif787/prism#g'
    sed -i 's#^module github.com/prism-obs/prism#module github.com/Talif787/prism#' go.mod

## 3. Tidy and build
No new dependencies, so tidy is a no-op safety step:

    go mod tidy
    gofmt -l internal/query cmd/query
    go vet ./...
    go build ./...
    go test ./internal/query/...

## 4. Integration test (optional, needs Docker)
Seeds ClickHouse through the Phase 3 writer and reads back through the store:

    go test -tags=integration -run TestQueryStore ./test/integration/...

## 5. Run it
Bring up the stack (query listens on 8092), or run locally with .env exported:

    make run-query

See docs/QUERY.md for the endpoints, auth, and cost guards.
