import { test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import type { AddressInfo } from "node:net";

import {
  Ctx,
  buildEvidence,
  encodeEvidence,
  merkleLeaf,
  merkleRoot,
  EVIDENCE_PREDICATE_TYPE,
  EVIDENCE_STATEMENT_TYPE,
} from "../src/index.js";

function startMockServer(handler: http.RequestListener): Promise<{ url: string; close: () => Promise<void> }> {
  return new Promise((resolve) => {
    const server = http.createServer(handler);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address() as AddressInfo;
      resolve({
        url: `http://127.0.0.1:${address.port}`,
        close: () => new Promise((res) => server.close(() => res())),
      });
    });
  });
}

const entryA = { position: 0, uri: "ctx://demo/policies/refunds", content_hash: "sha256:aaaa" };
const entryB = { position: 1, uri: "ctx://demo/policies/shipping", content_hash: "sha256:bbbb" };
const entryC = { position: 2, uri: "ctx://demo/faq", content_hash: "sha256:cccc" };
const entryD = { position: 3, uri: "ctx://demo/tos", content_hash: "sha256:dddd" };

// The same fixed vectors internal/evidence/merkle_test.go asserts. They were
// produced by an independent implementation of the documented rule, so these
// pin the two exporters to one another rather than to their own code.
test("merkleRoot matches the Go exporter's fixed vectors", () => {
  assert.equal(merkleRoot([]), "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
  assert.equal(merkleRoot([entryA]), "70c844e26de1089a9d8386db4c8c4aa51e6a21202a0e857f5d51f92b496c4799");
  assert.equal(merkleRoot([entryA, entryB]), "9f4a65f56078f8bbadbd8c2aaf697699ac14d7ada1e15e45a0af1a8b56c6f87a");
  assert.equal(
    merkleRoot([entryA, entryB, entryC]),
    "c56ea8ed87709d94dd208274e1865c78941d16bbbd14f7e09b6eeef96804a9b6",
  );
  assert.equal(
    merkleRoot([entryA, entryB, entryC, entryD]),
    "a5b64b93a399a39b411a31b4d5dd47adaef09272a9438649670d8ebe9459c99d",
  );
});

test("merkleLeaf matches the Go exporter and a single leaf is the root", () => {
  const leaf = merkleLeaf(entryA).toString("hex");
  assert.equal(leaf, "70c844e26de1089a9d8386db4c8c4aa51e6a21202a0e857f5d51f92b496c4799");
  assert.equal(merkleRoot([entryA]), leaf);
});

test("merkleRoot is sensitive to entry order and to content hashes", () => {
  assert.notEqual(merkleRoot([entryA, entryB]), merkleRoot([entryB, entryA]));
  assert.equal(merkleRoot([entryB, entryA]), "22fa08b5ca3cbcb085610c4b620fa8e8f05e1d01bf596bcdc43b175c3d5f4e88");
  assert.equal(
    merkleRoot([entryA, { ...entryB, content_hash: "sha256:bbbc" }]),
    "7f2132f24b570d6130a6aaeff85ce535a631daef70de4e17e58164aa90749e5d",
  );
});

const refunds = "Products can be refunded within 30 days.\n";
const shipping = "Orders ship within 2 business days.\n";
const refundsHash = "sha256:c8b0bb212e93151d720746e36ff3b7076727cb577614feafa0d61f168965aedb";
const shippingHash = "sha256:14b635244186199549594da12dd90ca4124d425b881c0d99a419b4ebdfb1b524";

const manifestResponse = {
  manifest_id: "manifest_1",
  run_id: "run-audit-1",
  created_at: "2026-03-01T10:00:00Z",
  entries: [
    { position: 0, uri: "ctx://demo/policies/refunds", snapshot_id: "snap_1", materialization_id: "mat_1", content_hash: refundsHash },
    { position: 1, uri: "ctx://demo/policies/shipping", snapshot_id: "snap_2", materialization_id: "mat_2", content_hash: shippingHash },
  ],
};

const snapshots: Record<string, unknown> = {
  snap_1: {
    id: "snap_1",
    resource_uri: "ctx://demo/policies/refunds",
    source_revision: "sha256:c8b0bb212e93",
    content_hash: refundsHash,
    observed_at: "2026-03-01T09:59:00Z",
    created_at: "2026-03-01T09:59:00Z",
    content_type: "text/markdown",
    bytes: 40,
    provenance: { source_type: "filesystem", path: "/srv/policies/refunds.md" },
  },
  snap_2: {
    id: "snap_2",
    resource_uri: "ctx://demo/policies/shipping",
    source_revision: "sha256:14b635244186",
    content_hash: shippingHash,
    observed_at: "2026-03-01T09:59:30Z",
    created_at: "2026-03-01T09:59:30Z",
    content_type: "text/markdown",
    bytes: 35,
    provenance: { source_type: "filesystem", path: "/srv/policies/shipping.md" },
  },
};

const resources: Record<string, unknown> = {
  "ctx://demo/policies/refunds": {
    uri: "ctx://demo/policies/refunds",
    namespace: "demo",
    path: "policies/refunds",
    source: { kind: "filesystem", filesystem: { path: "/srv/policies/refunds.md" } },
    policy: { strategy: "require_fresh" },
    created_at: "2026-03-01T09:00:00Z",
    updated_at: "2026-03-01T09:59:00Z",
  },
  "ctx://demo/policies/shipping": {
    uri: "ctx://demo/policies/shipping",
    namespace: "demo",
    path: "policies/shipping",
    source: {
      kind: "http",
      http: { url: "https://policies.internal/shipping", headers: { Authorization: "Bearer live-secret", "X-Trace-Id": "t-1" } },
    },
    policy: { strategy: "allow_stale", max_age_seconds: 3600 },
    created_at: "2026-03-01T09:00:00Z",
    updated_at: "2026-03-01T09:59:30Z",
  },
};

const replayResponse = {
  manifest: manifestResponse,
  entries: [
    {
      position: 0,
      uri: "ctx://demo/policies/refunds",
      materialization_id: "mat_1",
      recorded_hash: refundsHash,
      replayed_hash: refundsHash,
      content: Buffer.from(refunds, "utf-8").toString("base64"),
      match: true,
    },
    {
      position: 1,
      uri: "ctx://demo/policies/shipping",
      materialization_id: "mat_2",
      recorded_hash: shippingHash,
      replayed_hash: shippingHash,
      content: Buffer.from(shipping, "utf-8").toString("base64"),
      match: true,
    },
  ],
};

/** A ctxd stand-in serving the fixtures above; unknown resources 404. */
function startCtxdMock(): Promise<{ url: string; close: () => Promise<void> }> {
  return startMockServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://127.0.0.1");
    res.setHeader("Content-Type", "application/json");

    if (url.pathname === "/v1/manifests") {
      res.end(JSON.stringify(manifestResponse));
      return;
    }
    if (url.pathname === "/v1/snapshots") {
      const snapshot = snapshots[url.searchParams.get("id") ?? ""];
      if (!snapshot) {
        res.statusCode = 404;
        res.end(JSON.stringify({ error: "snapshot: not found" }));
        return;
      }
      res.end(JSON.stringify(snapshot));
      return;
    }
    if (url.pathname === "/v1/resources/get") {
      const resource = resources[url.searchParams.get("uri") ?? ""];
      if (!resource) {
        res.statusCode = 404;
        res.end(JSON.stringify({ error: "resource: not found" }));
        return;
      }
      res.end(JSON.stringify(resource));
      return;
    }
    if (url.pathname === "/v1/replay") {
      res.end(JSON.stringify(replayResponse));
      return;
    }
    res.statusCode = 404;
    res.end(JSON.stringify({ error: `no route ${url.pathname}` }));
  });
}

