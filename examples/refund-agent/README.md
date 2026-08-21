# Refund Agent — Reference Demo

This is Readproof's canonical demo (spec §42). It proves the core invariant:

```
SHA256(original materialization) == SHA256(replayed materialization)
```

...even after the underlying policy document has changed and the manifest
is replayed with no re-fetch from the live source.

Run this from the repository root, after building the CLI:

```bash
go build -o readproof ./cmd/readproof
```

## Walkthrough

```bash
# 1. Register the resource
./readproof resource add readproof://demo/policies/refunds \
  --source-type filesystem \
  --path examples/refund-agent/policies/refunds.md \
  --policy require_fresh

# 2. Resolve it directly — creates the first snapshot
./readproof get readproof://demo/policies/refunds
# -> "Products can be refunded within 30 days."

# 3. Simulate an agent run: mount + commit -> manifest for run-a
./readproof run --id run-a readproof://demo/policies/refunds
# NOTE: require_fresh re-verifies against the source on every resolve, so
# this creates a NEW snapshot row — but since the file hasn't changed since
# step 2, it dedupes to the SAME content_hash/blob. Run
#   ./readproof history readproof://demo/policies/refunds
# to see two snapshot rows sharing one content hash.

# 3b. Freeze what run-a saw behind a movable `prod` tag. Copy the snapshot
#     id from `readproof manifest run-a` (the SNAPSHOT column):
./readproof manifest run-a
./readproof tag set readproof://demo/policies/refunds prod <snapshot-id-from-run-a>
./readproof tag list readproof://demo/policies/refunds
# `readproof history` now shows that snapshot carrying the `prod` tag.

# 4. Edit the source
printf 'Products can be refunded within 14 days.\n' > examples/refund-agent/policies/refunds.md

# 5. Resolve again in a new run -> manifest for run-b, new content_hash
./readproof run --id run-b readproof://demo/policies/refunds

# 6. Diff the two manifests
./readproof diff run-a run-b
# -> a "why:" line (source revision + observed timestamps per side),
#    then the "30 days" -> "14 days" unified diff

# 6b. A run pinned to the tag still gets the OLD bytes — no fetch, and the
#     require_fresh policy is not consulted
./readproof get readproof://demo/policies/refunds@prod
# -> "Products can be refunded within 30 days."  (freshness: tagged)
./readproof run --id run-c readproof://demo/policies/refunds@prod
./readproof manifest run-c
# -> the entry records the bare uri plus REF=prod
./readproof replay run-c
# -> "Replay verified: SHA256 match for 1/1 entries."

# 7. Replay run-a and verify the SHA256 invariant, without touching the
#    (now-changed) live source file at all
./readproof replay run-a
# -> "Products can be refunded within 30 days."
# -> "Replay verified: SHA256 match for 1/1 entries."
#    (replay is strict: any hash mismatch or missing blob exits non-zero)
```

To restore the fixture after running the walkthrough:

```bash
printf 'Products can be refunded within 30 days.\n' > examples/refund-agent/policies/refunds.md
```

The automated version of this exact scenario lives in
`internal/e2e/demo_test.go` and runs as part of `go test ./...`.
