// §11.1: HAR files carry the real Authorization headers and cookies of the
// environment under test. They are stripped HERE, before upload, on by
// default; a user who turns it off does so deliberately.

const SENSITIVE_HEADERS = new Set(["authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "x-auth-token"]);

export function stripHar(harJson: string): { json: string; removed: number } {
  const har = JSON.parse(harJson);
  let removed = 0;
  const entries: any[] = har?.log?.entries ?? [];
  for (const entry of entries) {
    for (const side of [entry.request, entry.response]) {
      if (!side) continue;
      if (Array.isArray(side.headers)) {
        const before = side.headers.length;
        side.headers = side.headers.filter((h: any) => !SENSITIVE_HEADERS.has(String(h?.name ?? "").toLowerCase()));
        removed += before - side.headers.length;
      }
      if (Array.isArray(side.cookies) && side.cookies.length > 0) {
        removed += side.cookies.length;
        side.cookies = [];
      }
    }
    // Query strings and bodies may carry tokens too; §11.1 names headers and
    // cookies, and that is what this pass guarantees.
  }
  return { json: JSON.stringify(har), removed };
}
