---
name: readproof
description: Use Readproof whenever a task reads external documents that must be reproducible later — policies, runbooks, price tables, specs, anything fetched from a file, GitHub, or a URL. It gives each document a stable readproof:// identity and a freshness policy, records what a task actually read as an immutable manifest, and lets anyone diff, replay byte-for-byte, or export evidence for that task afterwards.
---

# Readproof — record exactly what you read

## When to use this
- The task reads a document (policy, contract, runbook, config, spec) that a
  human may later ask about: "what did the agent see?", "why did the answer
  change?", "can you rerun exactly that?".
- The task's output must be auditable or reproducible (support decisions,
  compliance, billing, anything regulated).
- You are about to paste a document into your own context: mount it through
  Readproof instead, so the bytes are recorded by hash.

## Setup (once per machine)
```bash
# binary: brew install fbzz/tap/readproof   or
go install github.com/fbzz/readproof/cmd/readproof@latest
export READPROOF_HOME=~/.readproof            # embedded store, no services
# shared server instead: export READPROOF_SERVER_URL=http://host:8080  (+ READPROOF_API_KEY)
```
If the MCP server is available (`readproof mcp`), prefer its tools
(`readproof_resolve`, `readproof_run_*`, `readproof_diff`, `readproof_replay`,
`readproof_evidence_export`, `readproof_tag_*`) over shelling out.

## Register a document (once per document)
```bash
readproof resource add readproof://<ns>/<path> --source-type filesystem --path /abs/file.md --policy require_fresh
readproof resource add readproof://<ns>/<path> --source-type github --owner O --repo R --path docs/x.md --ref main --policy allow_stale --max-age 1h
readproof resource add readproof://<ns>/<path> --source-type http --url https://… --header 'Authorization: Bearer ${TOKEN}' --policy require_fresh
```
Policies: `require_fresh` (re-verify every read), `allow_stale --max-age`
(reuse within a TTL). To pin a reviewed version, tag it and read `@tag`.

## The rule for every task that reads documents
1. Open one run per task: `readproof run start <task-id>`.
2. Read every document through the run, never directly:
   `readproof run mount <task-id> readproof://<ns>/<path>[@prod]`
   The command prints the bytes — use exactly those bytes in your reasoning.
3. Finish: `readproof run commit <task-id>` → prints a **manifest id**.
4. Put the manifest id in your output / ticket / PR description
   (`readproof manifest: manifest_…`). That id is what makes the task
   reproducible.
Single shot: `readproof run --id <task-id> <uri> <uri>…` does 1–3 at once.

## Answering "why did it change?" / "show me what it read"
```bash
readproof diff <task-a> <task-b>        # which document moved; why: source revision + observed time
readproof replay <task-id>              # the exact bytes, from the store, never the live source
readproof evidence export <task-id> --with-content --out bundle.json
readproof evidence verify bundle.json   # Merkle root + re-hash + store cross-check; non-zero on tamper
```

## Promotion with tags
```bash
readproof history readproof://<ns>/<path>            # snapshots + tags
readproof tag set readproof://<ns>/<path> prod <snapshot-id>
readproof run mount <task-id> readproof://<ns>/<path>@prod   # exactly that snapshot, no fetch
```
Never edit files to "deploy" a document; move the tag.

## Do not
- Do not bypass the run (read the file directly) for documents that matter.
- Do not paste secrets into resource definitions; use `${ENV_VAR}` headers.
- Do not delete or rewrite anything under the data directory; it is the record.
