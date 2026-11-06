import type { ApiClient } from "../../core/api/client";
import * as d from "../../core/api/decode";
import type { CheckStatus } from "../testmgmt/testmgmtRepository";
import { locators, stepActions, type AutoStep } from "./steps";
import { defaultVariant, runVariants } from "./matrix";
export type { AutoStep, Locator, StepAction } from "./steps";

// Auto test (QĐ-16): test cases the server runs by itself — the paths of
// backend/deploy/openapi.yaml (auto-specs, auto-drafts, auto-runs,
// environment accounts, site scans) and their field-by-field decoding.
// Step text, titles, console lines and page titles come from people, a
// model or the site under test: components interpolate them, never render
// HTML.

export type LoginStatus = "unknown" | "finding" | "ok" | "failed";
export interface TestAccount {
  key: string;
  username: string;
  loginUrl: string;
  loginSteps: AutoStep[] | null;
  loginStatus: LoginStatus;
  loginError: string;
  loginCheckedAt: string | null;
  updatedBy: string;
  updatedAt: string;
}
export interface AccountInput {
  key: string;
  username: string;
  password?: string;
  loginUrl: string;
  loginSteps?: AutoStep[];
}
export type CaseStatus = "pending" | "passed" | "failed" | "error";
export interface AutoSpec {
  caseId: string;
  title: string;
  priority: string;
  target: string;
  account: string;
  inputs: Record<string, string>;
  steps: AutoStep[];
  source: "manual" | "ai";
  updatedBy: string;
  updatedAt: string;
  last: { runId: string; status: CaseStatus; finishedAt: string } | null;
}
export interface SpecInput {
  target: string;
  account: string;
  inputs: Record<string, string>;
  steps: AutoStep[];
}
export interface CaseInput extends SpecInput {
  title: string;
  priority: string;
  testType: string;
  source: "manual" | "ai";
}
export interface AutoDraft {
  id: string;
  environmentId: string;
  target: string;
  account: string;
  inputs: Record<string, string>;
  context: string;
  count: number;
  testType: string;
  status: "queued" | "done" | "error";
  drafts: { title: string; priority: string; why: string; steps: AutoStep[] }[];
  pageRead: boolean;
  model: string;
  keySource: string;
  error: string;
  requestedBy: string;
  createdAt: string;
  finishedAt: string | null;
}
export interface DraftInput {
  environmentId: string;
  target: string;
  account: string;
  inputs: Record<string, string>;
  context: string;
  count: number;
  testType: string;
}
export type FailureKind =
  | "input"
  | "locator"
  | "assert"
  | "navigation"
  | "page";
