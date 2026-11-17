// A k6 script as sandboxd receives it (sandbox/test/e2e.sh). The run's vus
// and duration replace these options' own; the thresholds stay.
import http from "k6/http";
import { check } from "k6";

export const options = {
  vus: 500, // replaced by the run's --vus (the cap holds)
  duration: "10m",
  thresholds: { http_req_failed: ["rate<0.01"], http_req_duration: ["p(95)<2000"] },
};

export default function () {
  const res = http.get(__ENV.BASE_URL + "/");
  check(res, { "status is 200": (r) => r.status === 200, "says hello": (r) => r.body.includes("hello sandbox") });
}
