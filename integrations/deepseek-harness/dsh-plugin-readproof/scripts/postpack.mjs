// Undo scripts/prepack.mjs: put the `file:` @readproof/sdk dependency back,
// so the working tree after `npm pack` / `npm publish` is byte-identical to
// the one before it and `npm ci` here keeps resolving the in-repo SDK.
import { existsSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const pkgDir = dirname(dirname(fileURLToPath(import.meta.url)));
const pkgPath = join(pkgDir, "package.json");
const backupPath = join(pkgDir, "package.json.prepack-backup");

if (!existsSync(backupPath)) {
  // prepack decided there was nothing to rewrite.
  process.exit(0);
}

writeFileSync(pkgPath, readFileSync(backupPath, "utf8"));
rmSync(backupPath);
console.log("postpack: restored the file: @readproof/sdk dependency.");
