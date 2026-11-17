export type ScriptKind = "playwright" | "k6";
export interface ScriptFields {
  archive: File | null;
  kind: ScriptKind;
  environmentId: string;
  entry: string;
  project: string;
  grep: string;
  vus: string;
  seconds: string;
  timeoutSeconds: string;
}
const integer = (value: string) =>
  /^\d+$/.test(value.trim()) ? Number(value) : NaN;
export function buildScriptMultipart(fields: ScriptFields): {
  data: FormData | null;
  error: string;
} {
  if (!fields.archive)
    return { data: null, error: "Choose a .tar.gz archive." };
  if (!fields.archive.name.toLowerCase().endsWith(".tar.gz"))
    return { data: null, error: "The archive must be a .tar.gz file." };
  if (!fields.archive.size || fields.archive.size > 10 * 1024 * 1024)
    return {
      data: null,
      error: "The archive must be between 1 byte and 10 MB.",
    };
  if (!fields.environmentId)
    return { data: null, error: "Choose an environment with a site address." };
  if (
    fields.entry.length > 200 ||
    fields.project.length > 100 ||
    fields.grep.length > 100
  )
    return {
      data: null,
      error: "Entry, project, or grep is longer than the server accepts.",
    };
  const timeout = fields.timeoutSeconds.trim()
    ? integer(fields.timeoutSeconds)
    : undefined;
  if (
    timeout !== undefined &&
    (!Number.isSafeInteger(timeout) || timeout < 30 || timeout > 3600)
  )
    return {
      data: null,
      error: "Timeout must be between 30 and 3,600 seconds.",
    };
  let vus: number | undefined, seconds: number | undefined;
  if (fields.kind === "k6") {
    vus = integer(fields.vus);
    seconds = integer(fields.seconds);
    if (!fields.entry.trim())
      return { data: null, error: "K6 needs an entry script path." };
    if (!Number.isSafeInteger(vus) || vus! < 1 || vus! > 200)
      return {
        data: null,
        error: "K6 virtual users must be between 1 and 200.",
      };
    if (!Number.isSafeInteger(seconds) || seconds! < 5 || seconds! > 600)
      return {
        data: null,
        error: "K6 duration must be between 5 and 600 seconds.",
      };
  }
  const data = new FormData();
  data.append("archive", fields.archive);
  data.append("kind", fields.kind);
  data.append("environmentId", fields.environmentId);
  if (fields.entry.trim()) data.append("entry", fields.entry.trim());
  if (fields.kind === "playwright" && fields.project.trim())
    data.append("project", fields.project.trim());
  if (fields.kind === "playwright" && fields.grep.trim())
    data.append("grep", fields.grep.trim());
  if (vus !== undefined) data.append("vus", String(vus));
  if (seconds !== undefined) data.append("seconds", String(seconds));
  if (timeout !== undefined) data.append("timeoutSeconds", String(timeout));
  return { data, error: "" };
}
