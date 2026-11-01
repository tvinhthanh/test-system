import { ApiError } from "../../core/api/client";

// What the person can do about the refusals of the QĐ-14/15 screens. The
// client's generic per-status text ("This resource changed") is wrong for
// these: a rejected API key or a duplicate environment key is not a stale
// page. Codes not listed keep their message; INVALID_PAYLOAD and CONFLICT
// show the server's own sentence, which names the field, and
// AI_MODEL_UNAVAILABLE the server's, which names models the key can use.
const messages: Record<string, string> = {
  AI_KEY_REJECTED:
    "The provider rejected this API key. Check it was copied whole, is still active, and is for the provider chosen.",
  AI_SERVER_KEY_REJECTED:
    "The provider rejected the server's key. Tell the operator, or use your own key.",
  AI_KEY_NO_CREDIT:
    "The account behind this key is out of credit or over its spending limit. Add credit with the provider, or use another key.",
  AI_UNREACHABLE:
    "The AI provider could not be reached to check the key. Nothing was saved; try again in a moment.",
  AI_NOT_CONFIGURED:
    "AI drafting is not set up. An organization admin adds an API key under Team → organization → AI settings.",
  TOO_MANY_SUGGESTIONS:
    "This project already has drafts in progress, or reached today's limit of 30. Try again later.",
  URL_CHECKS_OFF:
    "URL checks are off on this server. The operator turns them on with URLCHECK=on.",
  TOO_MANY_URL_CHECKS:
    "Five checks are already waiting in this project. Try again when they finish.",
  AUTO_TEST_OFF:
    "Auto test runs in the checker service, which is off on this server. The operator turns it on with URLCHECK=on.",
  TOO_MANY_AUTO_RUNS:
    "Two runs are already going in this project, or it reached today's limit of 100. Try again when they finish.",
  TOO_MANY_AUTO_DRAFTS:
    "This project already has drafts in progress, or reached today's limit of 30. Try again later.",
  SCAN_RUNNING:
    "A site scan is already running in this project. Try again when it finishes.",
};

export function explain(error: unknown): unknown {
  if (!(error instanceof ApiError)) return error;
  const text =
    messages[error.code] ??
    (["INVALID_PAYLOAD", "CONFLICT", "AI_MODEL_UNAVAILABLE"].includes(error.code)
      ? error.detail
      : undefined);
  return text
    ? new ApiError(
        error.status,
        error.code,
        text,
        error.requestId,
        error.detail,
      )
    : error;
}
