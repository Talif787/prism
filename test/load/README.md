# Load testing (k6)

Two [k6](https://k6.io) scripts exercise the hot paths: `ingest.js` posts OTLP/HTTP
metrics to the gateway, and `query.js` reads the metrics query API. Both ramp virtual
users and enforce error-rate and latency thresholds, so a run either passes or fails.

## Get an API key

Create a tenant and keys the same way the smoke test does (see `docs/DEPLOYMENT.md`),
then port-forward the services under test. You need an ingest-scoped key for
`ingest.js` and a query-scoped key for `query.js`.

## Run

```bash
# ingest
PRISM_API_KEY=<ingest-key> PRISM_GATEWAY_URL=http://localhost:8090 \
  k6 run test/load/ingest.js

# query (run some ingest first so there is data to read)
PRISM_API_KEY=<query-key> PRISM_QUERY_URL=http://localhost:8092 \
  k6 run test/load/query.js
```

Environment variables: `PRISM_API_KEY` (required), `PRISM_GATEWAY_URL` and
`PRISM_QUERY_URL` (default to localhost), and `PRISM_BATCH` for the ingest points per
request (default 50).

## Thresholds

Ingest expects under 1 percent failed requests and a p95 under 500ms; query expects
under 1 percent failed and a p95 under 800ms. k6 exits non-zero if a threshold is
breached, which makes these safe to wire into CI against an ephemeral stack later.
Tune the `stages` and thresholds to your target load; the defaults are a modest
laptop-cluster baseline, not a capacity benchmark.
