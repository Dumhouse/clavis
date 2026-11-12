import http from 'k6/http'
import { check } from 'k6'

// Usage:
//   BASE_URL=https://clavis.example.com TOKEN=clv_mt_... \
//   ORG=acme PROJECT=web-app ENV=dev k6 run loadtest/reads.js
export const options = {
  scenarios: {
    reads: {
      executor: 'constant-arrival-rate',
      rate: Number(__ENV.RATE || 1000),
      timeUnit: '1s',
      duration: __ENV.DURATION || '30s',
      preAllocatedVUs: Number(__ENV.VUS || 100),
      maxVUs: Number(__ENV.MAX_VUS || 400),
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<200'],
  },
}

const base = __ENV.BASE_URL
const headers = { Authorization: `Bearer ${__ENV.TOKEN}` }
const path = `/api/v1/orgs/${__ENV.ORG}/projects/${__ENV.PROJECT}/environments/${__ENV.ENV}/secrets`

export default function () {
  const response = http.get(`${base}${path}`, { headers })
  check(response, { 'status is 200': (r) => r.status === 200 })
}