export interface AutoStepResult {
  status: "passed" | "failed" | "skipped";
  kind: FailureKind | "";
  error: string;
  note: string;
  ms: number;
  healed: { steps: AutoStep[]; reason: string } | null;
}
export interface AutoRunCase {
  caseId: string;
  variant: string;
  position: number;
  title: string;
  target: string;
  account: string;
  inputs: Record<string, string>;
  steps: AutoStep[];
  status: CaseStatus;
  results: AutoStepResult[];
  console: string[];
  healed: number;
  durationMs: number;
  error: string;
  screenshots: ("final" | "failure")[];
  executionId: string;
  finishedAt: string | null;
}
export interface AutoRun {
  id: string;
  environmentId: string;
  planId: string;
  status: "queued" | "running" | "done" | "error";
  trigger: "dashboard" | "ci";
  requestedBy: string;
  heal: boolean;
  variants: string[];
  total: number;
  passed: number;
  failed: number;
  errored: number;
  healed: number;
  testRunId: string;
  error: string;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
  cases: AutoRunCase[];
}
export interface ScanPage {
  id: string;
  url: string;
  status: "queued" | "done" | "error";
  verdict: "" | "passed" | "warning" | "failed";
  title: string;
  depth: number;
  checkStatuses: CheckStatus[];
  error: string;
}
export interface SiteScan {
  id: string;
  environmentId: string;
  startUrl: string;
  maxPages: number;
  account: string;
  status: "running" | "done" | "error";
  requestedBy: string;
  error: string;
  pages: number;
  checked: number;
  passed: number;
  warnings: number;
  failed: number;
  createdAt: string;
  finishedAt: string | null;
  checks: ScanPage[];
}
export interface ScanInput {
  environmentId: string;
  url: string;
  account: string;
  maxPages: number;
}
export interface SiteOwnership {
  host: string;
  token: string;
  method: "" | "file" | "dns";
  status: "unverified" | "checking" | "verified" | "failed";
  error: string;
  verifiedAt: string | null;
  checkedAt: string | null;
  fileUrl: string;
  fileText: string;
  dnsName: string;
  dnsText: string;
}
export interface LoadRunInput {
  environmentId: string;
  paths: string[];
  vus: number;
  seconds: number;
  rampSeconds: number;
  maxP95Ms?: number;
  maxErrorRate?: number;
}
export interface LoadResult {
  requests: number;
  errors: number;
  clientErrors: number;
  errorRate: number;
  rps: number;
  latencyMs: {
    min: number;
    p50: number;
    p90: number;
    p95: number;
    p99: number;
    max: number;
  };
  statuses: Record<string, number>;
  errorSamples: string[];
  timeline: { t: number; requests: number; errors: number; avgMs: number }[];
  seconds: number;
}
export interface LoadRun {
  id: string;
  environmentId: string;
  host: string;
  paths: string[];
  vus: number;
  seconds: number;
  rampSeconds: number;
  maxP95Ms: number | null;
  maxErrorRate: number | null;
  status: "queued" | "running" | "done" | "error";
  verdict: "" | "passed" | "failed";
  reasons: string[];
  result: LoadResult | null;
  error: string;
  requestedBy: string;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
}
export interface ScriptRun {
  id: string;
  environmentId: string;
  kind: "playwright" | "k6";
  base: string;
  entry: string;
  project: string;
  grep: string;
  vus: number | null;
  seconds: number | null;
  timeoutSeconds: number;
  archiveBytes: number;
  status: "queued" | "running" | "done" | "error";
  verdict: "" | "passed" | "failed";
  exitCode: number | null;
  timedOut: boolean;
  tests: number;
  failures: number;
  log: string;
  error: string;
  testRunId: string | null;
  requestedBy: string;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
}
export type Listing<T> = { items: T[]; enabled: boolean };

function oneOf<T extends string>(value: unknown, allowed: readonly T[]): T {
  return allowed.includes(value as T) ? (value as T) : d.invalid();
}
const date = (v: unknown) => (v == null ? null : d.date(v));
function strings(value: unknown, max: number): Record<string, string> {
  const x = d.record(value ?? {});
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(x).slice(0, max)) out[k] = d.string(v);
  return out;
}

export function step(value: unknown): AutoStep {
  const x = d.record(value);
  return {
    do: oneOf(x.do, stepActions),
    by: oneOf(x.by ?? "", locators),
    role: d.string(x.role ?? ""),
    name: d.string(x.name ?? ""),
    value: d.string(x.value ?? ""),
    say: d.string(x.say ?? ""),
    expected: d.string(x.expected ?? ""),
  };
}
const steps = (v: unknown) => d.array(v ?? [], step, 40);

