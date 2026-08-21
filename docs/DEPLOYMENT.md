# Deployment (Phase 7a: Kubernetes and Helm)

Prism ships as a single Helm chart at `deploy/helm/prism` that deploys all seven
backend services. This document covers building images, installing to a local
cluster with in-cluster infrastructure, and installing to a real cluster against
managed data services. Provisioning (Terraform) and the delivery pipeline (CI/CD),
along with load and chaos testing, arrive in Phase 7b.

## What the chart renders

For each of the seven services (control plane, gateway, relay, consumer, query,
alerter, metering) the chart renders a Deployment, and for services with a port a
Service, an optional HorizontalPodAutoscaler, and an optional PodDisruptionBudget.
Shared, non-secret configuration (infra endpoints, OTel, ports) is a single
ConfigMap; secret material (database URL, internal token, dev signing secret,
ClickHouse password) is a single Secret. Every pod runs as non-root with a
read-only root filesystem, all Linux capabilities dropped, and a writable `/tmp`
emptyDir.

Startup is health-gated rather than ordered by luck. The control plane applies the
Postgres migrations on startup, and the consumer applies the ClickHouse schema on
startup. Services that depend on those schemas carry an init container that blocks
on the dependency's `/readyz` before the main container starts: the gateway waits
for the control plane; query, alerter, and metering wait for both the control plane
and the consumer; relay waits for the control plane. Infrastructure readiness itself
is handled by each service's own connection retry and Kubernetes restart backoff.

relay, alerter, and metering run a single replica by design. Each runs a background
loop (outbox publish, rule evaluation, usage rollup); running several concurrently
would duplicate work and, for the alerter, duplicate alert webhooks. Scaling them
horizontally needs leader election, which is deferred.

## Prerequisites

- A Kubernetes cluster (kind, minikube, or a managed cluster) and `kubectl`.
- Helm 3.
- The service images built and reachable by the cluster.

## Build the images

All services build from one multi-stage Dockerfile parameterized by a `CMD`
build argument that selects the binary. Build one image per service, tagged
`prism-<service>`:

```bash
cd ~/prism
for s in controlplane gateway relay consumer query alerter metering; do
  docker build -f build/Dockerfile --build-arg CMD=$s -t prism-$s:dev .
done
```

For a local kind cluster, load them so the nodes can see them without a registry:

```bash
for s in controlplane gateway relay consumer query alerter metering; do
  kind load docker-image prism-$s:dev
done
```

For minikube, either `minikube image load prism-$s:dev` per image, or build inside
the minikube Docker environment (`eval $(minikube docker-env)` before the build
loop).

## Install to a local cluster (in-cluster infrastructure)

The dev overlay enables in-cluster Postgres, ClickHouse, Redis, Redpanda, and
Jaeger (all ephemeral), uses the locally built `prism-<service>:dev` images, runs
single replicas, and sets a one-minute metering rollup so usage is visible quickly.

```bash
kubectl create namespace prism
helm upgrade --install prism deploy/helm/prism \
  -n prism \
  -f deploy/helm/prism/values-dev.yaml
```

Watch it come up. On first install, give it a minute while migrations apply and the
init containers release:

```bash
kubectl -n prism get pods -w
```

## Smoke test

Port-forward the control plane and gateway, then run the same tenant, key, and
ingest flow used in the service test runbooks:

```bash
kubectl -n prism port-forward svc/prism-controlplane 8080:8080 &
kubectl -n prism port-forward svc/prism-gateway 8090:8090 &
kubectl -n prism port-forward svc/prism-query 8092:8092 &
```

Create a tenant and an ingest key against `localhost:8080`, post OTLP metrics to
`localhost:8090/v1/metrics`, and read them back from `localhost:8092`. The service
docs (`docs/INGEST.md`, `docs/QUERY.md`, `docs/METERING.md`) carry the exact
payloads.

Check that all deployments are available:

```bash
kubectl -n prism get deploy
kubectl -n prism rollout status deploy/prism-gateway
```

## Install to a real cluster (managed data services)

