// @atp/playwright-reporter — streams a Playwright run to the platform (§7).
//
// Invariant above everything else (§7.5): this reporter never fails the
// user's test run. Every network failure ends in a warning and a spool
// file; every exception inside a hook is caught. A platform outage must
// look, to the developer, like a warning line in CI output and nothing more.
//
// Configuration (playwright.config.ts):
//
//   reporter: [["list"], ["@atp/playwright-reporter", { url: "https://api...", token: process.env.ATP_TOKEN, environment: "staging" }]]
//
// or via env: ATP_URL, ATP_TOKEN, ATP_ENVIRONMENT.

import type { FullConfig, FullResult, Reporter, Suite, TestCase, TestResult, TestStep } from "@playwright/test/reporter";
import { randomUUID } from "node:crypto";
import { tmpdir } from "node:os";
import { relative } from "node:path";
import { uploadAttachment, type Attachment, type UploadOutcome } from "./artifacts";
import { Client, type RunResponse } from "./client";
import { clientTestId, normalizeFilePath, normalizeParams, type Signals } from "./fingerprint";
import { EventQueue, describe } from "./queue";
import { stripAnsi } from "./text";

export { Client, ApiError, NetworkError } from "./client";
export { clientTestId, fingerprint, normalize, normalizeFilePath, normalizeParams, normalizeTitle, TITLE_SEP, type Signals } from "./fingerprint";
export { stripHar } from "./har";
export { classify, uploadAttachment } from "./artifacts";
export { EventQueue } from "./queue";

export interface ReporterOptions {
  /** Platform base URL, e.g. https://api.example.com. Env: ATP_URL. */
  url?: string;
  /** Ingest token (atp_ing_…). Env: ATP_TOKEN. */
  token?: string;
  /** environmentKey of §7.3 (staging, prod-smoke…). Env: ATP_ENVIRONMENT. Default "local". */
  environment?: string;
  /** Repo root to strip from file paths (§9.2). Default: Playwright's rootDir. */
  repoRoot?: string;
  /** Where undeliverable batches are written. Default: <tmp>/atp-reporter. */
  spoolDir?: string;
  /** Upload attachments (screenshots, videos, traces, HAR). Default true. */
  uploadArtifacts?: boolean;
  /** Strip Authorization/Cookie from HAR files before upload (§11.1). Default true. */
  stripHar?: boolean;
  /**
   * How long onEnd waits for the platform after the last test before it
   * spools what is left and lets the run end. Default 30 000 (one request
   * timeout, §7.5). The tests' result never depends on it.
   */
  flushTimeoutMs?: number;
  /** Print debug lines. Env: ATP_DEBUG=1. */
  debug?: boolean;
  /** Test hooks. */
  _client?: Client;
  _now?: () => Date;
}

type Level = "warn" | "info" | "debug";

const ATTEMPT_STATUS: Record<string, string> = {
  passed: "passed",
  failed: "failed",
  timedOut: "timed_out",
  skipped: "skipped",
  interrupted: "interrupted",
};

export default class AtpReporter implements Reporter {
  private readonly opts: ReporterOptions;
  private client: Client | undefined;
  private queue: EventQueue | undefined;
  private localRunId = "";
  private run: RunResponse | undefined;
  private repoRoot = "";
  private environment = "local";
  private readonly occurrences = new Map<string, number>();
  private readonly testOccurrences = new WeakMap<TestCase, number>();
  private readonly uploads: Promise<UploadOutcome>[] = [];
  private uploadsInFlight = 0;
  private readonly uploadWaiters: (() => void)[] = [];
  private disabled = false;
  private readonly now: () => Date;
  private readonly identity = new Map<string, string>();

  constructor(opts: ReporterOptions = {}) {
    this.opts = opts;
    this.now = opts._now ?? (() => new Date());
  }

  printsToStdio(): boolean {
    return false;
  }

  private log(level: Level, msg: string): void {
    if (level === "debug" && !(this.opts.debug || process.env.ATP_DEBUG === "1")) return;
    const line = `[atp-reporter] ${msg}`;
    if (level === "warn") console.warn(line);
    else console.log(line);
  }

