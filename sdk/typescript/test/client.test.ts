import { test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import type { AddressInfo } from "node:net";

import { Ctx, CtxError } from "../src/index.js";

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

test("resolve decodes base64 content and typed fields", async () => {
  const { url, close } = await startMockServer(async (req, res) => {
    assert.equal(req.method, "POST");
    assert.equal(req.url, "/v1/resolve");
    const parsed = JSON.parse(await readBody(req));
    assert.equal(parsed.uri, "ctx://demo/policies/refunds");

    res.setHeader("Content-Type", "application/json");
    res.end(
      JSON.stringify({
        resource: { uri: parsed.uri, policy: { strategy: "require_fresh" } },
        snapshot: {
          id: "snap_1",
          resource_uri: parsed.uri,
          source_revision: "rev1",
          content_hash: "sha256:abc",
          observed_at: "2026-01-01T00:00:00Z",
          created_at: "2026-01-01T00:00:00Z",
          content_type: "text/markdown",
          bytes: 5,
          provenance: {},
        },
        materialization: {
          id: "mat_1",
          snapshot_id: "snap_1",
          strategy: "raw",
          content_hash: "sha256:abc",
          bytes: 5,
          created_at: "2026-01-01T00:00:00Z",
        },
        freshness: { status: "fetch", age_seconds: 0 },
        content: Buffer.from("hello", "utf-8").toString("base64"),
      }),
    );
  });

  try {
    const ctx = new Ctx({ endpoint: url });
    const result = await ctx.resolve("ctx://demo/policies/refunds");
    assert.equal(result.content, "hello");
    assert.equal(result.snapshot.id, "snap_1");
    assert.equal(result.freshness.status, "fetch");
    assert.equal(result.resource.policy.strategy, "require_fresh");
  } finally {
    await close();
  }
});

test("run.mount starts the run then mounts, commit produces a manifest", async () => {
  const calls: string[] = [];
  const { url, close } = await startMockServer(async (req, res) => {
    const body = await readBody(req);
    calls.push(`${req.method} ${req.url}`);
    res.setHeader("Content-Type", "application/json");

    if (req.url === "/v1/runs" && req.method === "POST") {
      res.statusCode = 204;
      res.end();
      return;
    }
    if (req.url === "/v1/runs/mount" && req.method === "POST") {
      const parsed = JSON.parse(body);
      res.end(
        JSON.stringify({
          position: 0,
          resolve: {
            resource: { uri: parsed.uri, policy: { strategy: "require_fresh" } },
            snapshot: {
              id: "snap_1",
              resource_uri: parsed.uri,
              source_revision: "r",
              content_hash: "sha256:abc",
              observed_at: "2026-01-01T00:00:00Z",
              created_at: "2026-01-01T00:00:00Z",
              content_type: "text/plain",
              bytes: 1,
              provenance: {},
            },
            materialization: {
              id: "mat_1",
              snapshot_id: "snap_1",
              strategy: "raw",
              content_hash: "sha256:abc",
              bytes: 1,
              created_at: "2026-01-01T00:00:00Z",
            },
            freshness: { status: "fetch", age_seconds: 0 },
            content: Buffer.from("x").toString("base64"),
          },
        }),
      );
      return;
    }
    if (req.url === "/v1/runs/commit" && req.method === "POST") {
      res.end(
        JSON.stringify({
          manifest_id: "manifest_1",
          run_id: "run-a",
          created_at: "2026-01-01T00:00:00Z",
          entries: [{ position: 0, uri: "ctx://demo/x", snapshot_id: "snap_1", materialization_id: "mat_1", content_hash: "sha256:abc" }],
        }),
      );
      return;
    }
    res.statusCode = 404;
    res.end();
  });

  try {
    const ctx = new Ctx({ endpoint: url });
    const run = ctx.run({ id: "run-a" });
    const mounted = await run.mount("ctx://demo/x");
    assert.equal(mounted.content, "x");

    const manifest = await run.commit();
    assert.equal(manifest.manifest_id, "manifest_1");
    assert.equal(manifest.entries.length, 1);
    assert.equal(manifest.entries[0]?.uri, "ctx://demo/x");

    assert.deepEqual(calls, ["POST /v1/runs", "POST /v1/runs/mount", "POST /v1/runs/commit"]);
  } finally {
    await close();
  }
});

test("run.mount only starts the run once across multiple mounts", async () => {
  let startCalls = 0;
  const { url, close } = await startMockServer(async (req, res) => {
    await readBody(req);
    res.setHeader("Content-Type", "application/json");
    if (req.url === "/v1/runs" && req.method === "POST") {
      startCalls++;
      res.statusCode = 204;
      res.end();
      return;
    }
    if (req.url === "/v1/runs/mount" && req.method === "POST") {
      res.end(
        JSON.stringify({
          position: 0,
          resolve: {
            resource: { uri: "ctx://demo/x", policy: { strategy: "require_fresh" } },
            snapshot: {
              id: "snap_1", resource_uri: "ctx://demo/x", source_revision: "r",
              content_hash: "sha256:abc", observed_at: "2026-01-01T00:00:00Z",
              created_at: "2026-01-01T00:00:00Z", content_type: "text/plain", bytes: 1, provenance: {},
            },
            materialization: {
              id: "mat_1", snapshot_id: "snap_1", strategy: "raw",
              content_hash: "sha256:abc", bytes: 1, created_at: "2026-01-01T00:00:00Z",
            },
            freshness: { status: "fetch", age_seconds: 0 },
            content: Buffer.from("x").toString("base64"),
          },
        }),
      );
      return;
    }
    res.statusCode = 404;
    res.end();
  });

  try {
    const ctx = new Ctx({ endpoint: url });
    const run = ctx.run({ id: "run-b" });
    await run.mount("ctx://demo/x");
    await run.mount("ctx://demo/x");
    assert.equal(startCalls, 1);
  } finally {
    await close();
  }
});

test("non-2xx responses throw CtxError with the server's error message", async () => {
  const { url, close } = await startMockServer((_req, res) => {
    res.statusCode = 404;
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify({ error: "resource: not found: ctx://demo/missing" }));
  });

  try {
    const ctx = new Ctx({ endpoint: url });
    await assert.rejects(
      () => ctx.getResource("ctx://demo/missing"),
      (err: unknown) => {
        assert.ok(err instanceof CtxError);
        assert.equal(err.status, 404);
        assert.match(err.message, /resource: not found/);
        return true;
      },
    );
  } finally {
    await close();
  }
});
