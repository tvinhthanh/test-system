import { expect, it } from "vitest";
import { ApiClient, ApiError, type Transport } from "../src/core/api/client";
import { AutotestRepository } from "../src/features/autotest/autotestRepository";
import { explainLoadError } from "../src/features/autotest/loadErrors";
import { buildLoadInput } from "../src/features/autotest/loadForm";

const signal = () => new AbortController().signal;
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });
const repository = (transport: Transport) => {
  const client = new ApiClient(transport);
  client.csrfToken = "csrf";
  return new AutotestRepository(client);
};

it("builds a bounded load request and validates paths and thresholds", () => {
  const valid = buildLoadInput({
    environmentId: "env-1",
    paths: "/\n/checkout",
    vus: "20",
    seconds: "60",
    rampSeconds: "5",
    maxP95Ms: "800",
    maxErrorRate: "1.5",
  });
  expect(valid).toEqual({
    input: {
      environmentId: "env-1",
      paths: ["/", "/checkout"],
      vus: 20,
      seconds: 60,
      rampSeconds: 5,
      maxP95Ms: 800,
      maxErrorRate: 1.5,
    },
    error: "",
  });
  expect(
    buildLoadInput({
      environmentId: "env-1",
      paths: "https://example.com",
      vus: "5",
      seconds: "30",
      rampSeconds: "0",
      maxP95Ms: "",
      maxErrorRate: "",
    }).error,
  ).toMatch(/starting with a single/);
  expect(
    buildLoadInput({
      environmentId: "env-1",
      paths: "/",
      vus: "5",
      seconds: "30",
      rampSeconds: "30",
      maxP95Ms: "",
      maxErrorRate: "",
    }).error,
  ).toMatch(/less than the duration/);
});

it("uses stable load-test error codes and preserves server cap messages", () => {
  const ownership = explainLoadError(
    new ApiError(409, "OWNERSHIP_NOT_PROVED", "conflict", "req-1", "raw"),
  ) as ApiError;
  expect(ownership.message).toMatch(/Prove ownership/);
  const limited = explainLoadError(
    new ApiError(429, "TOO_MANY_LOAD_RUNS", "limited"),
  ) as ApiError;
  expect(limited.message).toMatch(/one load test/i);
  const cap = explainLoadError(
    new ApiError(
      400,
      "INVALID_PAYLOAD",
      "bad request",
      "req-2",
      "vus: maximum 20",
    ),
  ) as ApiError;
  expect(cap.message).toBe("vus: maximum 20");
});

it("sends ownership checks and load runs through the repository", async () => {
  const calls: { path: string; body?: unknown }[] = [];
  const repo = repository(async (path, init) => {
    calls.push({
      path: String(path),
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
    });
    if (String(path).endsWith("/ownership/check"))
      return json(
        {
          host: "stage.example.test",
          token: "proof",
          method: "dns",
          status: "checking",
          error: "",
          verifiedAt: null,
          checkedAt: null,
          fileUrl:
            "https://stage.example.test/.well-known/atp-verification.txt",
          fileText: "atp-verification=proof",
          dnsName: "_atp-verification.stage.example.test",
          dnsText: "atp-verification=proof",
        },
        202,
      );
    return json(
      {
        id: "load-1",
        environmentId: "env-1",
        host: "stage.example.test",
        paths: ["/"],
        vus: 5,
        seconds: 30,
        rampSeconds: 5,
        maxP95Ms: 800,
        maxErrorRate: 1,
        status: "queued",
        verdict: "",
        reasons: [],
        result: null,
        error: "",
        requestedBy: "tester@example.test",
        createdAt: "2026-11-05T00:00:00Z",
        startedAt: null,
        finishedAt: null,
      },
      202,
    );
  });
  await repo.checkOwnership("project", "env-1", "dns", signal());
  await repo.createLoadRun(
    "project",
    {
      environmentId: "env-1",
      paths: ["/"],
      vus: 5,
      seconds: 30,
      rampSeconds: 5,
      maxP95Ms: 800,
      maxErrorRate: 1,
    },
    signal(),
  );
  expect(calls).toEqual([
    {
      path: "/v1/projects/project/environments/env-1/ownership/check",
      body: { method: "dns" },
    },
    {
      path: "/v1/projects/project/load-runs",
      body: {
        environmentId: "env-1",
        paths: ["/"],
        vus: 5,
        seconds: 30,
        rampSeconds: 5,
        maxP95Ms: 800,
        maxErrorRate: 1,
      },
    },
  ]);
});
