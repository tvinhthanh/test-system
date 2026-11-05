import type { LoadRunInput } from "./autotestRepository";

export function buildLoadInput(fields: {
  environmentId: string;
  paths: string;
  vus: string;
  seconds: string;
  rampSeconds: string;
  maxP95Ms: string;
  maxErrorRate: string;
}): { input: LoadRunInput; error: "" } | { input: null; error: string } {
  if (!fields.environmentId)
    return { input: null, error: "Choose an environment with a site address." };
  const paths = fields.paths
    .split(/\r?\n/)
    .map((path) => path.trim())
    .filter(Boolean);
  if (
    paths.length > 20 ||
    paths.some(
      (path) =>
        !path.startsWith("/") || path.startsWith("//") || /[\s\\]/.test(path),
    )
  )
    return {
      input: null,
      error:
        "Enter at most 20 paths, one per line, each starting with a single / and with no spaces.",
    };
  if (paths.some((path) => path.length > 2000))
    return { input: null, error: "A path is longer than 2,000 characters." };
  const integer = (raw: string) =>
    /^\d+$/.test(raw.trim()) ? Number(raw) : NaN;
  const vus = integer(fields.vus);
  const seconds = integer(fields.seconds);
  const rampSeconds = integer(fields.rampSeconds);
  const maxP95Ms = fields.maxP95Ms.trim()
    ? integer(fields.maxP95Ms)
    : undefined;
  const maxErrorRate = fields.maxErrorRate.trim()
    ? Number(fields.maxErrorRate)
    : undefined;
  if (!Number.isSafeInteger(vus) || vus < 1 || vus > 200)
    return {
      input: null,
      error:
        "Virtual users must be between 1 and 200. The server may have a lower cap.",
    };
  if (!Number.isSafeInteger(seconds) || seconds < 5 || seconds > 600)
    return {
      input: null,
      error:
        "Duration must be between 5 and 600 seconds. The server may have a lower cap.",
    };
  if (
    !Number.isSafeInteger(rampSeconds) ||
    rampSeconds < 0 ||
    rampSeconds >= seconds
  )
    return {
      input: null,
      error: "Ramp seconds must be zero or less than the duration.",
    };
  if (
    maxP95Ms !== undefined &&
    (!Number.isSafeInteger(maxP95Ms) || maxP95Ms < 0 || maxP95Ms > 600000)
  )
    return {
      input: null,
      error: "P95 budget must be between 0 and 600,000 ms.",
    };
  if (
    maxErrorRate !== undefined &&
    (!Number.isFinite(maxErrorRate) || maxErrorRate < 0 || maxErrorRate > 100)
  )
    return {
      input: null,
      error: "Error-rate budget must be a percent from 0 to 100.",
    };
  return {
    input: {
      environmentId: fields.environmentId,
      paths,
      vus,
      seconds,
      rampSeconds,
      ...(maxP95Ms !== undefined ? { maxP95Ms } : {}),
      ...(maxErrorRate !== undefined ? { maxErrorRate } : {}),
    },
    error: "",
  };
}
