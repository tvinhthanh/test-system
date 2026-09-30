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
  /**
   * Resolves once the run exists on the server (true) or could not be
   * created (false). Batches wait for it; with false they go straight to
   * the spool — the server has no run to put them in.
   */
  ready?: Promise<boolean>;
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
  private abandoned = false;
  /** Batches taken from the buffer and not yet acknowledged or spooled. */
  private readonly unsent = new Set<unknown[]>();
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
    this.unsent.add(batch);
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

  /**
   * Gives up on delivery: everything not yet acknowledged — buffered or in
   * flight — goes to the spool now, and nothing more is sent. The reporter
   * calls it when the platform has had its time after the last test (§7.5).
   * A batch that was in flight may still land; replaying the spool then
   * only yields duplicates, which (localRunId, sequence) absorbs (§7.1).
   */
  abandon(reason: string): void {
    this.closed = true;
    this.abandoned = true;
    if (this.timer) {
      clearTimeout(this.timer);
      this.timer = undefined;
    }
    if (this.buffer.length > 0) this.unsent.add(this.buffer.splice(0));
    const cause = new Error(reason);
    for (const batch of [...this.unsent]) this.spool(batch, cause);
  }

  private async send(batch: unknown[]): Promise<void> {
    const ready = await (this.opts.ready ?? Promise.resolve(true));
    if (this.abandoned || !this.unsent.has(batch)) return;
    if (!ready) {
      this.spool(batch, new Error("the run was not created on the server"));
      return;
    }
    try {
      const res = await this.opts.client.sendEvents(this.opts.localRunId, batch);
      this.unsent.delete(batch);
      this.stats.batches++;
      this.stats.sent += batch.length;
      this.stats.accepted += res.accepted;
      this.stats.duplicates += res.duplicates;
      if (res.identityMap && Object.keys(res.identityMap).length > 0) this.opts.onIdentity?.(res.identityMap);
    } catch (e: any) {
      if (!this.abandoned) this.spool(batch, e);
    }
  }

  /** The server is gone for good (or refused us): keep the batch on disk. */
  private spool(batch: unknown[], cause: unknown): void {
    this.unsent.delete(batch);
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
