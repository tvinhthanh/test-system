// HTTP client for §7.2 with the §7.5 rules: 30 s timeout on every request,
// exponential backoff 1/2/4/8 s up to 5 tries on 5xx and timeouts, no retry
// on 4xx except 429 (which honours Retry-After). Errors are returned, not
// thrown past the reporter: the reporter must never fail the user's run.

export interface ClientOptions {
  url: string;
  token: string;
  timeoutMs?: number;
  maxAttempts?: number;
  /** Sleep hook, replaced in tests. */
  sleep?: (ms: number) => Promise<void>;
  /** Fetch hook, replaced in tests. */
  fetch?: typeof fetch;
  protocolVersion?: string;
  userAgent?: string;
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
    public readonly details?: Record<string, unknown>,
    public readonly retryAfterMs?: number,
  ) {
    super(message);
  }
  /** §7.5: 5xx and 429 are retryable; other 4xx are not. */
  get retryable(): boolean {
    return this.status >= 500 || this.status === 429;
  }
}

export class NetworkError extends Error {
  readonly retryable = true;
}

export interface RunResponse {
  runId: string;
  limits: { maxEventsPerBatch: number; maxBatchBytes: number; artifactBytes: Record<string, number> };
  identityMap: Record<string, string>;
}

export interface BatchResponse {
  accepted: number;
  duplicates: number;
  identityMap: Record<string, string>;
}

export interface FinishResponse {
  runId: string;
  status: string;
  flakyCount: number;
  finishedAt: string;
}

export interface DeclareResponse {
  artifactId: string;
  status: string;
  expiresAt: string;
  upload?: { method: string; url: string; headers: Record<string, string>; expiresAt: string };
  multipart?: { uploadId: string; partSize: number; maxParallel: number; parts: { number: number; url: string }[]; received: number[]; expiresAt: string };
  linked: boolean;
}

export class Client {
  private readonly base: string;
  private readonly timeoutMs: number;
  private readonly maxAttempts: number;
  private readonly sleep: (ms: number) => Promise<void>;
  private readonly doFetch: typeof fetch;
  private readonly headers: Record<string, string>;

  constructor(private readonly opts: ClientOptions) {
    this.base = opts.url.replace(/\/+$/, "");
    this.timeoutMs = opts.timeoutMs ?? 30_000;
    this.maxAttempts = opts.maxAttempts ?? 5;
    this.sleep = opts.sleep ?? ((ms) => new Promise((r) => setTimeout(r, ms)));
    this.doFetch = opts.fetch ?? fetch;
    this.headers = {
      Authorization: `Bearer ${opts.token}`,
      "Content-Type": "application/json",
      "X-Protocol-Version": opts.protocolVersion ?? "1.1",
      "User-Agent": opts.userAgent ?? "atp-playwright-reporter",
    };
  }

  createRun(body: unknown): Promise<RunResponse> {
    return this.post("/v1/runs", body);
  }

  sendEvents(localRunId: string, events: unknown[]): Promise<BatchResponse> {
    return this.post(`/v1/runs/${localRunId}/events`, { events });
  }

  finishRun(localRunId: string, finishedAt: string): Promise<FinishResponse> {
    return this.post(`/v1/runs/${localRunId}/finish`, { finishedAt });
  }

  declareArtifact(body: unknown): Promise<DeclareResponse> {
    return this.post("/v1/artifacts", body);
  }

  completeArtifact(artifactId: string, body: unknown): Promise<{ artifactId: string; status: string; sizeBytes: number }> {
    return this.post(`/v1/artifacts/${artifactId}/complete`, body);
  }

  /** Raw PUT to a presigned URL (endpoint 5 of §7.2): no auth header, same retry policy. */
  async put(url: string, body: Uint8Array, headers: Record<string, string>): Promise<{ etag: string }> {
    return this.retrying(async () => {
      const res = await this.withTimeout((signal) => this.doFetch(url, { method: "PUT", body, headers, signal }));
      if (res.status >= 200 && res.status < 300) return { etag: res.headers.get("etag") ?? "" };
      const text = await res.text().catch(() => "");
      throw new ApiError(res.status, "UPLOAD_FAILED", `PUT ${res.status}: ${text.slice(0, 200)}`, undefined, retryAfter(res));
    });
  }

  private async post<T>(path: string, body: unknown): Promise<T> {
    const payload = JSON.stringify(body);
    return this.retrying(async () => {
      const res = await this.withTimeout((signal) => this.doFetch(this.base + path, { method: "POST", body: payload, headers: this.headers, signal }));
      const text = await res.text();
      let json: any = {};
      try {
        json = text ? JSON.parse(text) : {};
      } catch {
        json = {};
      }
      if (res.status >= 200 && res.status < 300) return json as T;
      const err = json?.error ?? {};
      throw new ApiError(res.status, err.code ?? "HTTP_" + res.status, err.message ?? `HTTP ${res.status}`, err.details, retryAfter(res));
    });
  }

  private async withTimeout<T>(fn: (signal: AbortSignal) => Promise<T>): Promise<T> {
    const ac = new AbortController();
    const timer = setTimeout(() => ac.abort(), this.timeoutMs);
    try {
      return await fn(ac.signal);
    } catch (e: any) {
      // AbortError (timeout) and connection failures are network errors.
      throw new NetworkError(e?.name === "AbortError" ? `timeout after ${this.timeoutMs} ms` : String(e?.message ?? e));
    } finally {
      clearTimeout(timer);
    }
  }

  /** §7.5 backoff: 1, 2, 4, 8 s between at most 5 attempts. */
  private async retrying<T>(fn: () => Promise<T>): Promise<T> {
    let attempt = 0;
    for (;;) {
      attempt++;
      try {
        return await fn();
      } catch (e) {
        const retryable = e instanceof ApiError ? e.retryable : e instanceof NetworkError;
        if (!retryable || attempt >= this.maxAttempts) throw e;
        const backoff = 1000 * 2 ** (attempt - 1);
        const wait = e instanceof ApiError && e.retryAfterMs ? Math.max(e.retryAfterMs, backoff) : backoff;
        await this.sleep(wait);
      }
    }
  }
}

function retryAfter(res: Response): number | undefined {
  const v = res.headers.get("retry-after");
  if (!v) return undefined;
  const secs = Number(v);
  return Number.isFinite(secs) ? secs * 1000 : undefined;
}
