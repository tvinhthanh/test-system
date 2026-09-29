import { it, expect, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { ReportsRepository } from "../src/features/reports/reportsRepository";

const deleted = {
  id: "r-1",
  branch: "main",
  commitSha: "abc",
  status: "failed",
  startedAt: "2026-09-29T08:00:00Z",
  deletedAt: "2026-09-29T09:00:00Z",
  deletedBy: "mia@acme.example",
  purgeAfter: "2026-10-29T09:00:00Z",
};
const repo = (transport: Transport) => {
  const client = new ApiClient(transport);
  client.csrfToken = "csrf-1";
  return new ReportsRepository(client);
};
const signal = () => new AbortController().signal;

it("deletes a run with DELETE and reads the restore window back", async () => {
  const transport = vi.fn<Transport>(
    async () => new Response(JSON.stringify(deleted)),
  );
  const out = await repo(transport).deleteRun("p", "r-1", signal());
  expect(out.purgeAfter).toBe(deleted.purgeAfter);
  const [path, init] = transport.mock.calls[0]!;
  expect(path).toBe("/v1/projects/p/runs/r-1");
  expect(init?.method).toBe("DELETE");
});

it("lists deleted runs and restores one", async () => {
  const transport = vi.fn<Transport>(async (path) =>
    String(path).endsWith("/deleted")
      ? new Response(JSON.stringify({ items: [deleted], nextCursor: null }))
      : new Response(JSON.stringify({ id: "r-1" })),
  );
  const r = repo(transport);
  expect((await r.deletedRuns("p", signal()))[0]?.deletedBy).toBe(
    "mia@acme.example",
  );
  await r.restoreRun("p", "r-1", signal());
  const [path, init] = transport.mock.calls[1]!;
  expect(path).toBe("/v1/projects/p/runs/r-1/restore");
  expect(init?.method).toBe("POST");
});
