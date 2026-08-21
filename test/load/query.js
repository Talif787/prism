// k6 query load test: reads the metrics query API under a ramping load, with
// error-rate and latency thresholds. Requires a query-scoped API key and assumes
// some data has already been ingested (run ingest.js first, or the smoke test).
//
//   PRISM_API_KEY=<query key> PRISM_QUERY_URL=http://localhost:8092 \
//     k6 run test/load/query.js
import http from "k6/http";
import { check, sleep } from "k6";

const QUERY = __ENV.PRISM_QUERY_URL || "http://localhost:8092";
const API_KEY = __ENV.PRISM_API_KEY;

export const options = {
  scenarios: {
    query: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "30s", target: 10 },
        { duration: "1m", target: 10 },
        { duration: "30s", target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<800"],
  },
};

export default function () {
  if (!API_KEY) {
    throw new Error("set PRISM_API_KEY to a query-scoped key");
  }
  const to = new Date().toISOString();
  const from = new Date(Date.now() - 3600 * 1000).toISOString();
  const url = `${QUERY}/v1/metrics/query?name=http_requests_total&step=1m&agg=avg&from=${from}&to=${to}`;
  const res = http.get(url, { headers: { Authorization: `Bearer ${API_KEY}` } });
  check(res, { "status is 200": (r) => r.status === 200 });
  sleep(0.2);
}
