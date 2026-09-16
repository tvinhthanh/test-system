import { readFileSync, readdirSync, statSync } from "node:fs";
import { resolve } from "node:path";
import { gzipSync } from "node:zlib";

const root = resolve("dist");
const manifest = JSON.parse(
  readFileSync(resolve(root, ".vite/manifest.json"), "utf8"),
);
const entry = Object.values(manifest).find((item) => item.isEntry);
if (!entry) throw new Error("No build entry");
const visited = new Set();
function walk(item) {
  if (visited.has(item.file)) return 0;
  visited.add(item.file);
  return (
    gzipSync(readFileSync(resolve(root, item.file))).length +
    (item.imports ?? []).reduce((size, key) => size + walk(manifest[key]), 0)
  );
}
const initialGzip = walk(entry);
if (initialGzip > 100 * 1024)
  throw new Error(`Initial JS exceeds 100 KiB gzip: ${initialGzip}`);
function check(directory) {
  for (const name of readdirSync(directory)) {
    const file = resolve(directory, name);
    if (statSync(file).isDirectory()) {
      check(file);
      continue;
    }
    if (!file.endsWith(".js")) continue;
    const content = readFileSync(file, "utf8");
    if (
      content.includes("ATP_FIXTURE_ONLY") ||
      content.includes("atp-fixture-session") ||
      content.includes("Acme Engineering")
    ) {
      throw new Error(`Fixture payload leaked into ${file}`);
    }
    if (gzipSync(content).length > 100 * 1024)
      throw new Error(`Chunk exceeds 100 KiB gzip: ${file}`);
  }
}
check(root);
const lazy = Object.values(manifest).filter((item) => item.isDynamicEntry);
if (lazy.length < 4)
  throw new Error("Expected independently lazy-loaded route modules");
console.log(
  `Bundle gate PASS: initial JS ${(initialGzip / 1024).toFixed(1)} KiB gzip, ${lazy.length} lazy modules, no fixture data.`,
);
