# syntax=docker/dockerfile:1

# Multi-stage build parameterized by CMD so the same Dockerfile builds every
# service binary (controlplane, gateway, relay). The final image is distroless,
# static, and runs as nonroot.
FROM golang:1.26-alpine AS build
WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

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
