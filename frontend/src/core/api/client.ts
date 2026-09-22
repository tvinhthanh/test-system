export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly requestId?: string,
  ) {
    super(message);
  }
}

export type Transport = (
  input: RequestInfo | URL,
  init?: RequestInit,
) => Promise<Response>;
// method defaults to POST when a body is given, GET otherwise; PATCH and
// DELETE (Test Management) are state-changing and carry the CSRF token too.
// A FormData body goes as multipart/form-data (the browser sets the
// boundary); anything else is JSON. timeoutMs overrides the client default
// for a request that moves a file rather than a few kilobytes.
export type RequestOptions = {
  signal?: AbortSignal;
  body?: unknown;
  method?: "GET" | "POST" | "PATCH" | "DELETE";
  timeoutMs?: number;
};

const statusMessage = (status: number) =>
  ({
    401: "Your session has expired. Sign in again.",
    403: "You do not have permission to access this resource.",
    404: "This resource is unavailable or no longer exists.",
    409: "This resource changed. Refresh and try again.",
    413: "This file is larger than the server accepts.",
    429: "Too many requests. Wait a moment and try again.",
  })[status] ?? "The request could not be completed. Please try again.";

export class ApiClient {
  csrfToken: string | null = null;
  onUnauthorized: (() => void) | undefined;
  private epoch = 0;
  private pending = new Set<AbortController>();
  constructor(
    private transport: Transport,
    private timeoutMs = 15000,
  ) {}

  clearSession() {
    this.epoch++;
    this.csrfToken = null;
    for (const controller of this.pending) controller.abort();
    this.pending.clear();
  }

  async request(path: string, options: RequestOptions = {}): Promise<unknown> {
    if (!path.startsWith("/v1/") || path.includes("://"))
      throw new Error("Only same-origin /v1/ API paths are allowed.");
    const method =
      options.method ?? (options.body !== undefined ? "POST" : "GET");
    const mutation = method !== "GET";
    if (mutation && !this.csrfToken)
      throw new ApiError(
        0,
        "CSRF_REQUIRED",
        "Refresh your session before submitting.",
      );
    const controller = new AbortController();
    const epoch = this.epoch;
    const abort = () => controller.abort();
    options.signal?.addEventListener("abort", abort, { once: true });
    if (options.signal?.aborted) controller.abort();
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, options.timeoutMs ?? this.timeoutMs);
    this.pending.add(controller);
    const multipart =
      typeof FormData !== "undefined" && options.body instanceof FormData;
    try {
      const response = await this.transport(path, {
        method,
        credentials: "include",
        signal: controller.signal,
        headers: {
          Accept: "application/json",
          ...(mutation ? { "X-CSRF-Token": this.csrfToken! } : {}),
          ...(options.body !== undefined && !multipart
            ? { "Content-Type": "application/json" }
            : {}),
        },
        ...(options.body !== undefined
          ? {
              body: multipart
                ? (options.body as FormData)
                : JSON.stringify(options.body),
            }
          : {}),
      });
      if (epoch !== this.epoch || controller.signal.aborted)
        throw new DOMException("Aborted", "AbortError");
      const text = await response.text();
      let json: unknown;
      try {
        json = text ? JSON.parse(text) : {};
      } catch {
        if (response.ok)
          throw new ApiError(
            0,
            "INVALID_RESPONSE",
            "The server returned an invalid response.",
          );
      }
      if (!response.ok) {
        const code =
          isRecord(json) &&
          isRecord(json.error) &&
          typeof json.error.code === "string"
            ? json.error.code
            : "HTTP_ERROR";
        if (response.status === 401) this.onUnauthorized?.();
        throw new ApiError(
          response.status,
          code,
          statusMessage(response.status),
          response.headers.get("x-request-id") ?? undefined,
        );
      }
      if (epoch !== this.epoch || controller.signal.aborted)
        throw new DOMException("Aborted", "AbortError");
      return json;
    } catch (error) {
      if (timedOut)
        throw new ApiError(
          0,
          "TIMEOUT",
          "The request timed out. Please try again.",
        );
      if (error instanceof ApiError) throw error;
      if (controller.signal.aborted)
        throw new DOMException("Aborted", "AbortError");
      throw new ApiError(
        0,
        "NETWORK",
        "Cannot connect to the server. Check your connection and retry.",
      );
    } finally {
      clearTimeout(timer);
      options.signal?.removeEventListener("abort", abort);
      this.pending.delete(controller);
    }
  }
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
