// Closed vocabularies of the Test Management Plane (§6.8.2), shared by the
// repository (decoding) and the screens (selects) without the screens
// touching the data layer.
export type Priority = "p0" | "p1" | "p2" | "p3";
export const priorities: readonly Priority[] = ["p0", "p1", "p2", "p3"];
export type AutomationStatus =
  | "manual_only"
  | "automation_candidate"
  | "automated"
  | "obsolete";
export const automationStatuses: readonly AutomationStatus[] = [
  "manual_only",
  "automation_candidate",
  "automated",
  "obsolete",
];
export type RequirementStatus = "draft" | "active" | "done" | "dropped";
export const requirementStatuses: readonly RequirementStatus[] = [
  "draft",
  "active",
  "done",
  "dropped",
];
export type PlanStatus = "draft" | "active" | "closed";
export type ExecutionStatus = "passed" | "failed" | "blocked" | "skipped";
export const executionStatuses: readonly ExecutionStatus[] = [
  "passed",
  "failed",
  "blocked",
  "skipped",
];
export type ReleaseStatus = "open" | "blocked" | "approved" | "shipped";
export type GateVerdict = "approved" | "warn" | "blocked" | "no_evidence";
