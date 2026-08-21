// Rewrite the @readproof/sdk dependency for the published tarball.
//
// In this repository the plugin consumes the SDK as `file:../../../sdk/typescript`,
// which is what makes `npm ci && npm test` work against unreleased SDK
// changes. A `file:` specifier is meaningless to anyone installing
// `dsh-plugin-readproof` from npm, so the packed package.json has to name
// the published semver instead.
//
// npm runs this before packing and `postpack.mjs` right after, so the
// working tree ends up exactly as it started. The backup file is the
// contract between the two: if a pack dies in between, the next prepack
// refuses to run rather than baking a semver range into the tree
// permanently.
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const pkgDir = dirname(dirname(fileURLToPath(import.meta.url)));
const pkgPath = join(pkgDir, "package.json");
const backupPath = join(pkgDir, "package.json.prepack-backup");
const sdkPkgPath = join(pkgDir, "..", "..", "..", "sdk", "typescript", "package.json");

if (existsSync(backupPath)) {
  console.error(
    `prepack: ${backupPath} already exists — a previous pack did not finish.\n` +
      `Restore it over package.json (or run \`node scripts/postpack.mjs\`) before packing again.`,
  );
  process.exit(1);
}

const raw = readFileSync(pkgPath, "utf8");
const pkg = JSON.parse(raw);

// The tarball ships built JavaScript only; packing an unbuilt tree would
// produce a package that installs and then fails to import.
const entry = join(pkgDir, "dist", "src", "index.js");
if (!existsSync(entry)) {
  console.error(`prepack: ${entry} is missing — run \`npm run build\` before packing.`);
  process.exit(1);
}

const current = pkg.dependencies?.["@readproof/sdk"];
if (typeof current !== "string" || !current.startsWith("file:")) {
  // Already a semver range (or gone). Nothing to rewrite, nothing to restore.
  console.log(`prepack: @readproof/sdk is "${current}" — leaving it alone.`);
  process.exit(0);
}

const sdkVersion = JSON.parse(readFileSync(sdkPkgPath, "utf8")).version;
const range = `^${sdkVersion}`;

writeFileSync(backupPath, raw);
pkg.dependencies["@readproof/sdk"] = range;
writeFileSync(pkgPath, `${JSON.stringify(pkg, null, 2)}\n`);

console.log(`prepack: @readproof/sdk "${current}" -> "${range}" for the tarball.`);
