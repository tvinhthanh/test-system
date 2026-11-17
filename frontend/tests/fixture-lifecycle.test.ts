import { expect, it } from "vitest";
import { createFixtureTransport } from "../src/dev/fixtureTransport";

const json = async (response: Response) => response.json() as Promise<any>;

it("keeps lifecycle and token mutations visible in fixture mode", async () => {
  sessionStorage.removeItem("atp-fixture-session");
  const transport = createFixtureTransport();
  const call = (path: string, init?: RequestInit) => transport(path, init);

  const projects = await json(await call("/v1/projects"));
  expect(projects.items[0].actions).toEqual(
    expect.arrayContaining(["token.manage", "project.manage", "data.delete"]),
  );

  const issued = await json(
    await call("/v1/projects/commerce/tokens", {
      method: "POST",
      body: JSON.stringify({
        name: "preview pipeline",
        permissions: ["project:ingest", "project:read"],
      }),
    }),
  );
  expect(issued.token.name).toBe("preview pipeline");
  let tokens = await json(await call("/v1/projects/commerce/tokens"));
  expect(tokens.items[0].id).toBe(issued.token.id);
  await call(`/v1/projects/commerce/tokens/${issued.token.id}`, {
    method: "DELETE",
  });
  tokens = await json(await call("/v1/projects/commerce/tokens"));
  expect(tokens.items[0].revokedAt).toBeTruthy();

  const deletedRuns = await json(
    await call("/v1/projects/commerce/runs/deleted"),
  );
  expect(deletedRuns.items[0].id).toBe("deleted-run-preview");
  await call("/v1/projects/commerce/runs/deleted-run-preview/restore", {
    method: "POST",
    body: "{}",
  });
  expect(
    (await json(await call("/v1/projects/commerce/runs/deleted"))).items,
  ).toEqual([]);

  const deletedProjects = await json(
    await call("/v1/workspaces/web/projects/deleted"),
  );
  expect(deletedProjects.items[0].id).toBe("retired-storefront");
  const restored = await json(
    await call("/v1/workspaces/web/projects/retired-storefront/restore", {
      method: "POST",
      body: "{}",
    }),
  );
  expect(restored.name).toBe("Retired Storefront");
});
