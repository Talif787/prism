# Phase 6 apply guide (metering and billing)

This overlays the metering service onto your tree. It adds no new dependencies, so
`go mod tidy` and `go.sum` should not change.

## 1. Unzip over the repo root

```bash
cd ~/prism
unzip -o /path/to/prism-phase6.zip -d .
```

New files: `migrations/0004_metering.{up,down}.sql`, `internal/metering/**`,
`cmd/metering/main.go`, `docs/METERING.md`. Changed files:
`internal/platform/config/config_services.go`, `deployments/docker-compose.yaml`,
`Makefile`, `README.md`, `.env.example`.

## 2. Rewrite the module path to yours

The zip uses `github.com/prism-obs/prism`; rewrite it to your module path.

```bash
grep -rl 'github.com/prism-obs/prism' --include='*.go' . \
  | xargs -r sed -i 's#github.com/prism-obs/prism#github.com/Talif787/prism#g'
sed -i 's#^module github.com/prism-obs/prism#module github.com/Talif787/prism#' go.mod
```

## 3. Tidy, build, test

```bash
go mod tidy   # expect no change to go.mod or go.sum
gofmt -l internal/metering cmd/metering internal/platform/config
go vet ./...
go build ./...
go test ./internal/metering/...
```

## 4. Bring the stack up

The metering tables are created by the control plane migrator (migration `0004`),
so the control plane must restart to pick them up. A rebuild does that.

```bash
COMPOSE="docker compose -f $HOME/prism/deployments/docker-compose.yaml"
$COMPOSE up -d --build
for p in 8080 8090 8092 8093 8094; do until curl -fsS localhost:$p/readyz >/dev/null; do sleep 2; done; echo "port $p ready"; done
$COMPOSE logs controlplane | grep -iE "migration|0004" | tail
```

The compose stack sets `METERING_ROLLUP_INTERVAL=1m` and `METERING_ROLLUP_TICK=15s`
so usage appears within a minute. Production defaults (in `.env.example`) are 1h and
5m. See `docs/METERING.md` for the API and design.
