# Refund Agent — Reference Demo

This is Ctx's canonical demo (spec §42). It proves the core invariant:

```
SHA256(original materialization) == SHA256(replayed materialization)
```

...even after the underlying policy document has changed and the manifest
is replayed with no re-fetch from the live source.

Run this from the repository root, after building the CLI:

```bash
go build -o ctx ./cmd/ctx
```

## Walkthrough

```bash
# 1. Register the resource
./ctx resource add ctx://demo/policies/refunds \
  --source-type filesystem \
  --path examples/refund-agent/policies/refunds.md \
  --policy require_fresh

# 2. Resolve it directly — creates the first snapshot
./ctx get ctx://demo/policies/refunds
# -> "Products can be refunded within 30 days."

# 3. Simulate an agent run: mount + commit -> manifest for run-a
./ctx run --id run-a ctx://demo/policies/refunds
# NOTE: require_fresh re-verifies against the source on every resolve, so
# this creates a NEW snapshot row — but since the file hasn't changed since
# step 2, it dedupes to the SAME content_hash/blob. Run
#   ./ctx history ctx://demo/policies/refunds
# to see two snapshot rows sharing one content hash.

# 4. Edit the source
printf 'Products can be refunded within 14 days.\n' > examples/refund-agent/policies/refunds.md

# 5. Resolve again in a new run -> manifest for run-b, new content_hash
./ctx run --id run-b ctx://demo/policies/refunds

# 6. Diff the two manifests
./ctx diff run-a run-b
# -> shows the "30 days" -> "14 days" unified diff

# 7. Replay run-a and verify the SHA256 invariant, without touching the
#    (now-changed) live source file at all
./ctx replay run-a
# -> "Products can be refunded within 30 days."
# -> "Replay verified: SHA256 match for 1/1 entries."
```

To restore the fixture after running the walkthrough:

```bash
printf 'Products can be refunded within 30 days.\n' > examples/refund-agent/policies/refunds.md
```

The automated version of this exact scenario lives in
`internal/e2e/demo_test.go` and runs as part of `go test ./...`.
