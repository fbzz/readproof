# Launch copy

Everything that gets posted, in one place, so the sentences stay the same
everywhere. The checklist at the bottom is the order of operations on the
day.

Ground rules for all of it: no adjective the terminal cannot back up, the
caveats stated plainly rather than buried, and the ask is about the **model**
— manifest, tag, evidence — not about features.

---

## Show HN

### Title

```
Show HN: Readproof – a lockfile and byte-exact replay for what AI agents read
```

77 characters. Alternatives, if that one reads long:

```
Show HN: Readproof – pin, diff and replay the documents your agent reads
Show HN: A lockfile for the documents your AI agent reads
```

### Body

> Readproof gives every document an AI agent reads a stable identity, a
> freshness policy, and a content-addressed snapshot — and records each run
> as a manifest you can diff, replay byte for byte without touching the live
> source, and hand over as evidence.
>
> The whole idea, in one terminal:
>
>     $ readproof run --id run-a readproof://demo/policies/refunds
>     $ printf 'Products can be refunded within 14 days.\n' > policies/refunds.md
>     $ readproof run --id run-b readproof://demo/policies/refunds
>
>     $ readproof diff run-a run-b
>     ~ readproof://demo/policies/refunds  (snap_01M0GQH8K6… -> snap_01M0GQHP7V…)
>       why: source revision sha256:c8b0bb212e93 → sha256:8f4b00474456
>       -Products can be refunded within 30 days.
>       +Products can be refunded within 14 days.
>
>     $ readproof replay run-a          # reads the store, never the file
>     Products can be refunded within 30 days.
>     Replay verified: SHA256 match for 1/1 entries.
>
> Models are probabilistic, but much context failure is infrastructural.
> Three failures I kept hitting: **"it worked on Tuesday"** — a document
> changed and the agent quietly answered from a different version; **"can
> you rerun exactly that?"** — traces keep strings, not bytes; **"what data
> did the agent consider?"** — what an EU AI Act Art. 12 log asks for.
>
> Today: a Go CLI and server, 13 MCP tools, 3 source adapters (filesystem,
> GitHub, HTTP), 2 storage backends (embedded SQLite, or Postgres + S3), a
> zero-dependency TypeScript SDK, a DeepSeek Harness plugin, and a
> support-agent example driven by a local model on Ollama. Apache-2.0.
>
> Sixty seconds:
>
>     brew install fbzz/tap/readproof    # or go install github.com/fbzz/readproof/cmd/readproof@latest
>     git clone https://github.com/fbzz/readproof && cd readproof/examples/refund-agent
>
> Not a vector database, an observability tool, a prompt registry, or a
> memory system: it sits underneath those and makes their inputs
> reproducible.
>
> Caveats, up front. The HTTP source adapter has no SSRF allow-list yet —
> fine while only the operator registers resources, not fine once anyone
> else can; on the roadmap. Evidence bundles are valid in-toto Statements
> but are **not signed**: integrity against the Merkle root and the store,
> not authorship; signing is on the roadmap too. And this is pre-1.0 — CLI,
> API and bundle shape can still change.
>
> What I would most like picked apart is the **model**, not the feature
> list. Is *one manifest per run* the right unit? Does a movable **tag**
> (`uri@prod`, promotion as one recorded pointer move) match how you promote
> a reviewed document? Would an *unsigned* evidence bundle mean anything to
> your auditors?

About 295 words of prose, terminal blocks aside. If it needs trimming,
cut the "sixty seconds" block before you cut the caveats.

### Notes for answering comments

- **"Isn't this just git / DVC / a lockfile?"** — a lockfile pins an agent's
  *static* configuration at install time. Readproof pins the *runtime
  documents, per run*, including ones that live behind an HTTP endpoint or a
  GitHub API and have no commit of their own.
- **"Why not just log the prompt?"** — a logged string cannot be replayed
  and re-hashed. `readproof replay` rebuilds the inputs from the
  content-addressed store and exits non-zero on any mismatch.
- **"Vendor lock-in?"** — Apache-2.0, one binary, a local SQLite file. The
  evidence bundle is an in-toto Statement and `verify --offline` needs no
  server.
- **SSRF and signing questions** — point at [`SECURITY.md`](../SECURITY.md)
  and [`docs/roadmap.md`](roadmap.md), and do not oversell the timeline.

---

## Three-post thread (X / LinkedIn)

**1 — the hook**

> Your agent read a policy document on Tuesday and answered correctly.
>
> It read the "same" document on Thursday and answered differently.
>
> Nothing in your traces can tell you which bytes it actually saw — or hand
> them back to you.
>
> That is not a model problem. That is a missing primitive.

**2 — the terminal**

