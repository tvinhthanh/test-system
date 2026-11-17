import * as d from "../../core/api/decode";

export const reportKinds = [
  "performance",
  "security",
  "accessibility",
  "compatibility",
  "recovery",
  "usability",
] as const;
export const reportOutcomes = [
  "passed",
  "failed",
  "inconclusive",
  "not_run",
  "not_applicable",
  "import_error",
] as const;
export type ReportKind = (typeof reportKinds)[number];
export type ReportData = Record<string, unknown>;
export interface ReportField {
  key: string;
  label: string;
  type?: "number" | "boolean" | "date" | "lines" | "text";
  options?: readonly string[];
  fields?: ReportField[];
  repeat?: boolean;
}
const findingFields: ReportField[] = [
  { key: "standard", label: "Standard / assessment scope" },
  { key: "standardVersion", label: "Standard version" },
  {
    key: "method",
    label: "Assessment method",
    options: ["manual", "automated", "combined"],
  },
  { key: "coverage", label: "Checked scope", type: "lines" },
  {
    key: "assessmentComplete",
    label: "Assessment complete for the recorded scope",
    type: "boolean",
  },
  {
    key: "findings",
    label: "Findings",
    repeat: true,
    fields: [
      { key: "id", label: "Finding ID" },
      { key: "severity", label: "Source severity" },
      { key: "location", label: "Location" },
      { key: "summary", label: "Finding" },
      { key: "evidence", label: "Evidence", type: "text" },
      {
        key: "status",
        label: "Finding status",
        options: ["open", "resolved", "accepted_risk", "not_retested"],
      },
      { key: "retested", label: "Resolution retested", type: "boolean" },
    ],
  },
];
export const reportFields: Record<ReportKind, ReportField[]> = {
  performance: [
    {
      key: "subtype",
      label: "Performance type",
      options: ["load", "stress", "spike", "soak", "volume", "scalability"],
    },
    { key: "scenario", label: "Scenario", type: "text" },
    { key: "loadConfiguration", label: "Load configuration", type: "text" },
    {
      key: "dataConfiguration",
      label: "Data / volume configuration",
      type: "text",
    },
    {
      key: "resources",
      label: "Compared resource configurations",
      type: "text",
    },
    { key: "durationSeconds", label: "Duration (s)", type: "number" },
    { key: "windowSeconds", label: "Measurement window (s)", type: "number" },
    { key: "p95Ms", label: "Latency p95 (ms)", type: "number" },
    { key: "p99Ms", label: "Latency p99 (ms)", type: "number" },
    {
      key: "throughputPerSecond",
      label: "Throughput (requests/s)",
      type: "number",
    },
    { key: "errorRatePercent", label: "Error rate (%)", type: "number" },
    {
      key: "thresholdsRecordedAt",
      label: "Thresholds declared at",
      type: "date",
    },
    {
      key: "thresholds",
      label: "Recorded thresholds",
      fields: [
        { key: "p95MaxMs", label: "Maximum p95 (ms)", type: "number" },
        { key: "p99MaxMs", label: "Maximum p99 (ms)", type: "number" },
        {
          key: "throughputMinPerSecond",
          label: "Minimum throughput (requests/s)",
          type: "number",
        },
        {
          key: "errorRateMaxPercent",
          label: "Maximum error rate (%)",
          type: "number",
        },
      ],
    },
  ],
  security: findingFields,
  accessibility: findingFields,
  compatibility: [
    {
      key: "matrix",
      label: "Platform matrix",
      repeat: true,
      fields: [
        { key: "browser", label: "Browser" },
        { key: "browserVersion", label: "Browser version" },
        { key: "os", label: "Operating system" },
        { key: "osVersion", label: "OS version" },
        { key: "device", label: "Device" },
        {
          key: "status",
          label: "Platform result",
          options: ["not_run", "passed", "failed", "blocked", "skipped"],
        },
        { key: "evidence", label: "Platform evidence", type: "text" },
      ],
    },
  ],
  recovery: [
    { key: "fault", label: "Injected fault", type: "text" },
    { key: "initialConditions", label: "Initial conditions", type: "text" },
    { key: "recoveryObjective", label: "Recovery objective", type: "text" },
    {
      key: "targetRecoverySeconds",
      label: "Target recovery time (s)",
      type: "number",
    },
    {
      key: "actualRecoverySeconds",
      label: "Actual recovery time (s)",
      type: "number",
    },
    {
      key: "allowedDataLossBytes",
      label: "Allowed data loss (bytes)",
      type: "number",
    },
    {
      key: "actualDataLossBytes",
      label: "Actual data loss (bytes)",
      type: "number",
    },
    { key: "evidence", label: "Recovery evidence", type: "text" },
  ],
  usability: [
    { key: "method", label: "Evaluation method", type: "text" },
    { key: "participants", label: "Participants", type: "number" },
    {
      key: "tasks",
      label: "Tasks",
      repeat: true,
      fields: [
        { key: "task", label: "Task" },
        { key: "successCriterion", label: "Success criterion" },
        { key: "observation", label: "Observation", type: "text" },
        { key: "conclusion", label: "Task conclusion", type: "text" },
        {
          key: "status",
          label: "Task assessment",
          options: ["inconclusive", "passed", "failed"],
        },
      ],
    },
    { key: "conclusion", label: "Overall conclusion", type: "text" },
  ],
};
export function emptyReportData(fields: ReportField[]): ReportData {
  return Object.fromEntries(
    fields.map((field) => [
      field.key,
      field.repeat
        ? []
        : field.fields
          ? emptyReportData(field.fields)
          : field.type === "number" || field.type === "date"
            ? null
            : field.type === "boolean"
              ? false
              : field.type === "lines"
                ? []
                : "",
    ]),
  );
}
function readDetails(value: unknown, fields: ReportField[]): ReportData {
  const x = d.record(value);
  return Object.fromEntries(
    fields.map((field) => {
      const v = x[field.key];
      let parsed: unknown;
      if (field.fields)
        parsed = field.repeat
          ? d.array(v ?? [], (item) => readDetails(item, field.fields!), 200)
          : readDetails(v ?? {}, field.fields);
      else if (field.type === "number")
        parsed =
          v == null
            ? null
            : typeof v === "number" && Number.isFinite(v) && v >= 0
              ? v
              : d.invalid();
      else if (field.type === "date") parsed = v == null ? null : d.date(v);
      else if (field.type === "boolean") parsed = d.boolean(v);
      else if (field.type === "lines") parsed = d.array(v ?? [], d.string, 200);
      else parsed = d.string(v);
      return [field.key, parsed];
    }),
  );
}
export interface SpecializedInput {
  schemaVersion: number;
  idempotencyKey: string;
  testCaseId: string;
  kind: ReportKind;
  title: string;
  state: string;
  notes: string;
  environment: string;
  applicationVersion: string;
  source: string;
  toolVersion: string;
  adapterVersion: string;
  startedAt: string;
  finishedAt: string;
  retestOf: string;
  details: ReportData;
  originalReport: string;
}
export interface SpecializedResult {
  id: string;
  kind: ReportKind;
  title: string;
  outcome: (typeof reportOutcomes)[number];
  reasons: string[];
  input: SpecializedInput;
  createdBy: string;
  createdAt: string;
}
function enumValue<T extends string>(v: unknown, values: readonly T[]): T {
  return typeof v === "string" && values.includes(v as T)
    ? (v as T)
    : d.invalid();
}
export function readSpecializedResult(value: unknown): SpecializedResult {
  const x = d.record(value),
    input = d.record(x.input);
  const kind = enumValue(x.kind, reportKinds);
  if (input.kind !== kind || d.integer(input.schemaVersion) !== 1)
    return d.invalid();
  return {
    id: d.string(x.id),
    kind,
    title: d.string(x.title),
    outcome: enumValue(x.outcome, reportOutcomes),
    reasons: d.array(x.reasons, d.string, 200),
    createdBy: d.string(x.createdBy),
    createdAt: d.date(x.createdAt),
    input: {
      schemaVersion: 1,
      kind,
      idempotencyKey: d.string(input.idempotencyKey),
      testCaseId: d.string(input.testCaseId),
      title: d.string(input.title),
      state: enumValue(input.state, [
        "completed",
        "not_run",
        "not_applicable",
        "import_error",
      ]),
      notes: d.string(input.notes),
      environment: d.string(input.environment),
      applicationVersion: d.string(input.applicationVersion),
      source: d.string(input.source),
      toolVersion: d.string(input.toolVersion),
      adapterVersion: d.string(input.adapterVersion),
      startedAt: d.date(input.startedAt),
      finishedAt: d.date(input.finishedAt),
      retestOf: d.string(input.retestOf),
      originalReport: d.string(input.originalReport),
      details:
        input.details == null
          ? {}
          : readDetails(input.details, reportFields[kind]),
    },
  };
}
