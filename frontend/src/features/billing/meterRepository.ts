import type { ApiClient } from "../../core/api/client";
import * as d from "../../core/api/decode";

// The §10.6 shadow meter of an organization (QĐ-13): counted daily by the
// worker, never charged. Owners only; the server checks.
export interface MeterDay {
  day: string;
  activeTests: number;
  rawFingerprints: number;
  corrections: number;
  runs: number;
  results: number;
  artifactBytes: number;
  drift: number;
  flagged: boolean;
}
export interface Meter {
  days: MeterDay[];
  ready: boolean;
  reasons: string[];
  requiredDays: number;
  driftLimit: number;
}
function fraction(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0
    ? value
    : d.invalid();
}
function meterDay(value: unknown): MeterDay {
  const x = d.record(value);
  return {
    day: d.string(x.day),
    activeTests: d.integer(x.activeTests),
    rawFingerprints: d.integer(x.rawFingerprints),
    corrections: d.integer(x.corrections),
    runs: d.integer(x.runs),
    results: d.integer(x.results),
    artifactBytes: d.integer(x.artifactBytes),
    drift: fraction(x.drift),
    flagged: d.boolean(x.flagged),
  };
}

export class MeterRepository {
  constructor(private api: ApiClient) {}
  async meter(
    orgId: string,
    days: number,
    signal: AbortSignal,
  ): Promise<Meter> {
    const x = d.record(
      await this.api.request(
        `/v1/organizations/${encodeURIComponent(orgId)}/meter?days=${days}`,
        { signal },
      ),
    );
    return {
      days: d.array(x.days, meterDay, 366),
      ready: d.boolean(x.ready),
      reasons: d.array(x.reasons, d.string, 10),
      requiredDays: d.integer(x.requiredDays),
      driftLimit: fraction(x.driftLimit),
    };
  }
}