> Readproof gives every document an agent reads an identity, a freshness
> policy, and a content-addressed snapshot. Every run becomes a manifest.
>
>     $ readproof diff run-a run-b
>     ~ readproof://demo/policies/refunds
>       why: source revision sha256:c8b0bb212e93 → sha256:8f4b00474456
>       -Products can be refunded within 30 days.
>       +Products can be refunded within 14 days.
>
>     $ readproof replay run-a          # from the store, never the source
>     Replay verified: SHA256 match for 1/1 entries.
>
> `SHA256(original) == SHA256(replay)` is a test in the repo, not a slogan.

**3 — the ask**

> Open source, Apache-2.0: Go CLI + server, 13 MCP tools, a TypeScript SDK,
> evidence bundles as in-toto Statements.
>
> Pre-1.0, and the shape is still negotiable — which is exactly why I am
> posting now.
>
> Is *one manifest per run* the right unit? Does a movable `@prod` tag match
> how you promote a reviewed document? Would an unsigned evidence bundle be
> worth anything to your auditors?
>
> → github.com/fbzz/readproof

---

## One-liners

Use these verbatim. Improvised variants are how a project ends up describing
itself three different ways on three different pages.

**Primary** (README lead, site hero, repo description):

> Readproof gives every document an AI agent reads a stable identity, a
> freshness policy, and a content-addressed snapshot — and records every run
> as a manifest you can diff, replay byte for byte without touching the live
> source, and hand over as evidence.

**Short** (GitHub "About", social bio):

> The lockfile and replay primitive for what AI agents read.

**Positioning** (whenever someone asks "so what is it, exactly?"):

> Not a vector database, not an observability tool, not a prompt registry,
> not a memory system. It sits underneath those and makes their inputs
> reproducible.

**The claim** (any time a demo is shown):

> `SHA256(original) == SHA256(replay)` — asserted in the test suite over
> SQLite, over Postgres + MinIO, and over a real HTTP round trip.

**Package descriptions** (Homebrew cask, MCP registry — ≤100 characters):

> Governed, versioned, replayable document reads for agents: snapshots,
> manifests, diff, evidence.

---

## Launch-day checklist

In order. None of it is cheaply reversible, so do one thing at a time and
check it before the next.

### 1. Flip the repository public

`fbzz/readproof` → Settings → General → Danger Zone → Change visibility →
Public.

This is **first** because everything else depends on it: npm provenance,
`go install` through the module proxy, the Homebrew cask, and the MCP
registry all fail against a private repository.

While in Settings: turn on **Discussions** (the issue chooser and
CONTRIBUTING both link there) and confirm **private vulnerability
reporting** is enabled — SECURITY.md and CODE_OF_CONDUCT.md both send people
to it.

Set the repo description to the short one-liner above, set the website to
`https://fbzz.github.io/readproof/`, and add topics: `mcp`, `mcp-server`,
`ai-agents`, `llm`, `provenance`, `evidence`, `in-toto`, `reproducibility`,
`golang`, `dsh-plugin`.

### 2. Drop `noindex` from the site

Three pages carry `<meta name="robots" content="noindex">` and should lose
it:

- `site/index.html`
- `site/docs/index.html`
- `site/examples/support-agent/index.html`

**`site/404.html` keeps its `noindex`** — a 404 has no business in an index.

Push to `main`; `.github/workflows/pages.yml` redeploys on any change under
`site/`. Confirm <https://fbzz.github.io/readproof/> renders before moving
on.

### 3. Tag the release

Follow [`docs/releasing.md`](releasing.md): version bumped in all four
places, CHANGELOG dated, everything green locally, then

```bash
git tag -a v0.3.1 -m "readproof v0.3.1"
git push origin v0.3.1
```

`HOMEBREW_TAP_GITHUB_TOKEN` and `NPM_TOKEN` must be in repository secrets
first, and the `@readproof` npm org must exist. All three are one-time owner
steps, documented in `releasing.md`.

### 4. Watch both workflows

```bash
gh run watch
```

- **Release** → a GitHub Release with 6 archives and `checksums.txt`, plus a
  commit in `fbzz/homebrew-tap`. A missing tap commit means the token secret
  is absent and `skip_upload` did its job: add the secret and re-run.
- **Publish npm packages** → `@readproof/sdk`, then `dsh-plugin-readproof`,
  each with a provenance attestation visible on npmjs.com.

### 5. Verify every install path yourself

```bash
go install github.com/fbzz/readproof/cmd/readproof@latest && readproof version
brew update && brew install fbzz/tap/readproof && readproof version
npm view @readproof/sdk version
npm view dsh-plugin-readproof version
```

Then the one that actually matters: in a scratch directory, run the README
Quickstart end to end against the *installed* binary — `resource add`, `run`,
edit the file, `run`, `diff`, `replay`, `evidence export`, `evidence verify`.
A broken Quickstart on launch day costs more than a late post does.

### 6. Publish the MCP registry entry

`integrations/mcp-registry/README.md` — `mcp-publisher validate`, then
`login github`, then `publish`. Needs step 1 done.

### 7. Post

Show HN first, morning US Eastern; then the thread, once the HN post exists
so it can link to it. Then stay at the keyboard — the first two hours of
comments are the entire reason for posting.
