# Chaos testing (Chaos Mesh)

Each experiment states a hypothesis tied to a resilience property of the system and
is small enough to reason about. They target the in-cluster components by their
`app.kubernetes.io/component` label.

## Install Chaos Mesh

```bash
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm install chaos-mesh chaos-mesh/chaos-mesh -n chaos-mesh --create-namespace
```

## Experiments

| File                            | Fault                          | Hypothesis                                                                 |
|---------------------------------|--------------------------------|---------------------------------------------------------------------------|
| `pod-kill-consumer.yaml`        | kill a consumer pod            | no data loss; offsets and ReplacingMergeTree dedup absorb the redelivery   |
| `network-delay-clickhouse.yaml` | 200ms latency to ClickHouse    | graceful degradation; batching absorbs it, no crashes                      |
| `pod-failure-gateway.yaml`      | one gateway pod unavailable    | ingest stays available behind multiple replicas (run with replicas >= 2)  |

## Run and observe

Apply an experiment, then watch the relevant signal while it runs. Drive load with
the k6 ingest script in parallel so the effect is visible.

```bash
kubectl apply -f test/chaos/pod-kill-consumer.yaml
kubectl -n prism get pods -w
# confirm recovery, then check that query totals still match what was ingested
```

The pod-kill experiment is one-shot; re-apply to repeat, or wrap it in a Chaos Mesh
`Schedule` for recurring kills. The delay and pod-failure experiments carry a
`duration` and self-revert. Remove any lingering experiment with
`kubectl delete -f <file>`.

The gateway experiment is most instructive as a contrast: against the production
values (two or more gateway replicas) ingest should stay flat, while against the dev
overlay (a single replica) you will see a brief outage, which is exactly why the
scalable services carry more than one replica and a disruption budget.
