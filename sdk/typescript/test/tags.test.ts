import { test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import type { AddressInfo } from "node:net";

import { Ctx } from "../src/index.js";
import type { DiffResult, Manifest } from "../src/index.js";

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

function readBody(req: http.IncomingMessage): Promise<string> {
  return new Promise((resolve) => {
    let body = "";
    req.on("data", (chunk) => (body += chunk));
    req.on("end", () => resolve(body));
  });
}

test("setTag PUTs the tag and returns it", async () => {
  let method: string | undefined;
  let path: string | undefined;
  let received: unknown;

  const { url, close } = await startMockServer(async (req, res) => {
    method = req.method;
    path = req.url;
    received = JSON.parse(await readBody(req));
    res.setHeader("Content-Type", "application/json");
    res.end(
      JSON.stringify({
        uri: "ctx://demo/policies/refunds",
        tag: "prod",
        snapshot_id: "snap_1",
        updated_at: "2026-08-20T09:00:00Z",
      }),
    );
  });

  try {
    const ctx = new Ctx({ endpoint: url });
    const tag = await ctx.setTag("ctx://demo/policies/refunds", "prod", "snap_1");
    assert.equal(method, "PUT");
    assert.equal(path, "/v1/tags");
    assert.deepEqual(received, { uri: "ctx://demo/policies/refunds", tag: "prod", snapshot_id: "snap_1" });
    assert.equal(tag.tag, "prod");
    assert.equal(tag.snapshot_id, "snap_1");
    assert.equal(tag.updated_at, "2026-08-20T09:00:00Z");
  } finally {
    await close();
  }
});

test("listTags unwraps the tags array", async () => {
  let path: string | undefined;
  const { url, close } = await startMockServer((req, res) => {
    path = req.url;
    res.setHeader("Content-Type", "application/json");
    res.end(
      JSON.stringify({
        tags: [
          { uri: "ctx://demo/x", tag: "baseline", snapshot_id: "snap_1", updated_at: "2026-08-19T00:00:00Z" },
          { uri: "ctx://demo/x", tag: "prod", snapshot_id: "snap_2", updated_at: "2026-08-20T00:00:00Z" },
        ],
      }),
    );
  });

  try {
    const ctx = new Ctx({ endpoint: url });
    const tags = await ctx.listTags("ctx://demo/x");
    assert.equal(path, "/v1/tags?uri=ctx%3A%2F%2Fdemo%2Fx");
    assert.equal(tags.length, 2);
    assert.equal(tags[0]?.tag, "baseline");
    assert.equal(tags[1]?.snapshot_id, "snap_2");
  } finally {
    await close();
  }
});

test("deleteTag sends DELETE with uri and tag, and tolerates 204", async () => {
  let method: string | undefined;
  let path: string | undefined;
  const { url, close } = await startMockServer((req, res) => {
    method = req.method;
    path = req.url;
    res.statusCode = 204;
    res.end();
  });

  try {
    const ctx = new Ctx({ endpoint: url });
    await ctx.deleteTag("ctx://demo/x", "prod");
    assert.equal(method, "DELETE");
    assert.equal(path, "/v1/tags?uri=ctx%3A%2F%2Fdemo%2Fx&tag=prod");
  } finally {
    await close();
  }
});

test("resolve passes a @tag URI through and surfaces use_tag freshness", async () => {
  let requestedURI: string | undefined;
  const { url, close } = await startMockServer(async (req, res) => {
    const parsed = JSON.parse(await readBody(req)) as { uri: string };
    requestedURI = parsed.uri;
    res.setHeader("Content-Type", "application/json");
    res.end(
      JSON.stringify({
        resource: { uri: "ctx://demo/policies/refunds", ref: "prod", policy: { strategy: "require_fresh" } },
        snapshot: {
          id: "snap_1",
          resource_uri: "ctx://demo/policies/refunds",
          source_revision: "rev1",
          content_hash: "sha256:abc",
          observed_at: "2026-01-01T00:00:00Z",
          created_at: "2026-01-01T00:00:00Z",
          content_type: "text/markdown",
          bytes: 5,
          provenance: { etag: '"v3"' },
        },
        materialization: {
          id: "mat_1",
          snapshot_id: "snap_1",
          strategy: "raw",
          content_hash: "sha256:abc",
          bytes: 5,
          created_at: "2026-01-01T00:00:00Z",
        },
        freshness: { status: "use_tag", age_seconds: 120 },
        content: Buffer.from("30 days", "utf-8").toString("base64"),
      }),
    );
  });

  try {
    const ctx = new Ctx({ endpoint: url });
    const result = await ctx.resolve("ctx://demo/policies/refunds@prod");
    assert.equal(requestedURI, "ctx://demo/policies/refunds@prod");
    assert.equal(result.resource.ref, "prod");
    assert.equal(result.freshness.status, "use_tag");
    assert.equal(result.content, "30 days");
    assert.equal(result.snapshot.provenance["etag"], '"v3"');
  } finally {
    await close();
  }
});

test("run.mount accepts a @tag URI and the manifest entry carries the ref", async () => {
  const { url, close } = await startMockServer(async (req, res) => {
    const body = await readBody(req);
    res.setHeader("Content-Type", "application/json");

    if (req.url === "/v1/runs") {
      res.statusCode = 204;
      res.end();
      return;
    }
    if (req.url === "/v1/runs/mount") {
      const parsed = JSON.parse(body) as { uri: string };
      assert.equal(parsed.uri, "ctx://demo/policies/refunds@prod");
      res.end(
        JSON.stringify({
          position: 0,
          resolve: {
            resource: { uri: "ctx://demo/policies/refunds", ref: "prod", policy: { strategy: "require_fresh" } },
            snapshot: {
              id: "snap_1", resource_uri: "ctx://demo/policies/refunds", source_revision: "rev1",
              content_hash: "sha256:abc", observed_at: "2026-01-01T00:00:00Z",
              created_at: "2026-01-01T00:00:00Z", content_type: "text/markdown", bytes: 1, provenance: {},
            },
            materialization: {
              id: "mat_1", snapshot_id: "snap_1", strategy: "raw",
              content_hash: "sha256:abc", bytes: 1, created_at: "2026-01-01T00:00:00Z",
            },
            freshness: { status: "use_tag", age_seconds: 0 },
            content: Buffer.from("30 days").toString("base64"),
          },
        }),
      );
      return;
    }
    if (req.url === "/v1/runs/commit") {
      const manifest: Manifest = {
        manifest_id: "manifest_1",
        run_id: "run-c",
        created_at: "2026-01-01T00:00:00Z",
        entries: [
          {
            position: 0,
            uri: "ctx://demo/policies/refunds",
            ref: "prod",
            snapshot_id: "snap_1",
            materialization_id: "mat_1",
            content_hash: "sha256:abc",
          },
        ],
      };
      res.end(JSON.stringify(manifest));
      return;
    }
    res.statusCode = 404;
    res.end();
  });

  try {
    const ctx = new Ctx({ endpoint: url });
    const run = ctx.run({ id: "run-c" });
    const mounted = await run.mount("ctx://demo/policies/refunds@prod");
    assert.equal(mounted.resource.ref, "prod");
    assert.equal(mounted.freshness.status, "use_tag");

    const manifest = await run.commit();
    assert.equal(manifest.entries[0]?.ref, "prod");
    assert.equal(manifest.entries[0]?.uri, "ctx://demo/policies/refunds");
  } finally {
    await close();
  }
});

test("diff entries carry per-side provenance", async () => {
  const { url, close } = await startMockServer((_req, res) => {
    const body: DiffResult = {
      manifest_a: { manifest_id: "m_a", run_id: "run-a", created_at: "2026-01-01T00:00:00Z", entries: [] },
      manifest_b: { manifest_id: "m_b", run_id: "run-b", created_at: "2026-01-02T00:00:00Z", entries: [] },
      entries: [
        {
          uri: "ctx://demo/policies/refunds",
          status: "changed",
          snapshot_id_a: "snap_1",
          snapshot_id_b: "snap_2",
          source_revision_a: "8af92d1",
          source_revision_b: "c31be07",
          observed_at_a: "2026-08-19T16:05:30Z",
          observed_at_b: "2026-08-20T09:00:00Z",
          ref_b: "prod",
          unified_diff: "--- a\n+++ b\n",
        },
      ],
    };
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify(body));
  });

  try {
    const ctx = new Ctx({ endpoint: url });
    const result = await ctx.diff("run-a", "run-b");
    const entry = result.entries[0];
    assert.equal(entry?.source_revision_a, "8af92d1");
    assert.equal(entry?.source_revision_b, "c31be07");
    assert.equal(entry?.observed_at_a, "2026-08-19T16:05:30Z");
    assert.equal(entry?.ref_b, "prod");
    assert.equal(entry?.ref_a, undefined);
  } finally {
    await close();
  }
});
