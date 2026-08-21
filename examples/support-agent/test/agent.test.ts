// End-to-end tests for the support agent, against a real readproofd.
//
// Everything is built and run from scratch here: `go build` produces
// readproof and readproofd, readproofd gets a throwaway data directory on a
// free port, and the three policy fixtures are COPIED into that directory
// before being registered.
// The tests then edit the copies freely — the repository's own fixtures are
// never touched, so a failing test can't leave the working tree dirty.
//
// The model is always the deterministic fake one: what is under test is
// what Readproof pinned, which has to be provable without Ollama.

import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { spawn } from "node:child_process";
import type { ChildProcess } from "node:child_process";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { after, before, test } from "node:test";
import { fileURLToPath } from "node:url";

import { Readproof, merkleRoot } from "@readproof/sdk";
import type { EvidenceBundle } from "@readproof/sdk";

// dist/test/agent.test.js -> example root -> repo root.
const exampleDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");
const repoRoot = path.resolve(exampleDir, "..", "..");

const QUESTION = "I bought headphones 20 days ago. Can I still get a refund?";

let tmpDir = "";
let readproofBin = "";
let readproofdBin = "";
let readproofdProcess: ChildProcess | undefined;
let endpoint = "";
let policyDir = "";
let rp: Readproof;

// Imported dynamically in before(), after the environment is set: the
// example's config module reads process.env once, at load time.
let agent: typeof import("../src/agent.js");

before(async () => {
  tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "readproof-support-agent-test-"));
  readproofBin = path.join(tmpDir, "readproof");
  readproofdBin = path.join(tmpDir, "readproofd");

  execFileSync("go", ["build", "-o", readproofdBin, "./cmd/readproofd"], { cwd: repoRoot, stdio: "inherit" });
  execFileSync("go", ["build", "-o", readproofBin, "./cmd/readproof"], { cwd: repoRoot, stdio: "inherit" });

  policyDir = path.join(tmpDir, "policies");
  fs.cpSync(path.join(exampleDir, "context", "policies"), policyDir, { recursive: true });

  const port = await freePort();
  endpoint = `http://127.0.0.1:${port}`;
  // readproofd refuses filesystem sources without an allow-listed root, so
  // the copied policy directory — the only place these tests read from — is
  // named explicitly.
  readproofdProcess = spawn(
    readproofdBin,
    ["--addr", `:${port}`, "--data-dir", path.join(tmpDir, "data"), "--filesystem-root", policyDir],
    { stdio: "ignore" },
  );
  await waitForHealthz(endpoint);

  process.env["READPROOF_ENDPOINT"] = endpoint;
  process.env["SUPPORT_CONTEXT_DIR"] = policyDir;
  process.env["SUPPORT_DATA_DIR"] = path.join(tmpDir, "agent-data");
  process.env["SUPPORT_FAKE_MODEL"] = "1";

  agent = await import("../src/agent.js");
  rp = new Readproof({ endpoint });
});

