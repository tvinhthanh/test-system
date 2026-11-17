import { ApiError } from "../../core/api/client";
const messages: Record<string, string> = {
  SCRIPTS_OFF:
    "Script runs are off on this server. Ask the operator to enable the sandbox.",
  OWNERSHIP_NOT_PROVED:
    "Prove ownership of this environment's host before running a k6 script.",
  PAYLOAD_TOO_LARGE: "The archive is larger than the 10 MB limit.",
  TOO_MANY_SCRIPT_RUNS:
    "One script runs at a time per organization, with at most 30 per day per project. Try again later.",
};
export function explainScriptError(error: unknown): unknown {
  if (!(error instanceof ApiError)) return error;
  return new ApiError(
    error.status,
    error.code,
    messages[error.code] ?? error.detail ?? error.message,
    error.requestId,
    error.detail,
  );
}
