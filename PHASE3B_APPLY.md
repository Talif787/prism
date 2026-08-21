# Applying the Phase 3b delta (OTLP/gRPC receiver)

This delta adds an OTLP/gRPC receiver to the gateway on port 4317, sharing the
same ingest pipeline as OTLP/HTTP.

## 1. Unzip over the repo root
    unzip -o prism-phase3b.zip -d .

New: internal/ingest/api/otlpgrpc/ (server.go, server_test.go).
Changed: cmd/gateway/main.go, internal/platform/config/config.go,
internal/platform/config/config_services.go, deployments/docker-compose.yaml,
README.md, .env.example, docs/INGEST.md.

## 2. Normalize the module path to your fork
    grep -rl 'github.com/prism-obs/prism' --include='*.go' . \
      | xargs -r sed -i 's#github.com/prism-obs/prism#github.com/Talif787/prism#g'
    sed -i 's#^module github.com/prism-obs/prism#module github.com/Talif787/prism#' go.mod

## 3. Tidy (REQUIRED this phase)
Unlike Phase 4, this phase adds a direct dependency. google.golang.org/grpc was
already an indirect dependency (pulled in by the OTLP proto module); importing it
directly promotes it. This needs network:

    go mod tidy
    gofmt -l internal/ingest/api/otlpgrpc cmd/gateway internal/platform/config
    go vet ./...
    go build ./...
    go test ./internal/ingest/...

## 4. Run it
Bring up the stack (the gateway now also listens on 4317), or run locally:

    make run-gateway    # if that target exists, else: go run ./cmd/gateway

Note the compose change: the gateway now publishes host port 4317 for OTLP/gRPC,
and Jaeger's OTLP host publish moved from 4317 to 4327 to avoid the clash. Jaeger
still receives internal traces at jaeger:4317 over the compose network.

See docs/INGEST.md for the gRPC section, including how to point an OpenTelemetry
SDK or Collector at the gateway.
