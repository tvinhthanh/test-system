import { it, expect, vi } from "vitest";
import { ApiClient, type Transport } from "../src/core/api/client";
import { MembersRepository } from "../src/features/members/membersRepository";

it("creates a project in a workspace with CSRF and reads back its id", async () => {
  const transport = vi.fn<Transport>(
    async () =>
      new Response(
        JSON.stringify({
          id: "p-1",
          name: "billing",
          workspaceName: "Payments",
        }),
        { status: 201 },
      ),
  );
  const client = new ApiClient(transport);
  client.csrfToken = "csrf-1";
  const out = await new MembersRepository(client).createProject(
    "ws 1",
    "billing",
    new AbortController().signal,
  );
  expect(out).toEqual({ id: "p-1", name: "billing" });
  const [path, init] = transport.mock.calls[0]!;
  expect(path).toBe("/v1/workspaces/ws%201/projects");
  expect(init?.method).toBe("POST");
  expect(JSON.parse(String(init?.body))).toEqual({ name: "billing" });
});
