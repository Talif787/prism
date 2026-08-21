// k6 ingest load test: posts OTLP/HTTP metrics to the gateway under a ramping load,
// with error-rate and latency thresholds. Requires an ingest-scoped API key.
//
//   PRISM_API_KEY=<ingest key> PRISM_GATEWAY_URL=http://localhost:8090 \
//     k6 run test/load/ingest.js
//
// See test/load/README.md for creating a key and reading the results.
import http from "k6/http";
import { check, sleep } from "k6";
import { Counter } from "k6/metrics";

const GATEWAY = __ENV.PRISM_GATEWAY_URL || "http://localhost:8090";
const API_KEY = __ENV.PRISM_API_KEY;
const BATCH = Number(__ENV.PRISM_BATCH || "50");

export const options = {
  scenarios: {
    ingest: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "30s", target: 20 },
        { duration: "1m", target: 20 },
        { duration: "30s", target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<500"],
  },
};

const accepted = new Counter("points_accepted");

function metricsPayload(n) {
  const now = Date.now() * 1e6;
  const pts = [];
  for (let i = 0; i < n; i++) {
    pts.push({
      asDouble: Math.random() * 100,
      timeUnixNano: String(now - i * 1000000),
      attributes: [{ key: "route", value: { stringValue: "/checkout" } }],
    });
  }
  return JSON.stringify({
    resourceMetrics: [
      {
        resource: { attributes: [{ key: "service.name", value: { stringValue: "loadtest" } }] },
        scopeMetrics: [
          { scope: { name: "k6" }, metrics: [{ name: "http_requests_total", gauge: { dataPoints: pts } }] },
        ],
      },
    ],
  });
}

export default function () {
  if (!API_KEY) {
    throw new Error("set PRISM_API_KEY to an ingest-scoped key");
  }
  const res = http.post(`${GATEWAY}/v1/metrics`, metricsPayload(BATCH), {
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${API_KEY}` },
  });
  const ok = check(res, { "status is 2xx": (r) => r.status >= 200 && r.status < 300 });
  if (ok) {
    accepted.add(BATCH);
  }
  sleep(0.1);
}