const fixedNow = () => new Date("2026-03-01T12:00:00Z");

test("buildEvidence produces an in-toto statement rooted at the merkle root", async () => {
  const { url, close } = await startCtxdMock();
  try {
    const ctx = new Ctx({ endpoint: url });
    const bundle = await buildEvidence(ctx, "run-audit-1", { withContent: true, now: fixedNow });

    assert.equal(bundle._type, EVIDENCE_STATEMENT_TYPE);
    assert.equal(bundle.predicateType, EVIDENCE_PREDICATE_TYPE);
    assert.equal(bundle.subject.length, 1);
    assert.equal(bundle.subject[0]?.name, "manifest_1");
    assert.equal(bundle.subject[0]?.digest.sha256, merkleRoot(bundle.predicate.entries));
    assert.equal(bundle.predicate.merkle.root, bundle.subject[0]?.digest.sha256);
    assert.equal(bundle.predicate.run_id, "run-audit-1");
    assert.equal(bundle.predicate.manifest_created_at, "2026-03-01T10:00:00Z");
    assert.equal(bundle.predicate.generated_at, "2026-03-01T12:00:00.000Z");
    assert.equal(bundle.predicate.exporter.name, "ctx");

    // Entries are hydrated from the snapshots, not just copied from the
    // manifest.
    assert.equal(bundle.predicate.entries.length, 2);
    const first = bundle.predicate.entries[0];
    assert.equal(first?.uri, "ctx://demo/policies/refunds");
    assert.equal(first?.snapshot_id, "snap_1");
    assert.equal(first?.source_revision, "sha256:c8b0bb212e93");
    assert.equal(first?.observed_at, "2026-03-01T09:59:00Z");
    assert.equal(first?.content_type, "text/markdown");
    assert.equal(first?.bytes, 40);
    assert.equal(Buffer.from(first?.content_b64 ?? "", "base64").toString("utf-8"), refunds);
    // Go's encoding/json sorts map keys; the SDK sorts to match.
    assert.deepEqual(Object.keys(first?.provenance ?? {}), ["path", "source_type"]);

    assert.equal(bundle.predicate.resources.length, 2);
    assert.equal(bundle.predicate.resources[0]?.source.kind, "filesystem");
    assert.equal(bundle.predicate.resources[0]?.source.config.filesystem?.path, "/srv/policies/refunds.md");
    assert.equal(bundle.predicate.resources[1]?.policy.max_age_seconds, 3600);

    assert.equal(bundle.predicate.replay.all_match, true);
    assert.equal(bundle.predicate.replay.verified_at, "2026-03-01T12:00:00.000Z");
    assert.deepEqual(
      bundle.predicate.replay.entries.map((e) => [e.position, e.match, e.expected_hash]),
      [
        [0, true, refundsHash],
        [1, true, shippingHash],
      ],
    );
  } finally {
    await close();
  }
});

