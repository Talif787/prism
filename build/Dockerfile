# syntax=docker/dockerfile:1

# Multi-stage build parameterized by CMD so the same Dockerfile builds every
# service binary (controlplane, gateway, relay, consumer, query). The final image
# is distroless, static, and runs as nonroot.
FROM golang:1.26-alpine AS build
WORKDIR /src

RUN apk add --no-cache git ca-certificates

# Module downloads go through the proxy but fall through to a direct VCS fetch on
# any error (the pipe, not a comma, falls through on connection failures too, not
# just 404/410). The retry loop rides out transient proxy or network blips rather
# than failing the whole build on a single refused connection.
ENV GOPROXY=https://proxy.golang.org|direct

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    for i in 1 2 3 4 5; do \
      go mod download && break; \
      echo "go mod download attempt $i failed; retrying in $((i * 3))s" >&2; \
      sleep $((i * 3)); \
      if [ "$i" = "5" ]; then echo "go mod download failed after 5 attempts" >&2; exit 1; fi; \
    done

COPY . .

ARG VERSION=dev
ARG CMD=controlplane
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/service ./cmd/${CMD}

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /
COPY --from=build /out/service /service
USER nonroot:nonroot
ENTRYPOINT ["/service"]
