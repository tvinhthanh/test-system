import { describe, it, expect, vi } from "vitest";
import { ApiClient, ApiError } from "../src/core/api/client";

describe("API boundary", () => {
  it("sends cookies and CSRF for writes without exposing tokens in the URL", async () => {
    const transport = vi.fn(async () => new Response("{}"));
    const api = new ApiClient(transport);
    api.csrfToken = "csrf";
    await api.request("/v1/session", {
      body: { email: "a@example.test", password: "x" },
    });
    expect(transport.mock.calls[0]).toMatchObject([
      "/v1/session",
      {
        credentials: "include",
        method: "POST",
        headers: { "X-CSRF-Token": "csrf" },
      },
    ]);
  });
  it("refuses writes without CSRF and never sends the request", async () => {
    const transport = vi.fn();
    const api = new ApiClient(transport);
    await expect(
      api.request("/v1/session", { body: {} }),
    ).rejects.toMatchObject({ code: "CSRF_REQUIRED" });
    expect(transport).not.toHaveBeenCalled();
  });
  it("preserves 401 after invalidating pending work and hides server internals", async () => {
    const api = new ApiClient(
      async () =>
        new Response(
          JSON.stringify({
            error: {
              code: "UNAUTHENTICATED",
              message: "SELECT secret FROM users",
            },
          }),
          { status: 401, headers: { "X-Request-Id": "req-1" } },
        ),
    );
    api.onUnauthorized = () => api.clearSession();
    await expect(api.request("/v1/projects")).rejects.toMatchObject({
      status: 401,
      code: "UNAUTHENTICATED",
      requestId: "req-1",
    });
  });
  it("rejects successful non-JSON responses", async () => {
    const api = new ApiClient(
      async () => new Response("<html>proxy page</html>"),
    );
    await expect(api.request("/v1/projects")).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });
  it("aborts timeouts and reports a retryable timeout", async () => {
    vi.useFakeTimers();
    const api = new ApiClient(
      (_url, init) =>
        new Promise((_resolve, reject) =>
          init?.signal?.addEventListener("abort", () =>
            reject(new DOMException("Aborted", "AbortError")),
          ),
        ),
      20,
    );
    const request = api.request("/v1/projects").catch((e) => e);
    await vi.advanceTimersByTimeAsync(25);
    expect(await request).toMatchObject({ code: "TIMEOUT" });
    vi.useRealTimers();
  });
  it("discards old-session responses even if transport ignores abort", async () => {
    let resolve!: (response: Response) => void;
    const api = new ApiClient(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    );
    const request = api.request("/v1/projects");
    api.clearSession();
    resolve(new Response("{}"));
    await expect(request).rejects.toMatchObject({ name: "AbortError" });
  });
  it("does not leak CSRF to absolute endpoints", async () => {
    const api = new ApiClient(vi.fn());
    await expect(
      api.request("https://example.com/v1/session"),
    ).rejects.not.toBeInstanceOf(ApiError);
  });
});