For anything beyond a demo, leave `devInfra` disabled (the default) and point the
chart at managed data services. Provide endpoints and secrets through your own
values file or `--set`, ideally sourcing secrets from a secret manager rather than
committing them.

```bash
helm upgrade --install prism deploy/helm/prism \
  -n prism --create-namespace \
  --set image.registry=ghcr.io/talif787 \
  --set image.tag=0.7.0 \
  --set endpoints.redis=your-memorystore-host:6379 \
  --set endpoints.clickhouse=your-clickhouse-host:9000 \
  --set endpoints.kafka=your-broker:9092 \
  --set endpoints.otel=your-collector:4317 \
  --set secrets.databaseUrl='postgres://user:pass@your-cloudsql-host:5432/prism?sslmode=require' \
  --set secrets.internalApiToken=$INTERNAL_TOKEN \
  --set secrets.authDevSecret=$DEV_SIGNING_SECRET \
  --set clickhouse.password=$CLICKHOUSE_PASSWORD
```

The control plane still applies Postgres migrations on startup against the managed
database, and the consumer still applies the ClickHouse schema, so no separate
migration step is required. Confirm the control plane can reach the database before
the rest, since everything gates on its readiness.

Production notes worth heeding: replace the dev auth mode and dev signing secret
with real OIDC and a managed secret; set `kafka.autoCreateTopics=false` and
pre-create topics; and size the autoscalers and resource requests to your traffic
(the defaults are deliberately small).

## Rendering raw manifests

If you prefer to inspect or apply plain manifests, render them without installing:

```bash
helm template prism deploy/helm/prism -f deploy/helm/prism/values-dev.yaml > /tmp/prism.yaml
```

Lint and dry-run before any real install:

```bash
helm lint deploy/helm/prism -f deploy/helm/prism/values-dev.yaml
helm install prism deploy/helm/prism -f deploy/helm/prism/values-dev.yaml --dry-run
```

## Scaling and disruption

The gateway, consumer, and query carry HPAs on CPU (disabled in the dev overlay).
The consumer scales within the bound of the Kafka partition count. The scalable
services carry PodDisruptionBudgets so a node drain cannot take the last replica.
The three singleton loops carry neither.

## Teardown

```bash
helm uninstall prism -n prism
kubectl delete namespace prism
```

## Provisioning with Terraform

The chart assumes a cluster and data services already exist. `deploy/terraform`
provisions them on GCP: a VPC-native GKE cluster with a managed, autoscaling node
pool, and optionally Cloud SQL Postgres and Memorystore Redis on private IPs, with
outputs (a `database_url`, a `redis_addr`, and a kubeconfig command) that wire
straight into the chart's secrets and endpoints. ClickHouse, Redpanda, and Jaeger
stay in-cluster. See `deploy/terraform/README.md` for the apply, wiring, and teardown
steps, and the cost warning.

## Continuous integration and delivery

`.github/workflows/ci.yml` runs on every pull request and push to main: Go build,
test, `gofmt`, and `go vet`; `helm lint` and a full template render; `terraform fmt`,
`init`, and `validate`; a Docker build matrix over all seven services; and a
conventions job that fails the build if any forbidden dash character is present, so
the repository's no-dash rule is enforced automatically. `.github/workflows/release.yml`
runs on a `v*` tag and builds and pushes each service image to GHCR
(`ghcr.io/<owner>/prism-<service>`), tagged with the semantic version and `latest`.

## Load and chaos testing

Operational test suites live under `test/`. `test/load` drives the ingest and query
paths with k6 under latency and error-rate thresholds; `test/chaos` injects faults
with Chaos Mesh to validate specific resilience claims (consumer pod kill, ClickHouse
latency, gateway pod failure). See `docs/TESTING.md`.

## Further hardening

A default-deny network policy for east-west traffic, a private GKE control plane with
Cloud NAT for egress, leader election so the singleton loops (relay, alerter,
metering) can scale, and sourcing secrets from Secret Manager rather than values are
all natural next steps beyond this backend.
