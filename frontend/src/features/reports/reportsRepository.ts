import type { ApiClient } from "../../core/api/client";
import * as d from "../../core/api/decode";
import { isFailureCategory } from "./categories";

export interface Project {
  id: string;
  name: string;
  workspaceName: string;
}
export interface Run {
  id: string;
  branch: string;
  commitSha: string;
  status: string;
  environmentName: string;
  startedAt: string;
  durationMs: number | null;
  totalCount: number;
  passedCount: number;
  failedCount: number;
  flakyCount: number;
}
export interface TestResult {
  id: string;
  displayName: string;
  filePath: string;
  finalStatus: string;
  attemptCount: number;
  durationMs: number;
  stableTestId: string | null;
}
export interface Attempt {
  attempt: number;
  status: string;
  durationMs: number;
  message: string | null;
  stack: string | null;
  artifacts: ArtifactRef[];
}
// A verified upload linked to an attempt; bytes come from the repository's
// artifactContentUrl (the server decides inline vs download, §11.5).
export interface ArtifactRef {
  id: string;
  name: string;
  kind: string;
  contentType: string;
  sizeBytes: number;
}
export interface TriageEvent {
  id: string;
  type: string;
  actorName: string;
  occurredAt: string;
}
export interface ResultDetail {
  result: TestResult;
  attempts: Attempt[];
  events: TriageEvent[];
}
// Engineering overview (§2.5 EM screen, §6.4 product signals). Every number is
// computed by the server over the window; the client only formats.
export interface TriageMetrics {
  failures: number;
  triaged: number;
  seen: number;
  untriagedRate: number;
  triageCoverage: number;
  ttrcMedianSeconds: number | null;
  timeToFirstViewSeconds: number | null;
}
export interface TrendBucket {
  week: string; // start of the week, RFC3339 UTC
  category: string; // §8.7 enum; "unknown" = not triaged yet
  count: number;
}
export interface Identity {
  id: string;
  canonicalTitle: string;
  canonicalFilePath: string;
  verdict: string;
  flakyScore: number | null;
  totalRuns: number;
  recentStatuses: string[];
}
export interface RunsQuery {
  q: string;
  status: string;
  environment: string;
  cursor: string;
}
export const emptyQuery: RunsQuery = {
  q: "",
  status: "",
  environment: "",
  cursor: "",
};
function project(value: unknown): Project {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    name: d.string(x.name),
    workspaceName: d.string(x.workspaceName),
  };
}
export function parseRun(value: unknown): Run {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    branch: d.string(x.branch),
    commitSha: d.string(x.commitSha),
    status: d.string(x.status),
    environmentName: d.string(x.environmentName),
    startedAt: d.date(x.startedAt),
    durationMs: x.durationMs === null ? null : d.integer(x.durationMs),
    totalCount: d.integer(x.totalCount),
    passedCount: d.integer(x.passedCount),
    failedCount: d.integer(x.failedCount),
    flakyCount: d.integer(x.flakyCount),
  };
}
function result(value: unknown): TestResult {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    displayName: d.string(x.displayName),
    filePath: d.string(x.filePath),
    finalStatus: d.string(x.finalStatus),
    attemptCount: d.integer(x.attemptCount),
    durationMs: d.integer(x.durationMs),
    stableTestId: d.nullableString(x.stableTestId),
  };
}
function artifactRef(value: unknown): ArtifactRef {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    name: d.string(x.name),
    kind: d.string(x.kind),
    contentType: d.string(x.contentType),
    sizeBytes: d.integer(x.sizeBytes),
  };
}
function attempt(value: unknown): Attempt {
  const x = d.record(value);
  const error = x.error === null ? null : d.record(x.error);
  return {
    attempt: d.integer(x.attempt),
    status: d.string(x.status),
    durationMs: d.integer(x.durationMs),
    message: error ? d.nullableString(error.message) : null,
    stack: error ? d.nullableString(error.stack) : null,
    artifacts: d.array(x.artifacts ?? [], artifactRef, 20),
  };
}
function ratio(value: unknown): number {
  return typeof value === "number" && value >= 0 && value <= 1
    ? value
    : d.invalid();
}
function nullableSeconds(value: unknown): number | null {
  if (value === null) return null;
  return typeof value === "number" && Number.isFinite(value) && value >= 0
    ? value
    : d.invalid();
}
export function parseTriageMetrics(value: unknown): TriageMetrics {
  const x = d.record(value);
  return {
    failures: d.integer(x.failures),
    triaged: d.integer(x.triaged),
    seen: d.integer(x.seen),
    untriagedRate: ratio(x.untriagedRate),
    triageCoverage: ratio(x.triageCoverage),
    ttrcMedianSeconds: nullableSeconds(x.ttrcMedianSeconds),
    timeToFirstViewSeconds: nullableSeconds(x.timeToFirstViewSeconds),
  };
}
function trendBucket(value: unknown): TrendBucket {
  const x = d.record(value);
  const category = d.string(x.category);
  if (!isFailureCategory(category)) return d.invalid();
  return { week: d.date(x.week), category, count: d.integer(x.count) };
}
function triageEvent(value: unknown): TriageEvent {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    type: d.string(x.type),
    actorName: d.string(x.actorName),
    occurredAt: d.date(x.occurredAt),
  };
}
function identity(value: unknown): Identity {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    canonicalTitle: d.string(x.canonicalTitle),
    canonicalFilePath: d.string(x.canonicalFilePath),
    verdict: d.string(x.verdict),
    flakyScore: x.flakyScore === null ? null : Number(x.flakyScore),
    totalRuns: d.integer(x.totalRuns),
    recentStatuses: d.array(x.recentStatuses, d.string, 30),
  };
}
const scope = (id: string) => `/v1/projects/${encodeURIComponent(id)}`;
function params(values: Record<string, string>) {
  return new URLSearchParams(
    Object.entries(values).filter(([, value]) => value !== ""),
  ).toString();
}