  onBegin(config: FullConfig, _suite: Suite): void {
    try {
      const url = this.opts.url ?? process.env.ATP_URL;
      const token = this.opts.token ?? process.env.ATP_TOKEN;
      if (!url || !token) {
        this.disabled = true;
        this.log("warn", "ATP_URL / ATP_TOKEN not set: results will not be reported (tests run normally)");
        return;
      }
      this.environment = this.opts.environment ?? process.env.ATP_ENVIRONMENT ?? "local";
      this.repoRoot = this.opts.repoRoot ?? config.rootDir;
      this.localRunId = randomUUID();
      this.client = this.opts._client ?? new Client({ url, token, userAgent: `atp-playwright-reporter/0.1.0 playwright/${config.version}` });
      // Create the run now, off the hot path; events queue behind it.
      const body = {
        localRunId: this.localRunId,
        environmentKey: this.environment,
        trigger: detectTrigger(),
        framework: { name: "playwright", version: config.version },
        reporter: { name: "@atp/playwright-reporter", version: "0.1.0", protocolVersion: "1.1" },
        startedAt: this.now().toISOString(),
      };
      this.runCreated = this.client
        .createRun(body)
        .then((r) => {
          this.run = r;
          this.log("debug", `run ${this.localRunId} → ${r.runId}`);
          return true;
        })
        .catch((e) => {
          // No run on the server: events will spool. The tests keep going.
          this.log("warn", `could not create run (${describe(e)}); events will be spooled`);
          return false;
        });
      this.queue = new EventQueue({
        ready: this.runCreated,
        localRunId: this.localRunId,
        client: this.client,
        spoolDir: this.opts.spoolDir ?? `${tmpdir()}/atp-reporter`,
        log: (l, m) => this.log(l, m),
        onIdentity: (m) => {
          for (const [k, v] of Object.entries(m)) this.identity.set(k, v);
        },
      });
    } catch (e) {
      this.disabled = true;
      this.log("warn", `reporter disabled: ${describe(e)}`);
    }
  }

  private runCreated: Promise<boolean> = Promise.resolve(false);
  /** onEnd's budget ran out: nothing more goes to the platform. */
  private gaveUp = false;

  onTestEnd(test: TestCase, result: TestResult): void {
    if (this.disabled || !this.queue) return;
    try {
      const signals = this.signalsOf(test);
      const ct = clientTestId(signals);
      const occurrence = this.occurrenceOf(test, ct);
      const started = result.startTime;
      const finished = new Date(started.getTime() + result.duration);
      const event: Record<string, unknown> = {
        type: "test.finished",
        clientTestId: ct,
        occurrenceIndex: occurrence,
        attempt: result.retry,
        identity: signals,
        status: ATTEMPT_STATUS[result.status] ?? "interrupted",
        outcome: test.outcome(), // Playwright's verdict, carried, never re-derived (N36)
        durationMs: Math.max(0, Math.round(result.duration)),
        workerId: String(result.workerIndex),
        startedAt: started.toISOString(),
        finishedAt: finished.toISOString(),
      };
      if (result.error) {
        event.error = {
          message: stripAnsi(result.error.message ?? ""),
          stack: stripAnsi(result.error.stack ?? ""),
          snippet: stripAnsi(result.error.snippet ?? ""),
        };
      }
      // Sequenced now; the queue holds batches until the run exists.
      this.queue.push(event);

      if (this.opts.uploadArtifacts !== false && result.attachments.length > 0) {
        const ref = { localRunId: this.localRunId, clientTestId: ct, occurrenceIndex: occurrence, attempt: result.retry };
        for (const att of result.attachments) {
          const a: Attachment = { name: att.name, contentType: att.contentType, path: att.path, body: att.body };
          this.uploads.push(this.runCreated.then(() => this.upload(ref, a)));
        }
      }
    } catch (e) {
      this.log("warn", `onTestEnd: ${describe(e)}`);
    }
  }

  private async upload(ref: { localRunId: string; clientTestId: string; occurrenceIndex: number; attempt: number }, a: Attachment): Promise<UploadOutcome> {
    // At most 4 uploads in flight, so a run with 500 screenshots does not
    // open 500 sockets.
    while (this.uploadsInFlight >= 4) await new Promise<void>((r) => this.uploadWaiters.push(r));
    this.uploadsInFlight++;
    try {
      if (!this.client || !this.run || this.gaveUp) return { name: a.name, status: "skipped", reason: "no run" };
      return await uploadAttachment(this.client, ref, a, { stripHar: this.opts.stripHar !== false, limits: this.run.limits.artifactBytes, log: (l, m) => this.log(l, m) });
    } finally {
      this.uploadsInFlight--;
      this.uploadWaiters.shift()?.();
    }
  }

