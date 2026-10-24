// Packs this reporter for the ATP server to hand out (QĐ-14): server/atp.sh
// up runs it into backend/deploy/downloads, Caddy serves that at /downloads/,
// and a test project installs the reporter with
//   npm install https://<atp-server>/downloads/<file>
// — nobody needs a copy of the ATP repository.
//
// The file name carries the version and the tarball's sha1 prefix: a
// lockfile pins the integrity of a URL, so one name must never stand for two
// different tarballs. Older tarballs stay where they are — some project's
// lockfile may still point at one. index.json names the current tarball for
// the dashboard's CI tokens page.
//
//   npm run build && node scripts/pack-for-server.mjs <out-dir>
import { execFileSync } from "node:child_process";
import { copyFileSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const out = process.argv[2];
if (!out) {
  console.error("usage: node scripts/pack-for-server.mjs <out-dir>");
  process.exit(2);
}
const tmp = mkdtempSync(join(tmpdir(), "atp-pack-"));
try {
  // npm is a .cmd on Windows, which only starts through a shell.
  const [packed] = JSON.parse(
    execFileSync("npm", ["pack", "--json", "--pack-destination", tmp], {
      encoding: "utf8",
      shell: process.platform === "win32",
    }),
  );
  const file = `atp-playwright-reporter-${packed.version}-${packed.shasum.slice(0, 8)}.tgz`;
  copyFileSync(join(tmp, packed.filename), join(out, file));
  const index = {
    name: packed.name,
    version: packed.version,
    file,
    integrity: packed.integrity,
    builtAt: new Date().toISOString(),
  };
  writeFileSync(join(out, "index.json"), JSON.stringify(index, null, 2) + "\n");
  console.log(`${file} (${packed.size} bytes)`);
} finally {
  rmSync(tmp, { recursive: true, force: true });
}