test("buildEvidence redacts credential-bearing source headers", async () => {
  const { url, close } = await startCtxdMock();
  try {
    const ctx = new Ctx({ endpoint: url });
    const bundle = await buildEvidence(ctx, "run-audit-1", { now: fixedNow });

    const headers = bundle.predicate.resources[1]?.source.config.http?.headers ?? {};
    assert.equal(headers["Authorization"], "[REDACTED]");
    assert.equal(headers["X-Trace-Id"], "t-1");
    assert.ok(!encodeEvidence(bundle).includes("live-secret"), "bundle leaked a credential");
  } finally {
    await close();
  }
});

test("buildEvidence omits content unless asked, and encodes with a trailing newline", async () => {
  const { url, close } = await startCtxdMock();
  try {
    const ctx = new Ctx({ endpoint: url });
    const bundle = await buildEvidence(ctx, "run-audit-1", { now: fixedNow });

    for (const entry of bundle.predicate.entries) {
      assert.equal(entry.content_b64, undefined);
    }
    const encoded = encodeEvidence(bundle);
    assert.ok(!encoded.includes("content_b64"), "metadata-only bundle carries a content_b64 key");
    assert.ok(!encoded.includes("refunded within"), "metadata-only bundle leaked content");
    assert.ok(encoded.endsWith("}\n"));

    // Key order is fixed by the Go struct definitions — the two exporters
    // must serialize the predicate the same way.
    assert.deepEqual(Object.keys(bundle), ["_type", "subject", "predicateType", "predicate"]);
    assert.deepEqual(Object.keys(bundle.predicate), [
      "run_id",
      "manifest_id",
      "manifest_created_at",
      "generated_at",
      "exporter",
      "merkle",
      "entries",
      "resources",
      "replay",
    ]);
  } finally {
    await close();
  }
});

test("buildEvidence records a deregistered resource as missing instead of failing", async () => {
  const { url, close } = await startMockServer((req, res) => {
    const url2 = new URL(req.url ?? "/", "http://127.0.0.1");
    res.setHeader("Content-Type", "application/json");

    if (url2.pathname === "/v1/manifests") {
      res.end(JSON.stringify(manifestResponse));
      return;
    }
    if (url2.pathname === "/v1/snapshots") {
      res.end(JSON.stringify(snapshots[url2.searchParams.get("id") ?? "snap_1"]));
      return;
    }
    if (url2.pathname === "/v1/replay") {
      res.end(JSON.stringify(replayResponse));
      return;
    }
    // Every resource lookup 404s: they were all deregistered.
    res.statusCode = 404;
    res.end(JSON.stringify({ error: "resource: not found" }));
  });

  try {
    const ctx = new Ctx({ endpoint: url });
    const bundle = await buildEvidence(ctx, "run-audit-1", { now: fixedNow });

    assert.equal(bundle.predicate.resources.length, 2);
    for (const resource of bundle.predicate.resources) {
      assert.equal(resource.missing, true);
      assert.equal(resource.namespace, "demo");
    }
    // A missing definition must not move the digest.
    assert.equal(bundle.subject[0]?.digest.sha256, merkleRoot(bundle.predicate.entries));
  } finally {
    await close();
  }
});

test("buildEvidence records a replay failure rather than throwing", async () => {
  const { url, close } = await startMockServer((req, res) => {
    const url2 = new URL(req.url ?? "/", "http://127.0.0.1");
    res.setHeader("Content-Type", "application/json");

    if (url2.pathname === "/v1/manifests") {
      res.end(JSON.stringify(manifestResponse));
      return;
    }
    if (url2.pathname === "/v1/snapshots") {
      res.end(JSON.stringify(snapshots[url2.searchParams.get("id") ?? "snap_1"]));
      return;
    }
    if (url2.pathname === "/v1/resources/get") {
      res.end(JSON.stringify(resources[url2.searchParams.get("uri") ?? ""]));
      return;
    }
    res.statusCode = 500;
    res.end(JSON.stringify({ error: "replay: load blob sha256:dead: blob: not found" }));
  });

  try {
    const ctx = new Ctx({ endpoint: url });
    const bundle = await buildEvidence(ctx, "run-audit-1", { withContent: true, now: fixedNow });

    assert.equal(bundle.predicate.replay.all_match, false);
    assert.match(bundle.predicate.replay.error ?? "", /blob: not found/);
    assert.equal(bundle.predicate.entries.length, 2);
    for (const entry of bundle.predicate.entries) {
      assert.equal(entry.content_b64, undefined);
    }
  } finally {
    await close();
  }
});
