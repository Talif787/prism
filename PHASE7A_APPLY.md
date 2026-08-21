# Phase 7a apply guide (Kubernetes and Helm)

Adds the Helm chart at `deploy/helm/prism` and `docs/DEPLOYMENT.md`. No Go code, so
no build, no `go mod tidy`, no module-path rename.

## 1. Unzip over the repo root
```bash
cd ~/prism
unzip -o /path/to/prism-phase7a.zip -d .
```
New: `deploy/helm/prism/**`, `docs/DEPLOYMENT.md`. Changed: `README.md`.

## 2. Validate (the real gate)
```bash
helm lint deploy/helm/prism -f deploy/helm/prism/values-dev.yaml
helm template prism deploy/helm/prism -f deploy/helm/prism/values-dev.yaml | head -60
```

## 3. Build images, load, install
```bash
for s in controlplane gateway relay consumer query alerter metering; do docker build -f build/Dockerfile --build-arg CMD=$s -t prism-$s:dev .; done
# kind: kind load docker-image prism-$s:dev --name <cluster>   (per image)
kubectl create namespace prism
helm upgrade --install prism deploy/helm/prism -n prism -f deploy/helm/prism/values-dev.yaml
kubectl -n prism get pods -w
```

## Note on the devInfra redpanda
The redpanda container uses `args:` (not `command:`) so the image entrypoint runs and
interprets `--mode=dev-container` via `rpk redpanda start`. Setting `command:` in
Kubernetes overrides the entrypoint and passes the flags to the raw broker binary,
which rejects them (compose works because its `command` is passed to the intact
entrypoint). This was found during live cluster testing and is baked into the chart.