after(() => {
  readproofdProcess?.kill();
  if (tmpDir) {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});

test("setup registers the three policies and tags tone@prod", async () => {
  const result = await agent.setup();

  assert.deepEqual(result.registered, [
    "readproof://acme/policies/refunds",
    "readproof://acme/policies/shipping",
    "readproof://acme/policies/tone",
  ]);
  assert.equal(result.toneTagCreated, true);
  assert.equal(result.toneTag.tag, "prod");

  const registered = await rp.listResources();
  assert.equal(registered.length, 3);

  // Each policy carries the freshness contract config.ts declares for it.
  const byURI = new Map(registered.map((r) => [r.uri, r]));
  assert.equal(byURI.get("readproof://acme/policies/refunds")?.policy.strategy, "require_fresh");
  assert.equal(byURI.get("readproof://acme/policies/shipping")?.policy.strategy, "allow_stale");
  assert.equal(byURI.get("readproof://acme/policies/shipping")?.policy.max_age_seconds, 3600);
  assert.equal(byURI.get("readproof://acme/policies/tone")?.policy.strategy, "require_fresh");

  // Registered against the throwaway copies, not the repository fixtures.
  assert.equal(
    byURI.get("readproof://acme/policies/refunds")?.source.filesystem?.path,
    path.join(policyDir, "refunds.md"),
  );

  // Idempotent: a second setup registers nothing and moves no tag.
  const again = await agent.setup();
  assert.deepEqual(again.registered, []);
  assert.equal(again.toneTagCreated, false);
  assert.equal(again.toneTag.snapshot_id, result.toneTag.snapshot_id);
});

test("ask commits a manifest with three entries in mount order, tone by ref", async () => {
  const record = await agent.ask("1001", QUESTION);

  assert.equal(record.run_id, "ticket-1001");
  assert.match(record.answer, /refunded within 30 days/);
  assert.equal(record.model, "fake-deterministic");

  const manifest = await rp.getManifest(record.manifest_id);
  assert.deepEqual(
    manifest.entries.map((e) => [e.position, e.uri, e.ref]),
    [
      [0, "readproof://acme/policies/refunds", undefined],
      [1, "readproof://acme/policies/shipping", undefined],
      [2, "readproof://acme/policies/tone", "prod"],
    ],
  );

  // The persisted ticket record agrees with the manifest readproofd holds.
  assert.deepEqual(
    record.entries.map((e) => e.content_hash),
    manifest.entries.map((e) => e.content_hash),
  );
  assert.deepEqual(agent.loadTicket("1001"), {
    ticket: record.ticket,
    question: record.question,
    answer: record.answer,
    model: record.model,
    manifest_id: record.manifest_id,
    run_id: record.run_id,
    entries: record.entries,
    at: record.at,
  });
});

test("editing the refund policy moves exactly one diff entry, with provenance", async () => {
  editPolicy("refunds.md", (text) => text.replace("within 30 days", "within 14 days"));

  const first = agent.loadTicket("1001");
  const second = await agent.ask("1002", QUESTION);
  assert.match(second.answer, /refunded within 14 days/);

  const refundsFirst = entryFor(first.entries, "refunds");
  const refundsSecond = entryFor(second.entries, "refunds");
  assert.notEqual(refundsSecond.snapshot_id, refundsFirst.snapshot_id);
  assert.notEqual(refundsSecond.content_hash, refundsFirst.content_hash);

  const diff = await rp.diff(first.manifest_id, second.manifest_id);
  const changed = diff.entries.filter((e) => e.status === "changed");
  assert.equal(changed.length, 1);

  const entry = changed[0];
  assert.equal(entry?.uri, "readproof://acme/policies/refunds");
  assert.ok(entry?.source_revision_a);
  assert.ok(entry?.source_revision_b);
  assert.notEqual(entry?.source_revision_a, entry?.source_revision_b);
  assert.ok(entry?.observed_at_a, "diff entry is missing observed_at_a");
  assert.ok(entry?.observed_at_b, "diff entry is missing observed_at_b");
  assert.match(entry?.unified_diff ?? "", /-Products can be refunded within 30 days/);
  assert.match(entry?.unified_diff ?? "", /\+Products can be refunded within 14 days/);

  assert.deepEqual(
    diff.entries.filter((e) => e.status === "unchanged").map((e) => e.uri),
    ["readproof://acme/policies/shipping", "readproof://acme/policies/tone"],
  );
});

test("replaying the first ticket returns the old bytes while the live source has moved", async () => {
  const first = agent.loadTicket("1001");
  const replay = await rp.replay(first.manifest_id);

  assert.equal(replay.entries.length, 3);
  for (const entry of replay.entries) {
    assert.equal(entry.match, true, `${entry.uri} failed the replay invariant`);
    assert.equal(entry.recorded_hash, entry.replayed_hash);
  }

  const refunds = replay.entries.find((e) => e.uri.endsWith("/refunds"));
  assert.match(refunds?.content ?? "", /within 30 days/);
  assert.doesNotMatch(refunds?.content ?? "", /within 14 days/);

  // ...and the same URI resolved live today says something else entirely.
  const live = await rp.resolve("readproof://acme/policies/refunds");
  assert.notEqual(live.snapshot.content_hash, refunds?.recorded_hash);
  assert.match(live.content, /within 14 days/);
});

test("editing tone.md changes nothing, because the agent mounts tone@prod", async () => {
  const previous = agent.loadTicket("1002");
  editPolicy("tone.md", (text) => `${text}\nAlways open with a one-line summary.\n`);
  editPolicy("refunds.md", (text) => text.replace("within 14 days", "within 7 days"));

  const third = await agent.ask("1003", QUESTION);

  // The tag was never moved, so the tone entry is byte-identical.
  assert.equal(entryFor(third.entries, "tone").snapshot_id, entryFor(previous.entries, "tone").snapshot_id);
  assert.equal(entryFor(third.entries, "tone").content_hash, entryFor(previous.entries, "tone").content_hash);
  assert.equal(entryFor(third.entries, "tone").ref, "prod");

  // The refund policy has no tag, so it followed the edit.
  assert.notEqual(entryFor(third.entries, "refunds").snapshot_id, entryFor(previous.entries, "refunds").snapshot_id);
  assert.match(third.answer, /refunded within 7 days/);

  // Resolving tone bare (no @prod) does see the edit — the pin is the ref,
  // not the resource.
  const live = await rp.resolve("readproof://acme/policies/tone");
  assert.match(live.content, /one-line summary/);
  assert.notEqual(live.snapshot.content_hash, entryFor(third.entries, "tone").content_hash);
});

test("the evidence bundle's subject digest is the merkle root, and the Go CLI verifies it", async () => {
  const { buildEvidence, encodeEvidence } = await import("@readproof/sdk");
  const first = agent.loadTicket("1001");

  const bundle = await buildEvidence(rp, first.manifest_id, { withContent: true });
  assert.equal(bundle.subject.length, 1);
  assert.equal(bundle.subject[0]?.name, first.manifest_id);
  assert.equal(bundle.subject[0]?.digest.sha256, merkleRoot(bundle.predicate.entries));
  assert.equal(bundle.predicate.merkle.root, bundle.subject[0]?.digest.sha256);
  assert.equal(bundle.predicate.replay.all_match, true);

  const bundlePath = path.join(tmpDir, "ticket-1001.bundle.json");
  fs.writeFileSync(bundlePath, encodeEvidence(bundle), "utf-8");

  // The Go verifier is the independent check: it recomputes the root, re-hashes
  // the embedded bytes, and cross-checks the store by replay.
  const output = execFileSync(readproofBin, ["--server", endpoint, "evidence", "verify", bundlePath], {
    encoding: "utf-8",
  });
  assert.match(output, /evidence verified: 3 entries/);
  assert.match(output, new RegExp(bundle.predicate.merkle.root));

  // One flipped base64 character and the same command must fail.
  const tamperedPath = path.join(tmpDir, "ticket-1001.tampered.json");
  fs.writeFileSync(tamperedPath, encodeEvidence(tamper(bundle)), "utf-8");
  assert.throws(
    () => execFileSync(readproofBin, ["--server", endpoint, "evidence", "verify", tamperedPath], { stdio: "pipe" }),
    /Command failed/,
  );
});

test("the repository's policy fixtures were never touched", () => {
  const fixtures = path.join(exampleDir, "context", "policies");
  assert.match(fs.readFileSync(path.join(fixtures, "refunds.md"), "utf-8"), /within 30 days/);
  assert.doesNotMatch(fs.readFileSync(path.join(fixtures, "tone.md"), "utf-8"), /one-line summary/);
});

/** Flips one character of the first entry's embedded content. */
function tamper(bundle: EvidenceBundle): EvidenceBundle {
  const entries = bundle.predicate.entries.map((entry, index) => {
    if (index !== 0 || entry.content_b64 === undefined) {
      return entry;
    }
    const head = entry.content_b64.slice(0, 1);
    // Stay inside the base64 alphabet so this is a content change, not a
    // decode error: the check that must fail is the content re-hash.
    return { ...entry, content_b64: (head === "A" ? "B" : "A") + entry.content_b64.slice(1) };
  });
  return { ...bundle, predicate: { ...bundle.predicate, entries } };
}

function entryFor(entries: import("../src/agent.js").TicketEntry[], name: string) {
  const entry = entries.find((e) => e.uri.endsWith(`/${name}`));
  if (!entry) {
    throw new Error(`no manifest entry for ${name}`);
  }
  return entry;
}

function editPolicy(file: string, edit: (text: string) => string): void {
  const target = path.join(policyDir, file);
  fs.writeFileSync(target, edit(fs.readFileSync(target, "utf-8")), "utf-8");
}

/** Bind port 0, read what the OS handed out, release it. */
function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.on("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (address === null || typeof address === "string") {
        reject(new Error("could not determine a free port"));
        return;
      }
      const { port } = address;
      server.close(() => resolve(port));
    });
  });
}

async function waitForHealthz(base: string): Promise<void> {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    try {
      const response = await fetch(`${base}/healthz`);
      if (response.ok) {
        return;
      }
    } catch {
      // readproofd is still starting up.
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`readproofd at ${base} did not become healthy`);
}
