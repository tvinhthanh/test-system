import type { ApiClient } from "../../core/api/client";
import * as d from "../../core/api/decode";
import {
  readSpecializedResult,
  type SpecializedInput,
} from "./specializedReports";
import {
  levels,
  purposes,
  usageTags,
  executionModes,
  performanceTypes,
  copyTaxonomy,
  type Taxonomy,
} from "./taxonomy";

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
  taxonomy?: Taxonomy;
  preconditions: string;
  testData: string;
  audit: CaseChange[];
  auditTruncated: boolean;
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
export interface CaseChange {
  id: string;
  actor: string;
  action: "case.create" | "case.update" | "case.import";
  changedFields: string[];
  createdAt: string;
}
// A manual_evidence artifact (SPEC §6.8.2 "Artifact dùng lại") attached to
// an execution; bytes come from evidenceContentUrl, the same content route
// the failure detail uses (§11.5: the server decides inline vs download).
export interface Evidence {
  id: string;
  name: string;
  kind: string;
  contentType: string;
  sizeBytes: number;
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
  evidence: Evidence[];
  caseSnapshot: CaseSnapshot | null;
  stepResults: StepResult[];
  bugUrl: string;
  retestOf: string;
}
export interface CaseSnapshot {
  taxonomy?: Taxonomy | null;
  preconditions: string | null;
  testData: string | null;
  title: string;
  steps: Step[];
  expected: string;
  updatedAt: string;
}
export interface StepResult {
  stepIndex: number;
  status: ExecutionStatus;
  actual: string;
}
// URL checks (QĐ-14): a page opened by the server's checker. Report text
// (titles, summaries, details) comes from the checked page and is untrusted;
// components interpolate it, never render it as HTML.
export type CheckStatus = "passed" | "warning" | "failed" | "not_run";
export interface UrlCheckItem {
  key: string;
  title: string;
  status: CheckStatus;
  summary: string;
  details: string[];
}
export interface UrlCheckReport {
  url: string;
  finalUrl: string;
  status: number;
  title: string;
  durationMs: number;
  blocked: string[];
  metrics: {
    ttfbMs: number | null;
    lcpMs: number | null;
    loadMs: number | null;
    cls: number | null;
    requests: number | null;
    transferBytes: number | null;
  };
  checks: UrlCheckItem[];
}
export interface UrlCheck {
  id: string;
  environmentId: string;
  url: string;
  status: "queued" | "done" | "error";
  verdict: "" | "passed" | "warning" | "failed";
  title: string;
  finalUrl: string;
  requestedBy: string;
  checkStatuses: CheckStatus[];
  report: UrlCheckReport | null;
  error: string;
  screenshots: ("desktop" | "mobile")[];
  createdAt: string;
  finishedAt: string | null;
}
const checkStatuses = ["passed", "warning", "failed", "not_run"] as const;
const optionalNumber = (value: unknown) =>
  typeof value === "number" && Number.isFinite(value) ? value : null;
