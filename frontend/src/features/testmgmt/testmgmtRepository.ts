import type { ApiClient } from "../../core/api/client";
import * as d from "../../core/api/decode";

// Test Management Plane (SPEC §6.8, P2). The repository owns every path and
// the field-by-field decoding of the contract in API_CONTRACT.md ("Test
// Management") / backend/deploy/openapi.yaml (tag testmgmt). Steps and titles
// are untrusted text; components interpolate them, never render HTML.

import {
  automationStatuses,
  executionStatuses,
  priorities,
  requirementStatuses,
  type AutomationStatus,
  type ExecutionStatus,
  type GateVerdict,
  type PlanStatus,
  type Priority,
  type ReleaseStatus,
  type RequirementStatus,
} from "./enums";
export type {
  AutomationStatus,
  ExecutionStatus,
  GateVerdict,
  PlanStatus,
  Priority,
  ReleaseStatus,
  RequirementStatus,
};

export interface Suite {
  id: string;
  parentId: string;
  name: string;
  cases: number;
}
export interface Requirement {
  id: string;
  externalKey: string;
  title: string;
  priority: Priority;
  status: RequirementStatus;
  coveredBy: number;
  createdAt: string;
}
export interface Step {
  action: string;
  expected: string;
}
export interface AutomationLink {
  id: string;
  testCaseId: string;
  stableTestId: string;
  linkType: "exact" | "partial" | "superset";
  createdBy: string;
  createdAt: string;
}
export interface TestCase {
  id: string;
  suiteId: string;
  suiteName: string;
  externalKey: string;
  title: string;
  steps: Step[];
  expected: string;
  priority: Priority;
  isCritical: boolean;
  automationStatus: AutomationStatus;
  ownerId: string;
  links: AutomationLink[]; // [] on lists
  requirements: string[]; // requirement ids; [] on lists
  createdAt: string;
  updatedAt: string;
}
export interface Execution {
  id: string;
  planId: string;
  testCaseId: string;
  caseTitle: string;
  executedBy: string;
  status: ExecutionStatus;
  note: string;
  executedAt: string;
}
export interface Plan {
  id: string;
  name: string;
  environmentId: string;
  status: PlanStatus;
  executions: Execution[]; // latest per case; [] on lists
  createdAt: string;
}
export interface GateRule {
  id: string;
  level: "blocked" | "warn";
  passed: boolean;
  applies: boolean;
  summary: string;
  details: Record<string, unknown>;
}
export interface GateResult {
  verdict: GateVerdict;
  evaluatedAt: string;
  runId: string;
  branch: string;
  rules: GateRule[];
  p0Coverage: number | null;
}
export interface Release {
  id: string;
  version: string;
  branch: string;
  status: ReleaseStatus;
  decidedBy: string;
  decidedAt: string | null;
  decisionNote: string;
  gate: GateResult | null;
  evaluatedAt: string | null;
  createdAt: string;
}
export interface ImportReport {
  created: number;
  updated: number;
  skipped: number;
  suites: number;
  requirements: number;
  errors: string[];
}

// ---- decoders ---------------------------------------------------------------

function oneOf<T extends string>(value: unknown, allowed: readonly T[]): T {
  const s = d.string(value);
  return (allowed as readonly string[]).includes(s) ? (s as T) : d.invalid();
}
const priority = (v: unknown) => oneOf(v, priorities);
const bool = (v: unknown) => (typeof v === "boolean" ? v : d.invalid());
const optionalString = (v: unknown) => (v === undefined ? "" : d.string(v));