function account(value: unknown): TestAccount {
  const x = d.record(value);
  return {
    key: d.string(x.key),
    username: d.string(x.username),
    loginUrl: d.string(x.loginUrl ?? ""),
    loginSteps: x.loginSteps == null ? null : steps(x.loginSteps),
    loginStatus: oneOf(x.loginStatus, ["unknown", "finding", "ok", "failed"]),
    loginError: d.string(x.loginError ?? ""),
    loginCheckedAt: date(x.loginCheckedAt),
    updatedBy: d.string(x.updatedBy),
    updatedAt: d.date(x.updatedAt),
  };
}
const caseStatuses = ["pending", "passed", "failed", "error"] as const;
function spec(value: unknown): AutoSpec {
  const x = d.record(value);
  const last = x.last == null ? null : d.record(x.last);
  return {
    caseId: d.string(x.caseId),
    title: d.string(x.title),
    priority: d.string(x.priority ?? ""),
    target: d.string(x.target),
    account: d.string(x.account ?? ""),
    inputs: strings(x.inputs, 30),
    steps: steps(x.steps),
    source: oneOf(x.source, ["manual", "ai"]),
    updatedBy: d.string(x.updatedBy),
    updatedAt: d.date(x.updatedAt),
    last: last
      ? {
          runId: d.string(last.runId),
          status: oneOf(last.status, caseStatuses),
          finishedAt: d.date(last.finishedAt),
        }
      : null,
  };
}
function draft(value: unknown): AutoDraft {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    environmentId: d.string(x.environmentId),
    target: d.string(x.target),
    account: d.string(x.account ?? ""),
    inputs: strings(x.inputs, 30),
    context: d.string(x.context ?? ""),
    count: d.integer(x.count),
    testType: d.string(x.testType ?? ""),
    status: oneOf(x.status, ["queued", "done", "error"]),
    drafts: d.array(
      x.drafts ?? [],
      (v) => {
        const c = d.record(v);
        return {
          title: d.string(c.title),
          priority: d.string(c.priority),
          why: d.string(c.why ?? ""),
          steps: steps(c.steps),
        };
      },
      15,
    ),
    pageRead: d.boolean(x.pageRead),
    model: d.string(x.model ?? ""),
    keySource: d.string(x.keySource ?? ""),
    error: d.string(x.error ?? ""),
    requestedBy: d.string(x.requestedBy),
    createdAt: d.date(x.createdAt),
    finishedAt: date(x.finishedAt),
  };
}
function stepResult(value: unknown): AutoStepResult {
  const x = d.record(value);
  const h = x.healed == null ? null : d.record(x.healed);
  return {
    status: oneOf(x.status, ["passed", "failed", "skipped"]),
    kind: oneOf(x.kind ?? "", [
      "",
      "input",
      "locator",
      "assert",
      "navigation",
      "page",
    ]),
    error: d.string(x.error ?? ""),
    note: d.string(x.note ?? ""),
    ms: d.integer(x.ms ?? 0),
    healed: h ? { steps: steps(h.steps), reason: d.string(h.reason) } : null,
  };
}
function runCase(value: unknown): AutoRunCase {
  const x = d.record(value);
  return {
    caseId: d.string(x.caseId),
    variant: oneOf(x.variant ?? "", ["", ...runVariants]),
    position: d.integer(x.position),
    title: d.string(x.title),
    target: d.string(x.target ?? ""),
    account: d.string(x.account ?? ""),
    inputs: strings(x.inputs, 30),
    steps: steps(x.steps),
    status: oneOf(x.status, caseStatuses),
    results: d.array(x.results ?? [], stepResult, 40),
    console: d.array(x.console ?? [], d.string, 50),
    healed: d.integer(x.healed ?? 0),
    durationMs: d.integer(x.durationMs ?? 0),
    error: d.string(x.error ?? ""),
    screenshots: d.array(
      x.screenshots ?? [],
      (v) => oneOf(v, ["final", "failure"]),
      2,
    ),
    executionId: d.string(x.executionId ?? ""),
    finishedAt: date(x.finishedAt),
  };
}
function run(value: unknown): AutoRun {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    environmentId: d.string(x.environmentId),
    planId: d.string(x.planId ?? ""),
    status: oneOf(x.status, ["queued", "running", "done", "error"]),
    trigger: oneOf(x.trigger, ["dashboard", "ci"]),
    requestedBy: d.string(x.requestedBy),
    heal: d.boolean(x.heal),
    variants: d.array(
      x.variants ?? [defaultVariant],
      (v) => oneOf(v, runVariants),
      9,
    ),
    total: d.integer(x.total),
    passed: d.integer(x.passed),
    failed: d.integer(x.failed),
    errored: d.integer(x.errored),
    healed: d.integer(x.healed),
    testRunId: d.string(x.testRunId ?? ""),
    error: d.string(x.error ?? ""),
    createdAt: d.date(x.createdAt),
    startedAt: date(x.startedAt),
    finishedAt: date(x.finishedAt),
    cases: d.array(x.cases ?? [], runCase, 100),
  };
}
const checkStatuses = ["passed", "warning", "failed", "not_run"] as const;
function scan(value: unknown): SiteScan {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    environmentId: d.string(x.environmentId ?? ""),
    startUrl: d.string(x.startUrl),
    maxPages: d.integer(x.maxPages),
    account: d.string(x.account ?? ""),
    status: oneOf(x.status, ["running", "done", "error"]),
    requestedBy: d.string(x.requestedBy),
    error: d.string(x.error ?? ""),
    pages: d.integer(x.pages),
    checked: d.integer(x.checked),
    passed: d.integer(x.passed),
    warnings: d.integer(x.warnings),
    failed: d.integer(x.failed),
    createdAt: d.date(x.createdAt),
    finishedAt: date(x.finishedAt),
    checks: d.array(
      x.checks ?? [],
      (v) => {
        const c = d.record(v);
        return {
          id: d.string(c.id),
          url: d.string(c.url),
          status: oneOf(c.status, ["queued", "done", "error"]),
          verdict: oneOf(c.verdict, ["", "passed", "warning", "failed"]),
          title: d.string(c.title ?? ""),
          depth: d.integer(c.depth ?? 0),
          checkStatuses: d.array(
            c.checkStatuses ?? [],
            (s) => oneOf(s, checkStatuses),
            20,
          ),
          error: d.string(c.error ?? ""),
        };
      },
      50,
    ),
  };
}
function quantity(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0
    ? value
    : d.invalid();
}
function ownership(value: unknown): SiteOwnership {
  const x = d.record(value);
  return {
    host: d.string(x.host),
    token: d.string(x.token),
    method: oneOf(x.method, ["", "file", "dns"]),
    status: oneOf(x.status, ["unverified", "checking", "verified", "failed"]),
    error: d.string(x.error),
    verifiedAt: date(x.verifiedAt),
    checkedAt: date(x.checkedAt),
    fileUrl: d.string(x.fileUrl),
    fileText: d.string(x.fileText),
    dnsName: d.string(x.dnsName),
    dnsText: d.string(x.dnsText),
  };
}
function loadResult(value: unknown): LoadResult {
  const x = d.record(value);
  const latency = d.record(x.latencyMs);
  const statuses = d.record(x.statuses);
  if (Object.keys(statuses).length > 100) return d.invalid();
  return {
    requests: d.integer(x.requests),
    errors: d.integer(x.errors),
    clientErrors: d.integer(x.clientErrors),
    errorRate: quantity(x.errorRate),
    rps: quantity(x.rps),
    latencyMs: {
      min: d.integer(latency.min),
      p50: d.integer(latency.p50),
      p90: d.integer(latency.p90),
      p95: d.integer(latency.p95),
      p99: d.integer(latency.p99),
      max: d.integer(latency.max),
    },
    statuses: Object.fromEntries(
      Object.entries(statuses).map(([code, count]) => [code, d.integer(count)]),
    ),
    errorSamples: d.array(x.errorSamples, d.string, 10),
    timeline: d.array(
      x.timeline,
      (value) => {
        const point = d.record(value);
        return {
          t: d.integer(point.t),
          requests: d.integer(point.requests),
          errors: d.integer(point.errors),
          avgMs: d.integer(point.avgMs),
        };
      },
      600,
    ),
    seconds: d.integer(x.seconds),
  };
}
function loadRun(value: unknown): LoadRun {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    environmentId: d.string(x.environmentId),
    host: d.string(x.host),
    paths: d.array(x.paths, d.string, 20),
    vus: d.integer(x.vus),
    seconds: d.integer(x.seconds),
    rampSeconds: d.integer(x.rampSeconds),
    maxP95Ms: x.maxP95Ms == null ? null : d.integer(x.maxP95Ms),
    maxErrorRate: x.maxErrorRate == null ? null : quantity(x.maxErrorRate),
    status: oneOf(x.status, ["queued", "running", "done", "error"]),
    verdict: oneOf(x.verdict, ["", "passed", "failed"]),
    reasons: d.array(x.reasons, d.string, 20),
    result: x.result == null ? null : loadResult(x.result),
    error: d.string(x.error),
    requestedBy: d.string(x.requestedBy),
    createdAt: d.date(x.createdAt),
    startedAt: date(x.startedAt),
    finishedAt: date(x.finishedAt),
  };
}
function scriptRun(value: unknown): ScriptRun {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    environmentId: d.string(x.environmentId),
    kind: oneOf(x.kind, ["playwright", "k6"]),
    base: d.string(x.base),
    entry: d.string(x.entry ?? ""),
    project: d.string(x.project ?? ""),
    grep: d.string(x.grep ?? ""),
    vus: x.vus == null ? null : d.integer(x.vus),
    seconds: x.seconds == null ? null : d.integer(x.seconds),
    timeoutSeconds: d.integer(x.timeoutSeconds),
    archiveBytes: d.integer(x.archiveBytes),
    status: oneOf(x.status, ["queued", "running", "done", "error"]),
    verdict: oneOf(x.verdict, ["", "passed", "failed"]),
    exitCode: x.exitCode == null ? null : d.integer(x.exitCode),
    timedOut: d.boolean(x.timedOut),
    tests: d.integer(x.tests),
    failures: d.integer(x.failures),
    log: d.string(x.log ?? ""),
    error: d.string(x.error ?? ""),
    testRunId: x.testRunId == null ? null : d.string(x.testRunId),
    requestedBy: d.string(x.requestedBy),
    createdAt: d.date(x.createdAt),
    startedAt: date(x.startedAt),
    finishedAt: date(x.finishedAt),
  };
}
function listing<T>(
  value: unknown,
  parse: (v: unknown) => T,
  max: number,
): Listing<T> {
  const x = d.record(value);
  return { items: d.array(x.items, parse, max), enabled: d.boolean(x.enabled) };
}