  async onEnd(_result: FullResult): Promise<void> {
    if (this.disabled || !this.queue || !this.client) return;
    // The platform gets flushTimeoutMs after the last test; then whatever is
    // undelivered goes to the spool and the run ends (§7.5).
    const budget = this.opts.flushTimeoutMs ?? 30_000;
    let timer: NodeJS.Timeout | undefined;
    const expired = new Promise<"expired">((r) => {
      timer = setTimeout(() => r("expired"), budget);
    });
    const outcome = await Promise.race([this.deliver().then(() => "done" as const), expired]);
    clearTimeout(timer);
    if (outcome === "expired") {
      this.gaveUp = true;
      this.queue.abandon(`no answer from the platform within ${budget} ms of the last test`);
      this.log("warn", `gave up on the platform after ${budget} ms: ${this.queue.stats.spooled} events spooled, uploads and finish skipped (tests unaffected)`);
    }
  }

  private async deliver(): Promise<void> {
    const queue = this.queue!;
    const client = this.client!;
    try {
      await this.runCreated;
      await queue.close();
      const outcomes = await Promise.all(this.uploads);
      const s = queue.stats;
      const counts = { verified: 0, skipped: 0, too_large: 0, failed: 0 };
      for (const o of outcomes) counts[o.status]++;
      if (this.gaveUp) return;
      if (this.run) {
        try {
          const fin = await client.finishRun(this.localRunId, this.now().toISOString());
          this.log("info", `run ${fin.runId}: ${fin.status}, ${s.accepted} events (${s.duplicates} dup, ${s.spooled} spooled), artifacts ${counts.verified} ok / ${counts.too_large} over cap / ${counts.failed} failed`);
        } catch (e) {
          this.log("warn", `could not finish run (${describe(e)}); the server will auto-close it (§7.4)`);
        }
      } else if (s.spooled > 0) {
        this.log("warn", `${s.spooled} events spooled; run was never created on the server`);
      }
    } catch (e) {
      this.log("warn", `onEnd: ${describe(e)}`);
    }
  }

  /** §7.3 identity: file relative to the repo root, describe titles then the test title, project as the parameter. */
  private signalsOf(test: TestCase): Signals {
    const file = this.repoRoot ? relative(this.repoRoot, test.location.file) : test.location.file;
    // titlePath() is [root "", project, file, ...describes, title]; keep the describes and the title.
    const titles = test.titlePath().slice(3);
    const project = test.parent.project()?.name ?? "";
    return {
      framework: "playwright",
      filePath: normalizeFilePath(file),
      titlePath: titles.length > 0 ? titles : [test.title],
      paramSignature: project ? normalizeParams({ project }) : "",
    };
  }

  /** occurrenceIndex (N03): the n-th test with this fingerprint in the run. Retries of the same test keep their index. */
  private occurrenceOf(test: TestCase, ct: string): number {
    const existing = this.testOccurrences.get(test);
    if (existing !== undefined) return existing;
    const n = this.occurrences.get(ct) ?? 0;
    this.occurrences.set(ct, n + 1);
    this.testOccurrences.set(test, n);
    return n;
  }

  onStepEnd(_test: TestCase, _result: TestResult, _step: TestStep): void {}
}

/** CI trigger detection (§7.3 trigger). Only well-known variables; nothing is fetched (§7.10). */
export function detectTrigger(env: NodeJS.ProcessEnv = process.env): Record<string, string> {
  if (env.GITHUB_ACTIONS) {
    const url = env.GITHUB_SERVER_URL && env.GITHUB_REPOSITORY && env.GITHUB_RUN_ID ? `${env.GITHUB_SERVER_URL}/${env.GITHUB_REPOSITORY}/actions/runs/${env.GITHUB_RUN_ID}` : "";
    return { type: "ci", provider: "github-actions", ref: env.GITHUB_REF ?? "", commitSha: env.GITHUB_SHA ?? "", ciRunUrl: url };
  }
  if (env.GITLAB_CI) {
    return { type: "ci", provider: "gitlab-ci", ref: env.CI_COMMIT_REF_NAME ? `refs/heads/${env.CI_COMMIT_REF_NAME}` : "", commitSha: env.CI_COMMIT_SHA ?? "", ciRunUrl: env.CI_JOB_URL ?? "" };
  }
  if (env.CI) return { type: "ci", provider: "unknown" };
  return { type: "local" };
}