function suite(value: unknown): Suite {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    parentId: d.string(x.parentId),
    name: d.string(x.name),
    cases: d.integer(x.cases),
  };
}
function requirement(value: unknown): Requirement {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    externalKey: d.string(x.externalKey),
    title: d.string(x.title),
    priority: priority(x.priority),
    status: oneOf(x.status, requirementStatuses),
    coveredBy: d.integer(x.coveredBy),
    createdAt: d.date(x.createdAt),
  };
}
function step(value: unknown): Step {
  const x = d.record(value);
  return { action: d.string(x.action), expected: d.string(x.expected) };
}
function link(value: unknown): AutomationLink {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    testCaseId: d.string(x.testCaseId),
    stableTestId: d.string(x.stableTestId),
    linkType: oneOf(x.linkType, ["exact", "partial", "superset"]),
    createdBy: d.string(x.createdBy),
    createdAt: d.date(x.createdAt),
  };
}
function testCase(value: unknown): TestCase {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    suiteId: d.string(x.suiteId),
    suiteName: d.string(x.suiteName),
    externalKey: d.string(x.externalKey),
    title: d.string(x.title),
    steps: d.array(x.steps, step, 200),
    expected: d.string(x.expected),
    priority: priority(x.priority),
    isCritical: bool(x.isCritical),
    automationStatus: oneOf(x.automationStatus, automationStatuses),
    ownerId: d.string(x.ownerId),
    links: x.links === undefined ? [] : d.array(x.links, link, 100),
    requirements:
      x.requirements === undefined
        ? []
        : d.array(x.requirements, d.string, 500),
    createdAt: d.date(x.createdAt),
    updatedAt: d.date(x.updatedAt),
  };
}
function execution(value: unknown): Execution {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    planId: d.string(x.planId),
    testCaseId: d.string(x.testCaseId),
    caseTitle: d.string(x.caseTitle),
    executedBy: d.string(x.executedBy),
    status: oneOf(x.status, executionStatuses),
    note: d.string(x.note),
    executedAt: d.date(x.executedAt),
  };
}
function plan(value: unknown): Plan {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    name: d.string(x.name),
    environmentId: d.string(x.environmentId),
    status: oneOf(x.status, ["draft", "active", "closed"]),
    executions:
      x.executions === undefined ? [] : d.array(x.executions, execution, 5000),
    createdAt: d.date(x.createdAt),
  };
}
function gateRule(value: unknown): GateRule {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    level: oneOf(x.level, ["blocked", "warn"]),
    passed: bool(x.passed),
    applies: bool(x.applies),
    summary: d.string(x.summary),
    details: x.details === undefined ? {} : d.record(x.details),
  };
}
function gate(value: unknown): GateResult {
  const x = d.record(value);
  const cov = x.p0Coverage;
  return {
    verdict: oneOf(x.verdict, ["approved", "warn", "blocked", "no_evidence"]),
    evaluatedAt: d.date(x.evaluatedAt),
    runId: optionalString(x.runId),
    branch: optionalString(x.branch),
    rules: d.array(x.rules, gateRule, 20),
    p0Coverage:
      cov === undefined || cov === null
        ? null
        : typeof cov === "number"
          ? cov
          : d.invalid(),
  };
}
function release(value: unknown): Release {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    version: d.string(x.version),
    branch: d.string(x.branch),
    status: oneOf(x.status, ["open", "blocked", "approved", "shipped"]),
    decidedBy: d.string(x.decidedBy),
    decidedAt: x.decidedAt === null ? null : d.date(x.decidedAt),
    decisionNote: d.string(x.decisionNote),
    gate: x.gate === null || x.gate === undefined ? null : gate(x.gate),
    evaluatedAt: x.evaluatedAt === null ? null : d.date(x.evaluatedAt),
    createdAt: d.date(x.createdAt),
  };
}
function importReport(value: unknown): ImportReport {
  const x = d.record(value);
  return {
    created: d.integer(x.created),
    updated: d.integer(x.updated),
    skipped: d.integer(x.skipped),
    suites: d.integer(x.suites),
    requirements: d.integer(x.requirements),
    errors: d.array(x.errors, d.string, 5000),
  };
}
function items<T>(value: unknown, parse: (v: unknown) => T, max: number) {
  return d.array(d.record(value).items, parse, max);
}

const scope = (id: string) => `/v1/projects/${encodeURIComponent(id)}`;
const enc = encodeURIComponent;
function params(values: Record<string, string>) {
  return new URLSearchParams(
    Object.entries(values).filter(([, v]) => v !== ""),
  ).toString();
}

export interface CaseFilter {
  suite?: string;
  automation?: AutomationStatus | "";
  critical?: "" | "true" | "false";
  q?: string;
}
export interface TestCaseInput {
  suiteId?: string;
  externalKey?: string;
  title?: string;
  steps?: Step[];
  expected?: string;
  priority?: Priority;
  isCritical?: boolean;
  automationStatus?: AutomationStatus;
}

export class TestmgmtRepository {
  constructor(private api: ApiClient) {}

  // ---- suites & requirements ----
  async suites(projectId: string, signal: AbortSignal) {
    return items(
      await this.api.request(`${scope(projectId)}/suites`, { signal }),
      suite,
      500,
    );
  }
  async createSuite(projectId: string, name: string, signal: AbortSignal) {
    return suite(
      await this.api.request(`${scope(projectId)}/suites`, {
        body: { name },
        signal,
      }),
    );
  }
  async requirements(projectId: string, signal: AbortSignal) {
    return items(
      await this.api.request(
        `${scope(projectId)}/requirements?${params({ limit: "500" })}`,
        { signal },
      ),
      requirement,
      500,
    );
  }
  async createRequirement(
    projectId: string,
    input: { title: string; externalKey?: string; priority?: Priority },
    signal: AbortSignal,
  ) {
    return requirement(
      await this.api.request(`${scope(projectId)}/requirements`, {
        body: input,
        signal,
      }),
    );
  }
  async updateRequirement(
    projectId: string,
    id: string,
    patch: { title?: string; priority?: Priority; status?: RequirementStatus },
    signal: AbortSignal,
  ) {
    return requirement(
      await this.api.request(`${scope(projectId)}/requirements/${enc(id)}`, {
        method: "PATCH",
        body: patch,
        signal,
      }),
    );
  }

