---
description: Propose a Conventional Commit message (subject + optional body) for the current diff
---

Look at the working-tree diff and propose a Conventional Commits message in a fenced code block. Don't auto-commit — the operator decides when to apply it.

Steps:

1. Run `git status --short` and `git diff --stat` to see what's changed
2. Pick the right type:
   - `feat(<scope>):` — new feature or capability
   - `fix(<scope>):` — bug fix
   - `test(<scope>):` — tests only (e2e or unit)
   - `docs(<scope>):` — markdown / docs only
   - `chore(<scope>):` — tooling, config, deps
   - `refactor(<scope>):` — same behavior, different shape
3. Scope = the affected package or area (e.g. `chat-api`, `providers/anthropic`, `mcp`, `e2e`)
4. Subject line: focuses on *what changed*, under 72 chars when possible
5. **Body** (separated by a blank line): explain *why*, list non-obvious trade-offs, note follow-ups, call out anything an operator reviewing the diff in 6 months will need context for. Wrap at ~72 chars.
6. Use the body for any of these when relevant:
   - the operator-visible behavior change
   - what was tried and rejected, and why
   - migration / compat notes (env knobs, schema, wire shape)
   - related issues or PRs (`Refs #123`, `Closes #456`)
   - bullet list of sub-changes when the commit touches several files for one coherent reason

A subject-only message is acceptable for trivial changes (typo fixes, dependency bumps) — but anything substantial should carry a body explaining *why*.

For pure-markdown changes, append `[skip ci]` to the subject (workflow paths-ignore catches `**/*.md` already, but the marker is belt-and-suspenders).

Examples that landed in this repo:

```
feat(chat-api): multi-modal content (OpenAI image_url + Anthropic image translation)

OpenAIChatMessage.Content was *string — couldn't represent vision
content blocks. Replace with a polymorphic OpenAIMessageContent that
round-trips JSON through string / array-of-blocks / null. Mirror the
shape on the providers package side.

On the cross-provider route, translate OpenAI image_url blocks into
Anthropic's image+source shape: URL → source.type=url; data URIs
parsed inline → source.type=base64 with extracted media_type. Saves
Anthropic from handling pseudo-URLs and works on older Anthropic
API versions that only accept base64.

Closes the biggest user-visible gap from the chat-API audit.
```

```
fix(providers/anthropic): drop arithmetic from make() cap to silence CodeQL CWE-190
```

```
test(e2e): cover multi-modal content end-to-end

Three new e2e tests using a new fakeUpstreamCapturing helper:
- OpenAI → OpenAI image_url passthrough (array-form on wire)
- OpenAI → Anthropic URL translation (source.type=url)
- OpenAI → Anthropic data URI → base64 translation

Catches regressions the unit tests can't — full HTTP roundtrip
against the real binary, asserting wire shape per provider.
```
