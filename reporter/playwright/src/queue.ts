// Event batcher (§7.5): flush every 5 s or 100 events, one batch in flight
// at a time so sequences reach the server in order, and a spool file when
// the server cannot be reached at all — the run still ends green (§7.5:
// "reporter chết thì test vẫn xanh — bất biến sản phẩm").

import { appendFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { Client } from "./client";

export interface QueueOptions {
  localRunId: string;
  client: Client;
  spoolDir: string;
  flushEveryMs?: number;
  flushAt?: number;
  maxBatch?: number;
  log: (level: "warn" | "info" | "debug", msg: string) => void;
  onIdentity?: (map: Record<string, string>) => void;
}

export interface QueueStats {
  sent: number;
  accepted: number;
  duplicates: number;
  spooled: number;
  batches: number;
}

export class EventQueue {
  private buffer: unknown[] = [];
  private sequence = 0;
  private timer: NodeJS.Timeout | undefined;
  private inflight: Promise<void> = Promise.resolve();
  private closed = false;
  readonly stats: QueueStats = { sent: 0, accepted: 0, duplicates: 0, spooled: 0, batches: 0 };
  private readonly flushEveryMs: number;
  private readonly flushAt: number;

  constructor(private readonly opts: QueueOptions) {
    this.flushEveryMs = opts.flushEveryMs ?? 5000;
    this.flushAt = opts.flushAt ?? 100;
  }

  /** Assigns the next sequence number and buffers the event. */
  push(event: Record<string, unknown>): number {
    if (this.closed) return -1;
    const seq = ++this.sequence;
    this.buffer.push({ sequence: seq, ...event });
    if (this.buffer.length >= this.flushAt) {
      void this.flush();
    } else if (!this.timer) {
      this.timer = setTimeout(() => void this.flush(), this.flushEveryMs);
      this.timer.unref?.();
    }
    return seq;
  }

  /** Sends what is buffered. Concurrent calls serialise on one in-flight send. */
  flush(): Promise<void> {
    if (this.timer) {
      clearTimeout(this.timer);
      this.timer = undefined;
    }
    if (this.buffer.length === 0) return this.inflight;
    const batch = this.buffer.splice(0, this.opts.maxBatch ?? 500);
    this.inflight = this.inflight.then(() => this.send(batch)).catch(() => undefined);
    // Anything left over 500 goes in the next flush.
    if (this.buffer.length > 0) void this.flush();
    return this.inflight;
  }

  /** Flushes everything and refuses further events. */
  async close(): Promise<void> {
    this.closed = true;
    while (this.buffer.length > 0) await this.flush();
    await this.inflight;
  }

  private async send(batch: unknown[]): Promise<void> {
    try {
      const res = await this.opts.client.sendEvents(this.opts.localRunId, batch);
      this.stats.batches++;
      this.stats.sent += batch.length;
      this.stats.accepted += res.accepted;
      this.stats.duplicates += res.duplicates;
      if (res.identityMap && Object.keys(res.identityMap).length > 0) this.opts.onIdentity?.(res.identityMap);
    } catch (e: any) {
      this.spool(batch, e);
    }
  }

  /** The server is gone for good (or refused us): keep the batch on disk. */
  private spool(batch: unknown[], cause: unknown): void {
    try {
      mkdirSync(this.opts.spoolDir, { recursive: true });
      const file = join(this.opts.spoolDir, `atp-spool-${this.opts.localRunId}.jsonl`);
      appendFileSync(file, batch.map((e) => JSON.stringify(e)).join("\n") + "\n");
      this.stats.spooled += batch.length;
      this.opts.log("warn", `could not deliver ${batch.length} events (${describe(cause)}); spooled to ${file}`);
    } catch (e) {
      this.opts.log("warn", `could not deliver ${batch.length} events and could not spool them either: ${describe(e)}`);
    }
  }
}

export function describe(e: unknown): string {
  if (e && typeof e === "object" && "message" in e) return String((e as any).message);
  return String(e);
}
