# Release

Cutting a public release tag. Companion to [`../../docs/release.md`](../../docs/release.md), which is the operator-facing version (release notes format, alpha gate, image build). This doc is the agent-side procedure with the footguns the v0.1.0-alpha.1 cycle earned the hard way.

## When this fires

- Operator says "cut a release" / "tag vX.Y.Z" / "ship the alpha" / similar.
- Master is in a stable state worth tagging.
- The change set since the previous tag is meaningful (a release with one typo fix is not worth the operational ceremony).

Default to producing a written plan first ([`../skills/architect/SKILL.md`](../skills/architect/SKILL.md)): version pick, gate posture, recovery path, what's in/out of the release notes.

## Pre-flight

Run these in order. Each can be done without remote impact.

1. **Clean worktree.** `git status` shows nothing modified, nothing staged. Goreleaser refuses to release from a dirty worktree.
2. **`dist/` is gitignored at repo root.** The goreleaser snapshot writes binaries and tarballs into `./dist`; if anything in there is tracked, the next release-CI run breaks with "git is in a dirty state" because `--clean` deletes the tracked files. The `ui/dist/` entry in `.gitignore` does **not** cover repo-root `dist/`.
3. **`goreleaser` installed.** `which goreleaser` returns a path. Install via `go install github.com/goreleaser/goreleaser/v2@latest` if missing.
4. **Snapshot dry-run.** `goreleaser release --snapshot --clean`. Builds 4 binaries + 2 Docker images locally without publishing. Inspect `dist/CHANGELOG.md` (or the workflow log on a real run) — the first tag in a repo includes every commit since git history began, which is rarely what you want for the release page. If the changelog is unusable, tune `.goreleaser.yaml`'s `changelog.filters` or pass `--release-notes <file>` on the real run.
5. **Verify-alpha gate.** `make verify-alpha` exit 0 — full ladder including `docs-env-check`, race suite, docker-smoke, and UI e2e. See [`../core/verification.md`](../core/verification.md). The gate is mandatory; calling it out as skipped in the release notes is acceptable only when the skip's risk is named.

## Tag and push

```bash
git tag -a vX.Y.Z -m "<release notes baked into the annotation>"
git push origin vX.Y.Z
```

Use annotated (`-a`) tags. The annotation message is what `git show vX.Y.Z` and the GitHub release page surface; treat it as the canonical release notes. Pre-release suffixes (`-alpha.N`, `-beta.N`, `-rc.N`) are recognized by goreleaser's `prerelease: auto` config and the GitHub Releases entry is auto-marked as a pre-release.

## Watch CI

Push triggers `.github/workflows/release.yml` → goreleaser → multi-arch binaries + Docker images on `ghcr.io/chicoxyzzy/hecate` + GitHub release entry. The full pipeline runs ~5–10 minutes (Docker buildx multi-arch dominates).

Acceptance:

- Workflow run is green.
- GitHub Releases page has the entry, marked **Pre-release** for `-alpha.N` tags.
- `docker pull ghcr.io/chicoxyzzy/hecate:X.Y.Z` succeeds (no `v` prefix — see footgun below).
- `docker run --rm -p 8765:8765 ghcr.io/chicoxyzzy/hecate:X.Y.Z` then `curl :8765/healthz` returns `version: "X.Y.Z"`.

## Footguns

- **`{{ .Version }}` strips the `v` prefix.** Docker tags are `0.1.0-alpha.1`, **not** `v0.1.0-alpha.1`. The git tag itself keeps the `v`. Same applies to tarball names. The `/healthz` `version` field also reports the bare semver.
- **`.env_file` in compose overrides Dockerfile `ENV`.** If your local `.env` has `GATEWAY_DATA_DIR=.data` (relative), it'll override the Dockerfile's absolute `/data` and break `docker compose cp /data/...`. The current `.env.example` comments these out specifically; old developer-machine `.env` copies may still have the override and will fail `make test-docker-smoke` locally even though CI passes.
- **First-tag changelog is all-history.** Goreleaser builds the auto-changelog from git log between previous and current tags; if there's no previous tag, it includes every commit since the initial commit. Inspect the snapshot output before tagging.
- **Don't run snapshot from a clean checkout, then `git add -A`.** The snapshot writes ~50 MB of binaries into `./dist`; a sweeping `git add` will pick them up if `dist/` isn't gitignored.

## Recovery

If CI fails:

```bash
git push --delete origin vX.Y.Z
git tag -d vX.Y.Z
# fix root cause, re-tag, re-push
```

Tag deletion on GitHub also clears the dangling Release entry (if one was created before the failure step). Goreleaser's release pipeline is mostly idempotent — a clean retag at a fixed commit produces the same artifacts.