  // ---- test cases ----
  async testCases(projectId: string, f: CaseFilter, signal: AbortSignal) {
    return items(
      await this.api.request(
        `${scope(projectId)}/test-cases?${params({
          suite: f.suite ?? "",
          automation: f.automation ?? "",
          critical: f.critical ?? "",
          q: f.q ?? "",
          limit: "500",
        })}`,
        { signal },
      ),
      testCase,
      500,
    );
  }
  async testCase(projectId: string, id: string, signal: AbortSignal) {
    return testCase(
      await this.api.request(`${scope(projectId)}/test-cases/${enc(id)}`, {
        signal,
      }),
    );
  }
  async createTestCase(
    projectId: string,
    input: TestCaseInput,
    signal: AbortSignal,
  ) {
    return testCase(
      await this.api.request(`${scope(projectId)}/test-cases`, {
        body: input,
        signal,
      }),
    );
  }
  async updateTestCase(
    projectId: string,
    id: string,
    patch: TestCaseInput,
    signal: AbortSignal,
  ) {
    return testCase(
      await this.api.request(`${scope(projectId)}/test-cases/${enc(id)}`, {
        method: "PATCH",
        body: patch,
        signal,
      }),
    );
  }
  async link(
    projectId: string,
    testCaseId: string,
    stableTestId: string,
    signal: AbortSignal,
  ) {
    return link(
      await this.api.request(
        `${scope(projectId)}/test-cases/${enc(testCaseId)}/links`,
        { body: { stableTestId, linkType: "exact" }, signal },
      ),
    );
  }
  async unlink(
    projectId: string,
    testCaseId: string,
    linkId: string,
    signal: AbortSignal,
  ) {
    await this.api.request(
      `${scope(projectId)}/test-cases/${enc(testCaseId)}/links/${enc(linkId)}`,
      { method: "DELETE", signal },
    );
  }
  async cover(
    projectId: string,
    testCaseId: string,
    requirementId: string,
    signal: AbortSignal,
  ) {
    await this.api.request(
      `${scope(projectId)}/test-cases/${enc(testCaseId)}/requirements/${enc(requirementId)}`,
      { method: "POST", body: {}, signal },
    );
  }
  async uncover(
    projectId: string,
    testCaseId: string,
    requirementId: string,
    signal: AbortSignal,
  ) {
    await this.api.request(
      `${scope(projectId)}/test-cases/${enc(testCaseId)}/requirements/${enc(requirementId)}`,
      { method: "DELETE", signal },
    );
  }
  async importCsv(projectId: string, csv: string, signal: AbortSignal) {
    return importReport(
      await this.api.request(`${scope(projectId)}/test-cases/import`, {
        body: { csv },
        signal,
      }),
    );
  }

  // ---- plans ----
  async plans(projectId: string, signal: AbortSignal) {
    return items(
      await this.api.request(`${scope(projectId)}/test-plans`, { signal }),
      plan,
      200,
    );
  }
  async plan(projectId: string, id: string, signal: AbortSignal) {
    return plan(
      await this.api.request(`${scope(projectId)}/test-plans/${enc(id)}`, {
        signal,
      }),
    );
  }
  async createPlan(projectId: string, name: string, signal: AbortSignal) {
    return plan(
      await this.api.request(`${scope(projectId)}/test-plans`, {
        body: { name },
        signal,
      }),
    );
  }
  async setPlanStatus(
    projectId: string,
    id: string,
    status: PlanStatus,
    signal: AbortSignal,
  ) {
    return plan(
      await this.api.request(`${scope(projectId)}/test-plans/${enc(id)}`, {
        method: "PATCH",
        body: { status },
        signal,
      }),
    );
  }
  async recordExecution(
    projectId: string,
    planId: string,
    input: { testCaseId: string; status: ExecutionStatus; note?: string },
    signal: AbortSignal,
  ) {
    return execution(
      await this.api.request(
        `${scope(projectId)}/test-plans/${enc(planId)}/executions`,
        { body: input, signal },
      ),
    );
  }

  // ---- releases ----
  async releases(projectId: string, signal: AbortSignal) {
    return items(
      await this.api.request(`${scope(projectId)}/releases`, { signal }),
      release,
      200,
    );
  }
  async release(projectId: string, id: string, signal: AbortSignal) {
    return release(
      await this.api.request(`${scope(projectId)}/releases/${enc(id)}`, {
        signal,
      }),
    );
  }
  async createRelease(
    projectId: string,
    input: { version: string; branch?: string },
    signal: AbortSignal,
  ) {
    return release(
      await this.api.request(`${scope(projectId)}/releases`, {
        body: input,
        signal,
      }),
    );
  }
  async evaluate(projectId: string, id: string, signal: AbortSignal) {
    return release(
      await this.api.request(
        `${scope(projectId)}/releases/${enc(id)}/evaluate`,
        { body: {}, signal },
      ),
    );
  }
  async decide(
    projectId: string,
    id: string,
    input: { status: Exclude<ReleaseStatus, "open">; note?: string },
    signal: AbortSignal,
  ) {
    return release(
      await this.api.request(
        `${scope(projectId)}/releases/${enc(id)}/decision`,
        { body: input, signal },
      ),
    );
  }
  // The CI URL a deploy job polls with a project:read token (§6.8.4 #2);
  // shown on the release page so nobody has to guess it.
  ciGateUrl(origin: string, releaseId: string) {
    return `${origin}/v1/releases/${enc(releaseId)}/gate`;
  }
}