function urlCheckItem(value: unknown): UrlCheckItem {
  const x = d.record(value);
  return {
    key: d.string(x.key),
    title: d.string(x.title),
    status: oneOf(x.status, checkStatuses),
    summary: d.string(x.summary),
    details: d.array(x.details ?? [], d.string, 20),
  };
}
function urlCheckReport(value: unknown): UrlCheckReport {
  const x = d.record(value);
  const m = x.metrics ? d.record(x.metrics) : {};
  return {
    url: d.string(x.url ?? ""),
    finalUrl: d.string(x.finalUrl ?? ""),
    status: optionalNumber(x.status) ?? 0,
    title: d.string(x.title ?? ""),
    durationMs: optionalNumber(x.durationMs) ?? 0,
    blocked: d.array(x.blocked ?? [], d.string, 50),
    metrics: {
      ttfbMs: optionalNumber(m.ttfbMs),
      lcpMs: optionalNumber(m.lcpMs),
      loadMs: optionalNumber(m.loadMs),
      cls: optionalNumber(m.cls),
      requests: optionalNumber(m.requests),
      transferBytes: optionalNumber(m.transferBytes),
    },
    checks: d.array(x.checks ?? [], urlCheckItem, 20),
  };
}
function urlCheck(value: unknown): UrlCheck {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    environmentId: d.string(x.environmentId),
    url: d.string(x.url),
    status: oneOf(x.status, ["queued", "done", "error"]),
    verdict: oneOf(x.verdict, ["", "passed", "warning", "failed"]),
    title: d.string(x.title),
    finalUrl: d.string(x.finalUrl),
    requestedBy: d.string(x.requestedBy),
    checkStatuses: d.array(
      x.checkStatuses ?? [],
      (v) => oneOf(v, checkStatuses),
      20,
    ),
    report: x.report ? urlCheckReport(x.report) : null,
    error: d.string(x.error ?? ""),
    screenshots: d.array(
      x.screenshots ?? [],
      (v) => oneOf(v, ["desktop", "mobile"]),
      2,
    ),
    createdAt: d.date(x.createdAt),
    finishedAt: x.finishedAt == null ? null : d.date(x.finishedAt),
  };
}

// AI-drafted test cases (QĐ-15). Drafts come from a model that read an
// untrusted page: shown as text, and nothing is a test case until the
// tester keeps it (createTestCase).
export interface AiStatus {
  available: boolean;
  source: "" | "organization" | "server";
  provider: string;
  model: string;
  readsPages: boolean;
}
export interface CaseDraft {
  title: string;
  priority: Priority;
  preconditions: string;
  steps: Step[];
  why: string;
}
export interface CaseSuggestion {
  id: string;
  environmentId: string;
  testType: string;
  url: string;
  context: string;
  count: number;
  status: "queued" | "done" | "error";
  drafts: CaseDraft[];
  pageRead: boolean;
  model: string;
  keySource: "" | "organization" | "server";
  error: string;
  createdAt: string;
  finishedAt: string | null;
}
export interface AiModel {
  id: string;
  label: string;
  note: string;
}
// A provider whose keys the server takes (QĐ-18): how its keys begin, where
// they are made, the models it suggests and whether another id may be typed
// (Anthropic's list is the only one that is also the allowlist).
export interface AiProvider {
  id: string;
  label: string;
  console: string;
  keyHint: string;
  keyPrefixes: string[];
  models: AiModel[];
  defaultModel: string;
  anyModel: boolean;
}
export interface AiSettings {
  providers: AiProvider[];
  models: AiModel[];
  server: { available: boolean; provider: string; model: string };
  defaultModel: string;
  organization: {
    provider: string;
    model: string;
    keySource: "organization" | "server";
    keyHint: string;
    updatedBy: string;
    updatedAt: string;
  } | null;
  effective: AiStatus;
}
const keySources = ["", "organization", "server"] as const;
function draft(value: unknown): CaseDraft {
  const x = d.record(value);
  return {
    title: d.string(x.title),
    priority: oneOf(x.priority, priorities),
    preconditions: d.string(x.preconditions ?? ""),
    steps: d.array(
      x.steps ?? [],
      (v) => {
        const s = d.record(v);
        return { action: d.string(s.action), expected: d.string(s.expected) };
      },
      20,
    ),
    why: d.string(x.why ?? ""),
  };
}
function suggestion(value: unknown): CaseSuggestion {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    environmentId: d.string(x.environmentId ?? ""),
    testType: d.string(x.testType),
    url: d.string(x.url ?? ""),
    context: d.string(x.context ?? ""),
    count: d.integer(x.count),
    status: oneOf(x.status, ["queued", "done", "error"]),
    drafts: d.array(x.drafts ?? [], draft, 20),
    pageRead: bool(x.pageRead ?? false),
    model: d.string(x.model ?? ""),
    keySource: oneOf(x.keySource ?? "", keySources),
    error: d.string(x.error ?? ""),
    createdAt: d.date(x.createdAt),
    finishedAt: x.finishedAt == null ? null : d.date(x.finishedAt),
  };
}
function aiStatus(value: unknown): AiStatus {
  const x = d.record(value);
  return {
    available: bool(x.available),
    source: oneOf(x.source ?? "", keySources),
    provider: d.string(x.provider ?? "anthropic"),
    model: d.string(x.model ?? ""),
    readsPages: bool(x.readsPages ?? false),
  };
}
function aiModels(value: unknown): AiModel[] {
  return d.array(
    value,
    (v) => {
      const m = d.record(v);
      return {
        id: d.string(m.id),
        label: d.string(m.label),
        note: d.string(m.note),
      };
    },
    20,
  );
}
function aiSettings(value: unknown): AiSettings {
  const x = d.record(value);
  const server = d.record(x.server);
  const org = x.organization == null ? null : d.record(x.organization);
  const models = aiModels(x.models);
  const defaultModel = d.string(x.defaultModel);
  // A server from before providers offers Anthropic alone, as models.
  const providers =
    x.providers == null
      ? [
          {
            id: "anthropic",
            label: "Anthropic (Claude)",
            console: "https://console.anthropic.com/settings/keys",
            keyHint: "sk-ant-…",
            keyPrefixes: ["sk-ant-"],
            models,
            defaultModel,
            anyModel: false,
          },
        ]
      : d.array(
          x.providers,
          (v) => {
            const p = d.record(v);
            return {
              id: d.string(p.id),
              label: d.string(p.label),
              console: d.string(p.console),
              keyHint: d.string(p.keyHint),
              keyPrefixes: d.array(p.keyPrefixes, d.string, 10),
              models: aiModels(p.models),
              defaultModel: d.string(p.defaultModel),
              anyModel: bool(p.anyModel),
            };
          },
          20,
        );
  return {
    providers,
    models,
    server: {
      available: bool(server.available),
      provider: d.string(server.provider ?? "anthropic"),
      model: d.string(server.model ?? defaultModel),
    },
    defaultModel,
    organization: org && {
      provider: d.string(org.provider ?? "anthropic"),
      model: d.string(org.model),
      keySource: oneOf(org.keySource, ["organization", "server"]),
      keyHint: d.string(org.keyHint),
      updatedBy: d.string(org.updatedBy),
      updatedAt: d.date(org.updatedAt),
    },
    effective: aiStatus(x.effective),
  };
}