const enc = encodeURIComponent;
const scope = (project: string) => `/v1/projects/${enc(project)}`;

export class AutotestRepository {
  constructor(private api: ApiClient) {}

  // Test accounts of an environment.
  async accounts(project: string, env: string, signal: AbortSignal) {
    const x = d.record(
      await this.api.request(
        `${scope(project)}/environments/${enc(env)}/accounts`,
        { signal },
      ),
    );
    return d.array(x.items, account, 20);
  }
  async saveAccount(
    project: string,
    env: string,
    input: AccountInput,
    signal: AbortSignal,
  ) {
    const body: Record<string, unknown> = {
      key: input.key,
      username: input.username,
      loginUrl: input.loginUrl,
    };
    if (input.password) body.password = input.password;
    if (input.loginSteps) body.loginSteps = input.loginSteps;
    return account(
      await this.api.request(
        `${scope(project)}/environments/${enc(env)}/accounts`,
        { body, signal },
      ),
    );
  }
  async deleteAccount(
    project: string,
    env: string,
    key: string,
    signal: AbortSignal,
  ) {
    await this.api.request(
      `${scope(project)}/environments/${enc(env)}/accounts/${enc(key)}`,
      { method: "DELETE", signal },
    );
  }
  async findLogin(
    project: string,
    env: string,
    key: string,
    signal: AbortSignal,
  ) {
    return account(
      await this.api.request(
        `${scope(project)}/environments/${enc(env)}/accounts/${enc(key)}/find-login`,
        { method: "POST", body: {}, signal },
      ),
    );
  }

