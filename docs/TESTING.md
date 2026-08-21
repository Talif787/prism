# Testing: load and chaos (Phase 7b)

Beyond the unit and integration tests in the Go packages, Prism carries two
operational test suites that run against a deployed stack.

Load testing lives in `test/load` and uses k6 to drive the ingest and query paths
under a ramping load with error-rate and latency thresholds, so a run is pass or
fail rather than a wall of numbers. See `test/load/README.md`.

Chaos testing lives in `test/chaos` and uses Chaos Mesh to inject faults that probe
specific resilience claims: a consumer pod kill (no data loss, thanks to Kafka
offsets and ReplacingMergeTree dedup), added ClickHouse latency (graceful
degradation via write batching and the query execution guard), and a gateway pod
failure (ingest stays available behind multiple replicas). See
`test/chaos/README.md`.

Both suites assume a running cluster (local kind or a provisioned GKE) and an API
key, created the same way as in the deployment smoke test. Neither is wired into CI
by default, since both need a live stack; the k6 thresholds are written so they
could gate a pipeline against an ephemeral environment in future.