// An environment with the address of the system under test there (QĐ-14).
// baseUrl is "" until someone records it; the api only stores http(s).
export interface Environment {
  id: string;
  key: string;
  baseUrl: string;
  createdAt: string;
  updatedAt: string;
}
export interface Plan {
  cases: PlanCase[];
  id: string;
  name: string;
  environmentId: string;
  environment: Environment | null;
  testType: string; // smoke, regression, … (QĐ-15); "" when not chosen
  status: PlanStatus;
  executions: Execution[]; // latest per case; [] on lists
  executionHistory: Execution[];
  historyTruncated: boolean;
  createdAt: string;
  updatedAt: string;
}
export interface PlanCase {
  testCase: TestCase;
  assigneeUserId: string;
  assigneeEmail: string;
  assignedBy: string;
  assignedAt: string | null;
  workStatus: "not_run" | "in_progress" | "completed";
}
export interface PlanCaseUpdate {
  testCaseId: string;
  assigneeEmail?: string;
  workStatus?: "not_run" | "in_progress";
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
const bool = d.boolean;
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
function taxonomy(value: unknown): Taxonomy {
  if (value === undefined) return copyTaxonomy();
  const x = d.record(value);
  const read = <T extends string>(value: unknown, options: readonly T[]) => {
    const values = d.array(value, (v) => oneOf(v, options), options.length);
    if (new Set(values).size !== values.length) return d.invalid();
    return values;
  };
  const out: Taxonomy = {
    level: oneOf(x.level, ["", ...levels]),
    purposes: read(x.purposes, purposes),
    tags: read(x.tags, usageTags),
    executionModes: read(x.executionModes, executionModes),
    performanceTypes: read(x.performanceTypes, performanceTypes),
  };
  if (out.performanceTypes.length && !out.purposes.includes("performance"))
    return d.invalid();
  return out;
}
function testCase(value: unknown): TestCase {
  const x = d.record(value);
  return {
    preconditions: optionalString(x.preconditions),
    taxonomy: taxonomy(x.taxonomy),
    testData: optionalString(x.testData),
    audit: d.array(x.audit ?? [], caseChange, 50),
    auditTruncated:
      x.auditTruncated === undefined ? false : bool(x.auditTruncated),
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
function evidence(value: unknown): Evidence {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    name: d.string(x.name),
    kind: d.string(x.kind),
    contentType: d.string(x.contentType),
    sizeBytes: d.integer(x.sizeBytes),
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
    evidence: d.array(x.evidence ?? [], evidence, 50),
    caseSnapshot: x.caseSnapshot == null ? null : caseSnapshot(x.caseSnapshot),
    stepResults: d.array(x.stepResults ?? [], stepResult, 200),
    bugUrl: bugUrl(x.bugUrl),
    retestOf: optionalString(x.retestOf),
  };
}
function caseSnapshot(value: unknown): CaseSnapshot {
  const x = d.record(value);
  return {
    taxonomy: x.taxonomy == null ? null : taxonomy(x.taxonomy),
    preconditions: x.preconditions == null ? null : d.string(x.preconditions),
    testData: x.testData == null ? null : d.string(x.testData),
    title: d.string(x.title),
    steps: d.array(x.steps, step, 200),
    expected: d.string(x.expected),
    updatedAt: d.date(x.updatedAt),
  };
}
function caseChange(value: unknown): CaseChange {
  const x = d.record(value);
  return {
    id: d.string(x.id),
    actor: d.string(x.actor),
    action: oneOf(x.action, ["case.create", "case.update", "case.import"]),
    changedFields: d.array(x.changedFields, d.string, 30),
    createdAt: d.date(x.createdAt),
  };
}
function bugUrl(value: unknown): string {
  const text = optionalString(value);
  if (!text) return text;
  try {
    const url = new URL(text);
    if (
      !["http:", "https:"].includes(url.protocol) ||
      !url.hostname ||
      url.username ||
      url.password
    )
      return d.invalid();
  } catch {
    return d.invalid();
  }
  return text;
}
function stepResult(value: unknown): StepResult {
  const x = d.record(value);
  const index = d.integer(x.stepIndex);
  if (index < 0 || index >= 200) return d.invalid();
  return {
    stepIndex: index,
    status: oneOf(x.status, executionStatuses),
    actual: d.string(x.actual),
  };
}
function plan(value: unknown): Plan {
  const x = d.record(value);
  return {
    cases: d.array(x.cases ?? [], planCase, 5000),
    id: d.string(x.id),
    name: d.string(x.name),
    environmentId: d.string(x.environmentId),
    environment:
      x.environment === null || x.environment === undefined
        ? null
        : environment(x.environment),
    testType: d.string(x.testType ?? ""),
    status: oneOf(x.status, ["draft", "active", "closed"]),
    executions:
      x.executions === undefined ? [] : d.array(x.executions, execution, 5000),
    executionHistory: d.array(x.executionHistory ?? [], execution, 200),
    historyTruncated:
      x.historyTruncated === undefined ? false : bool(x.historyTruncated),
    createdAt: d.date(x.createdAt),
    updatedAt: d.date(x.updatedAt ?? x.createdAt),
  };
}
function environment(value: unknown): Environment {
  const x = d.record(value);
  const baseUrl = d.string(x.baseUrl);
  // Rendered as a link: anything but http(s) would be a script vector.
  if (baseUrl !== "" && !/^https?:\/\//i.test(baseUrl)) return d.invalid();
  return {
    id: d.string(x.id),
    key: d.string(x.key),
    baseUrl,
    createdAt: d.date(x.createdAt),
    updatedAt: d.date(x.updatedAt ?? x.createdAt),
  };
}
function planCase(value: unknown): PlanCase {
  const x = d.record(value);
  return {
    testCase: testCase(x.testCase),
    assigneeUserId: d.string(x.assigneeUserId),
    assigneeEmail: d.string(x.assigneeEmail),
    assignedBy: d.string(x.assignedBy),
    assignedAt: x.assignedAt == null ? null : d.date(x.assignedAt),
    workStatus: oneOf(x.workStatus, ["not_run", "in_progress", "completed"]),
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
  level?: string;
  purpose?: string;
  tag?: string;
  executionMode?: string;
  performanceType?: string;
  suite?: string;
  automation?: AutomationStatus | "";
  critical?: "" | "true" | "false";
  q?: string;
}
export interface TestCaseInput {
  taxonomy?: Taxonomy;
  preconditions?: string;
  testData?: string;
  expectedUpdatedAt?: string;
  suiteId?: string;
  externalKey?: string;
  title?: string;
  steps?: Step[];
  expected?: string;
  priority?: Priority;
  isCritical?: boolean;
  automationStatus?: AutomationStatus;
}
export type BulkCasePatch = Pick<
  TestCaseInput,
  "priority" | "isCritical" | "suiteId" | "automationStatus" | "taxonomy"
>;
export interface BulkCaseResult {
  id: string;
  updated: boolean;
  code: string;
}
export interface CaseExport {
  csv: string;
  count: number;
  exportedAt: string;
}

// ---- ROI (SPEC §6.8.3, QĐ-13) ----
// Hours measured from triage events against a measured §5.4 baseline;
// `savedHours` is null until one exists ("not enough data"), never a
// default. Money is the reader's rate, applied on the page, never sent.
export type BaselineKind = "retrospective" | "prospective";
export interface Baseline {
  id: string;
  kind: BaselineKind;
  failures: number;
  medianMinutes: number;
  p90Minutes: number | null;
  rerunPassRate: number | null;
  flakyInvestigationMinutes: number | null;
  periodFrom: string;
  periodTo: string;
  note: string;
  enteredBy: string;
  enteredAt: string;
}
export interface BaselineInput {
  kind: BaselineKind;
  failures: number;
  medianMinutes: number;
  p90Minutes?: number;
  rerunPassRate?: number;
  flakyInvestigationMinutes?: number;
  periodFrom: string;
  periodTo: string;
  note?: string;
}
export interface Roi {
  month: string;
  failures: number;
  triaged: number;
  triageCoverage: number;
  untriagedRate: number;
  ttrcMedianMinutes: number | null;
  baseline: Baseline | null;
  savedHours: number | null;
  flakyHours: number;
  flakyRetryHours: number;
  markedFlaky: number;
  investigationCounted: boolean;
  fewTriaged: boolean;
  minTriaged: number;
}
// Any finite number (savedHours may be negative: slower than before).
function num(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value)
    ? value
    : d.invalid();
}
const nullableNum = (value: unknown) => (value === null ? null : num(value));
function baseline(value: unknown): Baseline {
  const x = d.record(value);
  const kind = d.string(x.kind);
  if (kind !== "retrospective" && kind !== "prospective") return d.invalid();
  return {
    id: d.string(x.id),
    kind,
    failures: d.integer(x.failures),
    medianMinutes: num(x.medianMinutes),
    p90Minutes: nullableNum(x.p90Minutes),
    rerunPassRate: nullableNum(x.rerunPassRate),
    flakyInvestigationMinutes: nullableNum(x.flakyInvestigationMinutes),
    periodFrom: d.string(x.periodFrom),
    periodTo: d.string(x.periodTo),
    note: d.string(x.note),
    enteredBy: d.string(x.enteredBy),
    enteredAt: d.date(x.enteredAt),
  };
}
function roi(value: unknown): Roi {
  const x = d.record(value);
  return {
    month: d.string(x.month),
    failures: d.integer(x.failures),
    triaged: d.integer(x.triaged),
    triageCoverage: num(x.triageCoverage),
    untriagedRate: num(x.untriagedRate),
    ttrcMedianMinutes: nullableNum(x.ttrcMedianMinutes),
    baseline: x.baseline === null ? null : baseline(x.baseline),
    savedHours: nullableNum(x.savedHours),
    flakyHours: num(x.flakyHours),
    flakyRetryHours: num(x.flakyRetryHours),
    markedFlaky: d.integer(x.markedFlaky),
    investigationCounted: d.boolean(x.investigationCounted),
    fewTriaged: d.boolean(x.fewTriaged),
    minTriaged: d.integer(x.minTriaged),
  };
}

export class TestmgmtRepository {
  async specializedResults(
    project: string,
    filter: Record<string, string>,
    signal: AbortSignal,
  ) {
    const x = d.record(
      await this.api.request(
        `${scope(project)}/specialized-results?${params(filter)}`,
        { signal },
      ),
    );
    return {
      items: d.array(x.items, readSpecializedResult, 50),
      nextCursor: d.string(x.nextCursor),
    };
  }
  async specializedResult(project: string, id: string, signal: AbortSignal) {
    return readSpecializedResult(
      await this.api.request(
        `${scope(project)}/specialized-results/${enc(id)}`,
        { signal },
      ),
    );
  }
  async createSpecializedResult(
    project: string,
    input: SpecializedInput,
    signal: AbortSignal,
  ) {
    return readSpecializedResult(
      await this.api.request(`${scope(project)}/specialized-results`, {
        body: input,
        signal,
        timeoutMs: 60000,
      }),
    );
  }
  specializedOriginalUrl(project: string, id: string) {
    return `${scope(project)}/specialized-results/${enc(id)}/original`;
  }
  constructor(private api: ApiClient) {}
  async bulkUpdateCases(
    projectId: string,
    targets: { id: string; expectedUpdatedAt: string }[],
    patch: BulkCasePatch,
    signal: AbortSignal,
  ): Promise<BulkCaseResult[]> {
    const x = d.record(
      await this.api.request(`${scope(projectId)}/test-cases/bulk`, {
        body: { items: targets, patch },
        signal,
      }),
    );
    const results = d.array(
      x.items,
      (value) => {
        const item = d.record(value);
        return {
          id: d.string(item.id),
          updated: d.boolean(item.updated),
          code: oneOf(item.code, [
            "",
            "CONFLICT",
            "NOT_FOUND",
            "INVALID_PAYLOAD",
            "INTERNAL",
          ]),
        };
      },
      100,
    );
    if (
      results.length !== targets.length ||
      results.some(
        (item, index) =>
          item.id !== targets[index]?.id || item.updated !== (item.code === ""),
      )
    )
      return d.invalid();
    return results;
  }
  async exportCases(
    projectId: string,
    filter: CaseFilter,
    ids: string[] | undefined,
    signal: AbortSignal,
  ): Promise<CaseExport> {
    const query = new URLSearchParams(
      params({
        q: filter.q ?? "",
        suite: filter.suite ?? "",
        automation: filter.automation ?? "",
        critical: filter.critical ?? "",
        level: filter.level ?? "",
        purpose: filter.purpose ?? "",
        tag: filter.tag ?? "",
        executionMode: filter.executionMode ?? "",
        performanceType: filter.performanceType ?? "",
      }),
    );
    if (ids !== undefined) query.set("ids", ids.join(","));
    const x = d.record(
      await this.api.request(`${scope(projectId)}/test-cases/export?${query}`, {
        signal,
        timeoutMs: 60000,
      }),
    );
    const count = d.integer(x.count);
    if (count < 0 || count > 5000) return d.invalid();
    return {
      csv: d.string(x.csv),
      count,
      exportedAt: d.date(x.exportedAt),
    };
  }

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
          level: f.level ?? "",
          purpose: f.purpose ?? "",
          tag: f.tag ?? "",
          executionMode: f.executionMode ?? "",
          performanceType: f.performanceType ?? "",
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
  async createPlan(
    projectId: string,
    name: string,
    signal: AbortSignal,
    caseIds?: string[],
    environmentId = "",
    testType = "",
  ) {
    return plan(
      await this.api.request(`${scope(projectId)}/test-plans`, {
        body: {
          name,
          ...(caseIds === undefined ? {} : { caseIds }),
          ...(environmentId ? { environmentId } : {}),
          ...(testType ? { testType } : {}),
        },
        signal,
      }),
    );
  }
  async setPlanEnvironment(
    projectId: string,
    id: string,
    environmentId: string,
    signal: AbortSignal,
  ) {
    return plan(
      await this.api.request(`${scope(projectId)}/test-plans/${enc(id)}`, {
        method: "PATCH",
        body: { environmentId },
        signal,
      }),
    );
  }
  async environments(projectId: string, signal: AbortSignal) {
    return items(
      await this.api.request(`${scope(projectId)}/environments`, { signal }),
      environment,
      500,
    );
  }
  async createEnvironment(
    projectId: string,
    input: { key: string; baseUrl: string },
    signal: AbortSignal,
  ) {
    return environment(
      await this.api.request(`${scope(projectId)}/environments`, {
        body: input,
        signal,
      }),
    );
  }
  async aiStatus(projectId: string, signal: AbortSignal) {
    return aiStatus(
      await this.api.request(`${scope(projectId)}/ai-status`, { signal }),
    );
  }
  async createSuggestion(
    projectId: string,
    input: {
      testType: string;
      environmentId: string;
      url: string;
      context: string;
      count: number;
    },
    signal: AbortSignal,
  ) {
    return suggestion(
      await this.api.request(`${scope(projectId)}/case-suggestions`, {
        body: {
          testType: input.testType,
          count: input.count,
          ...(input.environmentId
            ? { environmentId: input.environmentId }
            : {}),
          ...(input.url ? { url: input.url } : {}),
          ...(input.context ? { context: input.context } : {}),
        },
        signal,
      }),
    );
  }
  async suggestion(projectId: string, id: string, signal: AbortSignal) {
    return suggestion(
      await this.api.request(
        `${scope(projectId)}/case-suggestions/${enc(id)}`,
        { signal },
      ),
    );
  }
  async aiSettings(orgId: string, signal: AbortSignal) {
    return aiSettings(
      await this.api.request(`/v1/organizations/${enc(orgId)}/ai-settings`, {
        signal,
      }),
    );
  }
  async saveAiSettings(
    orgId: string,
    input: {
      provider: string;
      model: string;
      keySource: "organization" | "server";
      apiKey: string;
    },
    signal: AbortSignal,
  ) {
    return aiSettings(
      await this.api.request(`/v1/organizations/${enc(orgId)}/ai-settings`, {
        method: "POST",
        body: {
          provider: input.provider,
          model: input.model,
          keySource: input.keySource,
          ...(input.keySource === "organization" && input.apiKey
            ? { apiKey: input.apiKey }
            : {}),
        },
        signal,
      }),
    );
  }
  async deleteAiSettings(orgId: string, signal: AbortSignal) {
    return aiSettings(
      await this.api.request(`/v1/organizations/${enc(orgId)}/ai-settings`, {
        method: "DELETE",
        signal,
      }),
    );
  }
  async urlChecks(projectId: string, signal: AbortSignal) {
    const x = d.record(
      await this.api.request(`${scope(projectId)}/url-checks`, { signal }),
    );
    return {
      enabled: x.enabled === true,
      items: d.array(x.items, urlCheck, 100),
    };
  }
  async urlCheck(projectId: string, id: string, signal: AbortSignal) {
    return urlCheck(
      await this.api.request(`${scope(projectId)}/url-checks/${enc(id)}`, {
        signal,
      }),
    );
  }
  async createUrlCheck(
    projectId: string,
    input: { url: string; environmentId: string },
    signal: AbortSignal,
  ) {
    return urlCheck(
      await this.api.request(`${scope(projectId)}/url-checks`, {
        body: {
          ...(input.url ? { url: input.url } : {}),
          ...(input.environmentId
            ? { environmentId: input.environmentId }
            : {}),
        },
        signal,
      }),
    );
  }
  // An <img> source, not a request: same-origin, the session cookie rides
  // along, and the api serves only JPEG bytes there.
  urlCheckScreenshot(
    projectId: string,
    id: string,
    which: "desktop" | "mobile",
  ) {
    return `${scope(projectId)}/url-checks/${enc(id)}/screenshots/${which}`;
  }
  async setEnvironmentUrl(
    projectId: string,
    id: string,
    baseUrl: string,
    signal: AbortSignal,
  ) {
    return environment(
      await this.api.request(`${scope(projectId)}/environments/${enc(id)}`, {
        method: "PATCH",
        body: { baseUrl },
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
  async updatePlan(
    projectId: string,
    id: string,
    input: {
      caseIds?: string[];
      caseUpdate?: PlanCaseUpdate;
      expectedUpdatedAt: string;
    },
    signal: AbortSignal,
  ) {
    return plan(
      await this.api.request(`${scope(projectId)}/test-plans/${enc(id)}`, {
        method: "PATCH",
        body: input,
        signal,
      }),
    );
  }
  async recordExecution(
    projectId: string,
    planId: string,
    input: {
      testCaseId: string;
      status: ExecutionStatus;
      note?: string;
      stepResults?: StepResult[];
      bugUrl?: string;
      retestOf?: string;
      caseUpdatedAt?: string;
    },
    signal: AbortSignal,
  ) {
    return execution(
      await this.api.request(
        `${scope(projectId)}/test-plans/${enc(planId)}/executions`,
        { body: input, signal },
      ),
    );
  }
  // One file per call, as multipart; the server sniffs the bytes against
  // the File.type it is told, so a mislabelled file is a 400, not a stored
  // lie. A file has its own timeout: 20 MiB on a slow link is not 15 s.
  async attachEvidence(
    projectId: string,
    planId: string,
    executionId: string,
    file: File,
    signal: AbortSignal,
  ) {
    const body = new FormData();
    body.append("file", file, file.name);
    return evidence(
      await this.api.request(
        `${scope(projectId)}/test-plans/${enc(planId)}/executions/${enc(executionId)}/evidence`,
        { body, signal, timeoutMs: 120000 },
      ),
    );
  }
  // Same-origin URL of an evidence file's bytes (the content route of the
  // reports repository; one route serves every artifact kind).
  evidenceContentUrl(projectId: string, artifactId: string) {
    return `${scope(projectId)}/artifacts/${enc(artifactId)}/content`;
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

  // ---- ROI ----
  async roi(projectId: string, month: string, signal: AbortSignal) {
    return roi(
      await this.api.request(`${scope(projectId)}/roi?${params({ month })}`, {
        signal,
      }),
    );
  }
  async baselines(projectId: string, signal: AbortSignal) {
    return items(
      await this.api.request(`${scope(projectId)}/roi/baselines`, { signal }),
      baseline,
      100,
    );
  }
  async addBaseline(
    projectId: string,
    input: BaselineInput,
    signal: AbortSignal,
  ) {
    return baseline(
      await this.api.request(`${scope(projectId)}/roi/baselines`, {
        signal,
        body: input,
      }),
    );
  }
}
