import { it, expect, vi } from "vitest";
import { ApiClient, ApiError, type Transport } from "../src/core/api/client";
import { SessionRepository } from "../src/features/auth/sessionRepository";

const api = (transport: Transport) => {
  const client = new ApiClient(transport);
  client.csrfToken = "csrf-1";
  return new SessionRepository(client);
};

it("changes the password with the current one, as JSON with CSRF", async () => {
  const transport = vi.fn<Transport>(
    async () => new Response(null, { status: 204 }),
  );
  await api(transport).changePassword("old passphrase", "new passphrase!");
  const [path, init] = transport.mock.calls[0]!;
  expect(path).toBe("/v1/session/password");
  expect(init?.method).toBe("POST");
  expect((init?.headers as Record<string, string>)["X-CSRF-Token"]).toBe(
    "csrf-1",
  );
  expect(JSON.parse(String(init?.body))).toEqual({
    current: "old passphrase",
    password: "new passphrase!",
  });
});

it("surfaces WRONG_PASSWORD as its own code, not a sign-out", async () => {
  const transport = vi.fn<Transport>(
    async () =>
      new Response(
        JSON.stringify({ error: { code: "WRONG_PASSWORD", message: "x" } }),
        { status: 400 },
      ),
  );
  const client = new ApiClient(transport);
  client.csrfToken = "csrf-1";
  const unauthorized = vi.fn();
  client.onUnauthorized = unauthorized;
  const err = await new SessionRepository(client)
    .changePassword("wrong", "new passphrase!")
    .catch((e: unknown) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect((err as ApiError).code).toBe("WRONG_PASSWORD");
  expect(unauthorized).not.toHaveBeenCalled();
});
