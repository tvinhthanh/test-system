import { ApiError, isRecord } from "./client";

export function invalid(): never {
  throw new ApiError(
    0,
    "INVALID_RESPONSE",
    "The server returned an unexpected data format.",
  );
}
export function record(value: unknown): Record<string, unknown> {
  return isRecord(value) ? value : invalid();
}
export function string(value: unknown): string {
  return typeof value === "string" ? value : invalid();
}
export function integer(value: unknown): number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0
    ? value
    : invalid();
}
export function boolean(value: unknown): boolean {
  return typeof value === "boolean" ? value : invalid();
}
export function nullableString(value: unknown): string | null {
  return value === null ? null : string(value);
}
export function date(value: unknown): string {
  const s = string(value);
  return Number.isFinite(Date.parse(s)) ? s : invalid();
}
export function array<T>(
  value: unknown,
  parse: (item: unknown) => T,
  max: number,
): T[] {
  if (!Array.isArray(value) || value.length > max) return invalid();
  return value.map(parse);
}
export interface Page<T> {
  items: T[];
  nextCursor: string | null;
}
export function page<T>(
  value: unknown,
  parse: (item: unknown) => T,
  max: number,
): Page<T> {
  const data = record(value);
  return {
    items: array(data.items, parse, max),
    nextCursor: nullableString(data.nextCursor),
  };
}
