# Releasing

Cutting a release is **pushing an annotated `vX.Y.Z` tag**. Two workflows
watch for it:

| Workflow | What it produces |
| --- | --- |
| [`release.yml`](../.github/workflows/release.yml) → [`.goreleaser.yaml`](../.goreleaser.yaml) | Cross-compiled `readproof` + `readproofd` archives for linux/darwin/windows × amd64/arm64, `checksums.txt`, a grouped changelog, a GitHub Release, and the Homebrew cask in [`fbzz/homebrew-tap`](https://github.com/fbzz/homebrew-tap) |
| [`publish-npm.yml`](../.github/workflows/publish-npm.yml) | `@readproof/sdk` then `dsh-plugin-readproof` on npm, both with Sigstore provenance |

Everything else — the version in the binaries, the version in the packages,
the CHANGELOG heading — is set **before** the tag, by hand, in one commit.
The tag is the last thing that happens.

> **The existing `v0.3.0` tag predates the module path move.** At that tag
> `go.mod` still says `module readproof`, so
> `go install github.com/fbzz/readproof/cmd/readproof@v0.3.0` fails with a
> module-path mismatch and no amount of proxy patience fixes it. The first
> tag cut *after* that change — `v0.3.1` in the examples below — is the
> first installable one. Do not retag `v0.3.0`; the proxy caches tags
> immutably.

## One-time setup

These are owner actions. None of them is done yet; each is listed with the
exact commands.

### 1. Make the repository public

`go install`, `brew install`, npm provenance, and the MCP registry all
require it. Until it flips, the release workflow still runs and still
produces artifacts, but nobody outside the org can fetch them.

### 2. `HOMEBREW_TAP_GITHUB_TOKEN`

The cask is committed to `fbzz/homebrew-tap`, a **different repository**
than the one running the workflow. `secrets.GITHUB_TOKEN` is scoped to
`fbzz/readproof` alone and cannot write there, so GoReleaser needs a token
of its own:

1. GitHub → Settings → Developer settings → Personal access tokens →
   **Tokens (classic)** → Generate new token.
2. Scope: **`repo`** (a fine-grained token with Contents: read & write on
   `fbzz/homebrew-tap` works too and is tighter).
3. Copy it into `fbzz/readproof` → Settings → Secrets and variables →
   Actions → New repository secret, named `HOMEBREW_TAP_GITHUB_TOKEN`.

Until that secret exists the release still succeeds: `.goreleaser.yaml`
sets

```yaml
skip_upload: '{{ if .Env.HOMEBREW_TAP_GITHUB_TOKEN }}false{{ else }}true{{ end }}'
```

so an empty token turns the tap push into a no-op instead of a failed
release. The cask is still generated into `dist/` and can be committed to
the tap by hand.

### 3. The npm `@readproof` org and `NPM_TOKEN`

The `@readproof` scope **does not exist yet**. Create it and mint a token:

```bash
npm login                                  # the account that will own the scope
npm org create readproof                   # or create it at npmjs.com/org/create
npm access ls-packages readproof           # sanity check: empty, but resolves
```

Then npmjs.com → avatar → Access Tokens → Generate New Token →
**Granular Access Token** with *Read and write* on `@readproof/*` and on
`dsh-plugin-readproof`, or a classic **Automation** token. Add it to
`fbzz/readproof` → Settings → Secrets and variables → Actions as
`NPM_TOKEN`.

> Provenance (`npm publish --provenance`) requires a **public** repository
> and the `id-token: write` permission the workflow already declares. On a
> private repo the publish step fails; that is the third reason the repo has
> to be public before the first tag.

If you would rather publish the first version by hand instead of through
the workflow — reasonable, since it is also what proves the org exists:

```bash
cd sdk/typescript && npm ci && npm run build && npm test
npm publish --access public                # prepublishOnly re-runs build + test

cd ../../integrations/deepseek-harness/dsh-plugin-readproof
npm ci && npm run build && npm test
npm publish --access public                # prepack rewrites the file: SDK dep
```

The tag-driven workflow skips any version already on the registry, so a
manual first publish does not break the automated one.

## Cutting a release

### 1. Bump the version in all four places

They must agree. The Homebrew cask's smoke test compares the cask version
against `readproof version`, so a mismatch between the tag and
`internal/version` fails the tap install rather than shipping quietly.

| File | Field |
| --- | --- |
| [`internal/version/version.go`](../internal/version/version.go) | `const Version` |
| [`sdk/typescript/package.json`](../sdk/typescript/package.json) | `version` |
| [`integrations/deepseek-harness/dsh-plugin-readproof/package.json`](../integrations/deepseek-harness/dsh-plugin-readproof/package.json) | `version` |
| [`integrations/mcp-registry/server.json`](../integrations/mcp-registry/server.json) | `version` and each `packages[].version` |

The plugin's dependency on the SDK needs no bump: `scripts/prepack.mjs`
reads the SDK's version at pack time and writes `^<version>` into the
tarball.

Refresh the lockfiles so `npm ci` still matches — the consumers record the
linked SDK's version too:

```bash
for d in sdk/typescript \
         integrations/deepseek-harness/dsh-plugin-readproof \
         examples/langgraph-ts examples/support-agent; do
  (cd "$d" && npm install --package-lock-only)
done
```

### 2. Date the CHANGELOG

Move the `## Unreleased` entries under `## X.Y.Z — YYYY-MM-DD`. GoReleaser
writes its own commit-derived changelog into the release body; the
CHANGELOG is the human one, and the release footer links to it.

Also update the `release-vX.Y.Z` badge in [`README.md`](../README.md) and
the "Status" section's version.

### 3. Verify locally

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./...

(cd sdk/typescript && npm ci && npm run build && npm test)
(cd integrations/deepseek-harness/dsh-plugin-readproof && npm ci && npm run build && npm test)
(cd examples/langgraph-ts && npm ci && npm run build)
(cd examples/support-agent && npm ci && npm run build && SUPPORT_FAKE_MODEL=1 npm test)

go run github.com/goreleaser/goreleaser/v2@latest check
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean --skip=publish
./dist/readproof_darwin_arm64_v8.0/readproof version      # adjust for your platform
rm -rf dist
```

`--snapshot` builds everything and writes the cask to
`dist/homebrew/Casks/readproof.rb` without touching GitHub or the tap, so it
is safe to run any time.

### 4. Commit, tag, push

```bash
git commit -am "release: v0.3.1"
git tag -a v0.3.1 -m "readproof v0.3.1"
git push origin main
git push origin v0.3.1        # this is what starts both workflows
```

### 5. Watch and verify

```bash
gh run watch                                   # release.yml and publish-npm.yml
gh release view v0.3.1

go install github.com/fbzz/readproof/cmd/readproof@v0.3.1 && readproof version
brew update && brew install fbzz/tap/readproof && readproof version
npm view @readproof/sdk version
npm view dsh-plugin-readproof version
```

`go install` reads from the Go module proxy, which can lag a few minutes
behind the tag; `GOPROXY=direct go install …` bypasses it if you are
impatient.

## Notes and gotchas

- **`internal/version.Version` is a constant, not a linker variable.**
  Only `Commit` is stamped (`-X …/internal/version.Commit={{ .ShortCommit }}`),
  so `readproof version` prints `0.3.1+a1b2c3d`. That is deliberate: the
  version an evidence bundle records has to be reproducible across builds of
  the same source, and a linker flag is not.
- **Archives are reproducible.** `mod_timestamp: {{ .CommitTimestamp }}`
  means two builds of the same tag produce byte-identical archives, so the
  checksums are meaningful.
- **A cask, not a formula.** GoReleaser deprecated `brews` in v2.10 in
  favour of `homebrew_casks`; Homebrew wants pre-compiled binaries in casks
  and reserves formulae for source builds. `brew install fbzz/tap/readproof`
  resolves to the cask on macOS. Homebrew-on-Linux has no cask support, so
  Linux users take `go install` or the release archive — both are in the
  README's Install section.
- **The binaries are not notarised.** The cask carries a `postflight` hook
  that strips `com.apple.quarantine`, which is the standard workaround and
  also the reason release signing is on [the roadmap](roadmap.md).
- **Pre-releases.** `prerelease: auto` marks any tag with a suffix
  (`v0.4.0-rc.1`) as a GitHub pre-release automatically. npm does *not* get
  the same treatment — a pre-release tag would publish to the `latest` dist-tag.
  Add `--tag next` to the publish steps first if you cut one.
- **Re-running a publish is safe.** `publish-npm.yml` checks
  `npm view <name>@<version>` and skips anything already on the registry, so
  `workflow_dispatch` is the retry path when one of the two packages failed.
- **Yanking.** GitHub releases can be deleted; npm versions can only be
  deprecated (`npm deprecate @readproof/sdk@0.3.1 "…"`) after 72 hours.
  Prefer cutting `0.3.2`.

## MCP registry

[`integrations/mcp-registry/`](../integrations/mcp-registry/) holds the
`server.json` describing `readproof mcp` and the `mcp-publisher` commands
for pushing it. That is a separate, manual step after the release exists —
its README has the details.
