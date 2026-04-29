# Implementation

How to implement once a plan exists, or when the change is small enough to skip planning.

## Make minimal coherent changes

One logical change at a time. If it fans out across unrelated reasons, split into separate commits. The reviewer (human or agent) should be able to hold the whole change in their head; "and while I was here…" diffs defeat that.

## Preserve local style

Read the neighboring code first. Mirror its conventions — naming, error wrapping, comment density, test layout. Don't introduce a new style island. The codebase is internally consistent on purpose; matching what's already there is faster than inventing a new pattern.

## Avoid unrelated edits

Drive-by formatting, renaming, or "while I'm here" cleanups bloat the diff and obscure review. If a cleanup is worth doing, it's worth a follow-up commit.

## When to update docs

Docs are part of the deliverable, not a follow-up. The same change syncs them.

| Change | Doc |
|---|---|
| New env var | `.env.example` and the relevant `docs/<feature>.md` env-var table |
| New API field | `docs/runtime-api.md` (or wherever the contract lives) |
| New event type | `docs/events.md` with payload shape |
| New built-in tool | `docs/agent-runtime.md` and/or `docs/mcp.md` |
| New behavior on the api↔providers boundary | both sides' tests |

Stale env-var docs cause more on-call pages than missing features.

## When to add comments

When the *why* isn't obvious from the code. State the trade-off being accepted, not the mechanic. The reader can see *what* the code does; they can't see what was rejected and why.

Don't add comments that paraphrase identifiers. `// increment counter` ages into noise. If the function name says it, don't say it again.

## The seven-step chain

When the change is "add a passthrough wire field," follow the canonical seven-step chain in [`../skills/providers/SKILL.md`](../skills/providers/SKILL.md). Forgetting to plumb the field into the streaming `wireReq` is the most common bug — the non-stream tests pass, the field silently drops in production for any client using `stream: true`.
