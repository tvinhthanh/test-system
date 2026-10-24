import { ApiError } from "../../core/api/client";

// What the person can do about the refusals of the QĐ-14/15 screens. The
// client's generic per-status text ("This resource changed") is wrong for
// these: a rejected API key or a duplicate environment key is not a stale
// page. Codes not listed keep their message; INVALID_PAYLOAD and CONFLICT
// show the server's own sentence, which names the field.
const messages: Record<string, string> = {
  AI_KEY_REJECTED:
    "Anthropic rejected this API key. Check it was copied whole and is still active.",
  AI_SERVER_KEY_REJECTED:
    "Anthropic rejected the server's key. Tell the operator, or use your own key.",
  AI_MODEL_UNAVAILABLE:
    "This key cannot use the chosen model. Pick another model, or check the key's access in the Anthropic console.",
  AI_UNREACHABLE:
    "Anthropic could not be reached to check the key. Nothing was saved; try again in a moment.",
  AI_NOT_CONFIGURED:
    "AI drafting is not set up. An organization admin adds an API key under Team → organization → AI settings.",
  TOO_MANY_SUGGESTIONS:
    "This project already has drafts in progress, or reached today's limit of 30. Try again later.",
  URL_CHECKS_OFF:
    "URL checks are off on this server. The operator turns them on with URLCHECK=on.",
  TOO_MANY_URL_CHECKS:
    "Five checks are already waiting in this project. Try again when they finish.",
};

export function explain(error: unknown): unknown {
  if (!(error instanceof ApiError)) return error;
  const text =
    messages[error.code] ??
    (["INVALID_PAYLOAD", "CONFLICT"].includes(error.code)
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
