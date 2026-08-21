// RP-19 / RP-20 / RP-24: the SDK's failure modes, not its happy path.
//
// A hung readproofd used to stall the caller indefinitely (fetch has no
// default timeout), an unbounded body was buffered whole and then interpolated
// into an error the model reads, and an endpoint was never validated.

import { test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import type { AddressInfo } from "node:net";

import { Readproof, ReadproofError } from "../src/index.js";

function startMockServer(handler: http.RequestListener): Promise<{ url: string; close: () => Promise<void> }> {
  return new Promise((resolve) => {
    const server = http.createServer(handler);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address() as AddressInfo;
      resolve({
        url: `http://127.0.0.1:${address.port}`,
        // destroy(), not close(): a test that leaves a socket hanging on
        // purpose would otherwise keep close() waiting for it.
        close: () =>
          new Promise((res) => {
            server.closeAllConnections();
            server.close(() => res());
          }),
      });
    });
  });
}

test("the constructor rejects an endpoint that is not an http(s) URL", () => {
  for (const endpoint of ["", "localhost:8080", "/v1", "file:///etc/passwd", "data:text/plain,x", "ftp://x/y"]) {
    assert.throws(
      () => new Readproof({ endpoint }),
      (err: unknown) => err instanceof ReadproofError && /invalid endpoint/.test((err as Error).message),
      `endpoint ${JSON.stringify(endpoint)} was accepted`,
    );
  }
  // …and still accepts the ordinary ones, trailing slashes and all.
  assert.doesNotThrow(() => new Readproof({ endpoint: "http://localhost:8080/" }));
  assert.doesNotThrow(() => new Readproof({ endpoint: "https://readproofd.internal" }));
});

test("a request that never answers is aborted by the timeout", async () => {
  const { url, close } = await startMockServer(() => {
    // Accept the connection and never respond.
  });

  try {
    const rp = new Readproof({ endpoint: url, timeoutMs: 150 });
    const started = Date.now();
    await assert.rejects(rp.listResources(), (err: unknown) => {
      assert.ok(err instanceof ReadproofError, `expected a ReadproofError, got ${String(err)}`);
      assert.match((err as Error).message, /timed out after 150ms/);
      return true;
    });
    assert.ok(Date.now() - started < 5_000, "the timeout did not apply");
  } finally {
    await close();
  }
});

test("a response over the size cap is refused rather than buffered", async () => {
  const { url, close } = await startMockServer((_req, res) => {
    res.setHeader("Content-Type", "application/json");
    // Chunked, so there is no Content-Length to short-circuit on: the cap has
    // to hold while the body streams in.
    res.write('{"resources":[');
    for (let i = 0; i < 64; i++) res.write(`"${"x".repeat(1024)}",`);
    res.end('""]}');
  });

  try {
    const rp = new Readproof({ endpoint: url, maxResponseBytes: 4096 });
    await assert.rejects(rp.listResources(), (err: unknown) => {
      assert.ok(err instanceof ReadproofError);
      assert.match((err as Error).message, /exceeds the 4096-byte limit/);
      return true;
    });
  } finally {
    await close();
  }
});

test("a declared Content-Length over the cap is refused before the body is read", async () => {
  const big = JSON.stringify({ resources: [] }).padEnd(8192, " ");
  const { url, close } = await startMockServer((_req, res) => {
    res.setHeader("Content-Type", "application/json");
    res.setHeader("Content-Length", String(Buffer.byteLength(big)));
    res.end(big);
  });

  try {
    const rp = new Readproof({ endpoint: url, maxResponseBytes: 1024 });
    await assert.rejects(rp.listResources(), /exceeds the 1024-byte limit/);
  } finally {
    await close();
  }
});

test("an error message truncates the body it echoes", async () => {
  const noise = "N".repeat(5000);
  const { url, close } = await startMockServer((_req, res) => {
    res.statusCode = 500;
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify({ error: noise }));
  });

  try {
    const rp = new Readproof({ endpoint: url });
    await assert.rejects(rp.listResources(), (err: unknown) => {
      const message = (err as Error).message;
      assert.ok(message.length < 800, `error message is ${message.length} chars: not truncated`);
      assert.match(message, /truncated/);
      return true;
    });
  } finally {
    await close();
  }
});

test("a non-JSON error body is truncated too, not pasted in whole", async () => {
  const { url, close } = await startMockServer((_req, res) => {
    res.statusCode = 502;
    res.setHeader("Content-Type", "text/html");
    res.end(`<html><body>${"gateway ".repeat(2000)}</body></html>`);
  });

  try {
    const rp = new Readproof({ endpoint: url });
    await assert.rejects(rp.listResources(), (err: unknown) => {
      const message = (err as Error).message;
      assert.ok(message.length < 800, `error message is ${message.length} chars: not truncated`);
      assert.match(message, /invalid JSON response/);
      return true;
    });
  } finally {
    await close();
  }
});
