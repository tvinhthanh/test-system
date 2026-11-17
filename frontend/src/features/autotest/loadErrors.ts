import { ApiError } from "../../core/api/client";

const messages: Record<string, string> = {
  OWNERSHIP_NOT_PROVED:
    "Prove ownership of this environment's host before starting a load test.",
  TOO_MANY_LOAD_RUNS:
    "One load test runs at a time per organization, with at most 20 per day per project. Try again later.",
  AUTO_TEST_OFF:
    "Load tests are off on this server. Ask the operator to enable the checker.",
};

export function explainLoadError(error: unknown): unknown {
  if (!(error instanceof ApiError)) return error;
  return new ApiError(
    error.status,
    error.code,
    messages[error.code] ?? error.detail ?? error.message,
    error.requestId,
    error.detail,
  );
}