  // Runnable cases.
  async specs(project: string, signal: AbortSignal) {
    return listing(
      await this.api.request(`${scope(project)}/auto-specs`, { signal }),
      spec,
      1000,
    );
  }
  async createCase(project: string, input: CaseInput, signal: AbortSignal) {
    return spec(
      await this.api.request(`${scope(project)}/auto-specs`, {
        body: input,
        signal,
      }),
    );
  }
  async saveSpec(
    project: string,
    caseId: string,
    input: SpecInput,
    signal: AbortSignal,
  ) {
    return spec(
      await this.api.request(
        `${scope(project)}/test-cases/${enc(caseId)}/auto-spec`,
        { body: input, signal },
      ),
    );
  }
  async deleteSpec(project: string, caseId: string, signal: AbortSignal) {
    await this.api.request(
      `${scope(project)}/test-cases/${enc(caseId)}/auto-spec`,
      { method: "DELETE", signal },
    );
  }
  async exportSpecs(
    project: string,
    environmentId: string,
    caseIds: string[],
    signal: AbortSignal,
  ) {
    const q = new URLSearchParams();
    if (environmentId) q.set("environmentId", environmentId);
    if (caseIds.length) q.set("caseIds", caseIds.join(","));
    const x = d.record(
      await this.api.request(`${scope(project)}/auto-specs/export?${q}`, {
        signal,
      }),
    );
    return { filename: d.string(x.filename), content: d.string(x.content) };
  }