export class ReportsRepository {
  constructor(private api: ApiClient) {}
  async projects(cursor: string, signal: AbortSignal) {
    return d.page(
      await this.api.request(
        `/v1/projects?${params({ cursor, limit: "50" })}`,
        { signal },
      ),
      project,
      50,
    );
  }
  async runs(id: string, query: RunsQuery, signal: AbortSignal) {
    return d.page(
      await this.api.request(
        `${scope(id)}/runs?${params({ ...query, limit: "20", sort: "-startedAt" })}`,
        { signal },
      ),
      parseRun,
      20,
    );
  }
  async run(id: string, runId: string, signal: AbortSignal) {
    return parseRun(
      await this.api.request(`${scope(id)}/runs/${encodeURIComponent(runId)}`, {
        signal,
      }),
    );
  }
  async results(
    id: string,
    runId: string,
    cursor: string,
    signal: AbortSignal,
  ) {
    return d.page(
      await this.api.request(
        `${scope(id)}/runs/${encodeURIComponent(runId)}/results?${params({ cursor, limit: "50" })}`,
        { signal },
      ),
      result,
      50,
    );
  }
  async result(
    projectId: string,
    resultId: string,
    signal: AbortSignal,
  ): Promise<ResultDetail> {
    const x = d.record(
      await this.api.request(
        `${scope(projectId)}/results/${encodeURIComponent(resultId)}`,
        { signal },
      ),
    );
    return {
      result: result(x.result),
      attempts: d.array(x.attempts, attempt, 20),
      events: d.array(x.events, triageEvent, 100),
    };
  }
  // Same-origin URL of an artifact's bytes; the browser follows the server's
  // Content-Disposition (inline for images/video/text, download otherwise).
  artifactContentUrl(projectId: string, artifactId: string) {
    return `${scope(projectId)}/artifacts/${encodeURIComponent(artifactId)}/content`;
  }
  // The vendored Playwright trace viewer (/trace/, same origin) opened on an
  // artifact's bytes. The viewer needs an absolute URL for the zip; the
  // caller supplies the page origin so this module stays free of window.
  traceViewerUrl(origin: string, projectId: string, artifactId: string) {
    return `/trace/index.html?trace=${encodeURIComponent(origin + this.artifactContentUrl(projectId, artifactId))}`;
  }
  async triage(
    projectId: string,
    resultId: string,
    type: string,
    signal: AbortSignal,
  ) {
    await this.api.request(
      `${scope(projectId)}/results/${encodeURIComponent(resultId)}/triage-events`,
      { signal, body: { type, payload: {} } },
    );
  }
  async identities(
    projectId: string,
    view: string,
    cursor: string,
    signal: AbortSignal,
  ) {
    return d.page(
      await this.api.request(
        `${scope(projectId)}/identities?${params({ view, cursor, limit: "20" })}`,
        { signal },
      ),
      identity,
      20,
    );
  }
  async identity(projectId: string, identityId: string, signal: AbortSignal) {
    const x = d.record(
      await this.api.request(
        `${scope(projectId)}/identities/${encodeURIComponent(identityId)}`,
        { signal },
      ),
    );
    return identity(x);
  }
  async identityHistory(
    projectId: string,
    identityId: string,
    cursor: string,
    signal: AbortSignal,
  ) {
    return d.page(
      await this.api.request(
        `${scope(projectId)}/identities/${encodeURIComponent(identityId)}/runs?${params({ cursor, limit: "20" })}`,
        { signal },
      ),
      parseRun,
      20,
    );
  }
  // Engineering overview. `days` is the window the server aggregates over.
  async triageMetrics(projectId: string, days: number, signal: AbortSignal) {
    return parseTriageMetrics(
      await this.api.request(
        `${scope(projectId)}/metrics/triage?${params({ days: String(days) })}`,
        { signal },
      ),
    );
  }
  async categoryTrend(projectId: string, days: number, signal: AbortSignal) {
    const x = d.record(
      await this.api.request(
        `${scope(projectId)}/metrics/trend?${params({ days: String(days) })}`,
        { signal },
      ),
    );
    // 7 categories × at most ~53 weeks in a year-long window.
    return d.array(x.items, trendBucket, 7 * 53);
  }
}
