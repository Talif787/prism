# Phase 5 apply guide (alerting service)

Unzip over your repo root, rename the module path, tidy (a no-op here, but keeps
go.sum honest), then build and test.

## 1. Unzip over the tree

    unzip -o prism-phase5.zip -d ~/prism

## 2. Rename the module path to yours

    cd ~/prism
    grep -rl 'github.com/prism-obs/prism' --include='*.go' . \
      | xargs -r sed -i 's#github.com/prism-obs/prism#github.com/Talif787/prism#g'
    sed -i 's#^module github.com/prism-obs/prism#module github.com/Talif787/prism#' go.mod

## 3. Tidy, build, test

    go mod tidy        # no new dependencies; pgx, clickhouse, redis, grpc already present
    go build ./...
    go test ./...

## 4. Apply the migration (important)

The alerting tables are migration 0003, applied by the CONTROL PLANE on startup,
not by the alerter. If the stack is already running, restart the control plane so
it picks up 0003 before the alerter serves traffic:

    COMPOSE="docker compose -f $HOME/prism/deployments/docker-compose.yaml"
    $COMPOSE up -d --build controlplane
    $COMPOSE logs --tail=20 controlplane   # look for "migrations applied ... 0003_alerting"

Then bring up the alerter:

    $COMPOSE up -d --build alerter
    curl -fsS localhost:8093/readyz && echo OK

## What is in this delta

New:
- migrations/0003_alerting.up.sql, .down.sql   (alert_rules, alert_instances)
- internal/alerting/**                          (domain, app, infra, api)
- cmd/alerter/main.go                           (service + eval loop)
- docs/ALERTING.md

Changed:
- internal/platform/config/config_services.go   (LoadAlerter)
- deployments/docker-compose.yaml               (alerter service on 8093)
- Makefile                                       (bin/alerter, run-alerter)
- README.md, .env.example

go.mod and go.sum are unchanged.