  // Drafts.
  async createDraft(project: string, input: DraftInput, signal: AbortSignal) {
    return draft(
      await this.api.request(`${scope(project)}/auto-drafts`, {
        body: input,
        signal,
      }),
    );
  }
  async draft(project: string, id: string, signal: AbortSignal) {
    return draft(
      await this.api.request(`${scope(project)}/auto-drafts/${enc(id)}`, {
        signal,
      }),
    );
  }

  // Runs.
  async createRun(
    project: string,
    input: {
      environmentId: string;
      caseIds?: string[];
      heal: boolean;
      variants?: string[];
    },
    signal: AbortSignal,
  ) {
    return run(
      await this.api.request(`${scope(project)}/auto-runs`, {
        body: input,
        signal,
      }),
    );
  }
  async runs(project: string, signal: AbortSignal) {
    return listing(
      await this.api.request(`${scope(project)}/auto-runs`, { signal }),
      run,
      50,
    );
  }
  async run(project: string, id: string, signal: AbortSignal) {
    return run(
      await this.api.request(`${scope(project)}/auto-runs/${enc(id)}`, {
        signal,
      }),
    );
  }
  shotUrl(
    project: string,
    runId: string,
    caseId: string,
    which: "final" | "failure",
    variant?: string,
  ) {
    const path = `${scope(project)}/auto-runs/${enc(runId)}/cases/${enc(caseId)}/screenshots/${which}`;
    return variant ? `${path}?variant=${enc(variant)}` : path;
  }
  async applyHeal(
    project: string,
    runId: string,
    caseId: string,
    stepIndex: number,
    signal: AbortSignal,
    variant?: string,
  ) {
    return spec(
      await this.api.request(
        `${scope(project)}/auto-runs/${enc(runId)}/cases/${enc(caseId)}/apply-heal`,
        { body: { step: stepIndex, ...(variant ? { variant } : {}) }, signal },
      ),
    );
  }

  // Site scans.
  async createScan(project: string, input: ScanInput, signal: AbortSignal) {
    return scan(
      await this.api.request(`${scope(project)}/site-scans`, {
        body: input,
        signal,
      }),
    );
  }
  async scans(project: string, signal: AbortSignal) {
    return listing(
      await this.api.request(`${scope(project)}/site-scans`, { signal }),
      scan,
      30,
    );
  }
  async scan(project: string, id: string, signal: AbortSignal) {
    return scan(
      await this.api.request(`${scope(project)}/site-scans/${enc(id)}`, {
        signal,
      }),
    );
  }

  // QĐ-19 tier 3: ownership proof and load tests.
  async ownership(project: string, environment: string, signal: AbortSignal) {
    return ownership(
      await this.api.request(
        `${scope(project)}/environments/${enc(environment)}/ownership`,
        { signal },
      ),
    );
  }
  async checkOwnership(
    project: string,
    environment: string,
    method: "file" | "dns",
    signal: AbortSignal,
  ) {
    return ownership(
      await this.api.request(
        `${scope(project)}/environments/${enc(environment)}/ownership/check`,
        { body: { method }, signal },
      ),
    );
  }
  async loadRuns(project: string, signal: AbortSignal) {
    return listing(
      await this.api.request(`${scope(project)}/load-runs`, { signal }),
      loadRun,
      50,
    );
  }
  async createLoadRun(
    project: string,
    input: LoadRunInput,
    signal: AbortSignal,
  ) {
    return loadRun(
      await this.api.request(`${scope(project)}/load-runs`, {
        body: input,
        signal,
      }),
    );
  }
  async loadRun(project: string, id: string, signal: AbortSignal) {
    return loadRun(
      await this.api.request(`${scope(project)}/load-runs/${enc(id)}`, {
        signal,
      }),
    );
  }
  async scriptRuns(project: string, signal: AbortSignal) {
    return listing(
      await this.api.request(`${scope(project)}/script-runs`, { signal }),
      scriptRun,
      50,
    );
  }
  async createScriptRun(project: string, data: FormData, signal: AbortSignal) {
    return scriptRun(
      await this.api.request(`${scope(project)}/script-runs`, {
        body: data,
        signal,
        timeoutMs: 60000,
      }),
    );
  }
  async scriptRun(project: string, id: string, signal: AbortSignal) {
    return scriptRun(
      await this.api.request(`${scope(project)}/script-runs/${enc(id)}`, {
        signal,
      }),
    );
  }
}
