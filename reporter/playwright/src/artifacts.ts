// Artifact upload (§7.7): declare → PUT or multipart → complete, per
// attempt, with the caps the server sent back on run creation. A refused or
// failed artifact is logged and dropped; it never fails the run (§10.4).

import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { Client, ApiError } from "./client";
import { describe } from "./queue";
import { stripHar } from "./har";

export type Kind = "screenshot" | "video" | "trace" | "har" | "log" | "attachment";

export interface Attachment {
  name: string;
  contentType: string;
  path?: string;
  body?: Buffer;
}

export interface ArtifactRef {
  localRunId: string;
  clientTestId: string;
  occurrenceIndex: number;
  attempt: number;
}

export interface UploadOptions {
  stripHar: boolean;
  limits?: Record<string, number>;
  log: (level: "warn" | "info" | "debug", msg: string) => void;
}

/** Playwright attachment → §7.7 kind and a storable content type. */
export function classify(a: Attachment): { kind: Kind; contentType: string } | null {
  const name = a.name.toLowerCase();
  const ct = (a.contentType || "").toLowerCase().split(";")[0].trim();
  if (name === "trace" || name.endsWith(".zip") && ct === "application/zip") return { kind: "trace", contentType: "application/zip" };
  if (name.endsWith(".har") || name === "har") return { kind: "har", contentType: "application/json" };
  if (ct.startsWith("image/")) {
    if (ct === "image/png" || ct === "image/jpeg" || ct === "image/webp") return { kind: "screenshot", contentType: ct };
    return null; // svg and friends are not stored (§11.5)
  }
  if (ct === "video/webm" || ct === "video/mp4") return { kind: "video", contentType: ct };
  if (ct === "text/plain" || name.endsWith(".log") || name.endsWith(".txt")) return { kind: "log", contentType: "text/plain" };
  if (ct === "text/html" || ct === "image/svg+xml" || ct === "application/javascript" || ct === "text/javascript" || ct === "") return null;
  return { kind: "attachment", contentType: ct };
}

export interface UploadOutcome {
  name: string;
  status: "verified" | "skipped" | "too_large" | "failed";
  reason?: string;
}

export async function uploadAttachment(client: Client, ref: ArtifactRef, a: Attachment, opts: UploadOptions): Promise<UploadOutcome> {
  const cls = classify(a);
  if (!cls) return { name: a.name, status: "skipped", reason: "content type not accepted" };
  try {
    let data: Buffer;
    if (a.body) data = a.body;
    else if (a.path) data = await readFile(a.path);
    else return { name: a.name, status: "skipped", reason: "no content" };
    if (cls.kind === "har" && opts.stripHar) {
      const { json, removed } = stripHar(data.toString("utf8"));
      data = Buffer.from(json, "utf8");
      if (removed > 0) opts.log("debug", `${a.name}: stripped ${removed} sensitive header/cookie entries`);
    }
    if (data.length === 0) return { name: a.name, status: "skipped", reason: "empty" };
    const cap = opts.limits?.[cls.kind];
    if (cap !== undefined && data.length > cap) {
      opts.log("warn", `${a.name}: ${data.length} bytes exceeds the ${cls.kind} cap of ${cap}; not uploaded (run still valid)`);
      return { name: a.name, status: "too_large" };
    }
    const sha256 = createHash("sha256").update(data).digest("hex");
    const decl = await client.declareArtifact({
      localRunId: ref.localRunId, clientTestId: ref.clientTestId, occurrenceIndex: ref.occurrenceIndex, attempt: ref.attempt,
      name: a.name.slice(0, 200), kind: cls.kind, contentType: cls.contentType, sizeBytes: data.length, sha256,
    });
    if (decl.status === "verified") return { name: a.name, status: "verified" };
    const parts: { partNumber: number; etag: string }[] = [];
    if (decl.upload) {
      await client.put(decl.upload.url, data, decl.upload.headers);
    } else if (decl.multipart) {
      const mp = decl.multipart;
      const received = new Set(mp.received);
      const todo = mp.parts.filter((p) => !received.has(p.number));
      // ≤ 4 in parallel (§7.7).
      const workers = Math.min(mp.maxParallel || 4, 4);
      let next = 0;
      await Promise.all(
        Array.from({ length: workers }, async () => {
          for (;;) {
            const i = next++;
            if (i >= todo.length) return;
            const p = todo[i];
            const start = (p.number - 1) * mp.partSize;
            const chunk = data.subarray(start, Math.min(start + mp.partSize, data.length));
            const { etag } = await client.put(p.url, chunk, {});
            parts.push({ partNumber: p.number, etag });
          }
        }),
      );
      // Parts the server already had: it knows their ETags; re-declare would
      // have listed them. Send what we uploaded; the server assembles from
      // its own list when ours is partial.
    }
    const done = await client.completeArtifact(decl.artifactId, { sha256, parts });
    return { name: a.name, status: done.status === "verified" ? "verified" : "failed", reason: done.status };
  } catch (e) {
    if (e instanceof ApiError && e.status === 413) {
      opts.log("warn", `${a.name}: refused by the server cap (${e.message}); run still valid`);
      return { name: a.name, status: "too_large", reason: e.message };
    }
    opts.log("warn", `${a.name}: upload failed: ${describe(e)}`);
    return { name: a.name, status: "failed", reason: describe(e) };
  }
}
