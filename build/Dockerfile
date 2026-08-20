# syntax=docker/dockerfile:1

# Multi-stage build: a cached module layer, a static binary, then a distroless
# runtime. The final image contains no shell and runs as nonroot by default.
FROM golang:1.26-alpine AS build
WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/controlplane ./cmd/controlplane

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /
COPY --from=build /out/controlplane /controlplane
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/controlplane"]
