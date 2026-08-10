# Code Intelligence Model Dogfood — 2026-08-10

Evidence for
[`native-code-intelligence.md`](../../implemented/native-code-intelligence.md)
and PR #966. The primary result used source revision `e02cd408`, the
`project-agent-loop-v1` harness, and three repeats of six scenarios launched
through real Hecate Project assignments.

## Summary verdict

**The exercised workspace and policy signals held; the tested local-model
workflow is not reliable enough to graduate.** One of 18 scenarios passed every
strict check. The model usually discovered capabilities and attempted the
intended semantic or structural route, but those attempts rarely produced
useful tool results, and none of the unavailable-provider or restricted-policy
scenarios completed a qualifying bounded fallback.

This result does not justify embedded Tree-sitter, additional semantic language
servers, language-server pooling, or write-side refactoring. Recorded
code-intelligence step latency was small compared with model-run time, and the
existing ast-grep provider was available but used unreliably. The next gate is
the same repeated matrix with a stronger tool-capable model. If request-shape
and fallback failures reproduce with that model, then evaluate
operation-specific tools or a provider-compatible conditional schema.

That gate was subsequently exercised in the
[strong-model follow-up](code-intelligence-strong-model-dogfood-2026-08-10.md).
The stronger model completed every task without request-shape failures, but the
exact-path prompts made direct file reads the rational route. The follow-up
therefore moves the next gate to less-confounded navigation-fit scenarios
before any production schema change.

## Setup

| Setting                      | Value                                                                                                                                                                                 |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Runtime source               | PR #966, `e02cd4083572befc3da8609a6e03113168959cb8`                                                                                                                                   |
| Model route                  | Ollama / `ministral-3:latest`                                                                                                                                                         |
| Repeats                      | 3                                                                                                                                                                                     |
| Scenarios per repeat         | 6                                                                                                                                                                                     |
| Platform                     | macOS arm64, `sandbox-exec`                                                                                                                                                           |
| Go semantic provider         | gopls 0.23.0, `installed_unverified` baseline                                                                                                                                         |
| TypeScript semantic provider | tsc 7.0.2, `installed_unverified` baseline                                                                                                                                            |
| Structural provider          | ast-grep 0.45.0, `installed_unverified` baseline                                                                                                                                      |
| Score semantics              | v2, recomputed after auditing the primary report's allowlisted observations                                                                                                           |
| Task posture                 | tools enabled; 15 write-capable and 3 read-only restricted scenarios; effectful proposals approval-gated and rejected; network, browser, Project memory, and context sources disabled |
| Workspace posture            | isolated, revision-pinned, test-owned temporary clones                                                                                                                                |

`installed_unverified` means the pre-run capability probe found a trusted
binary and bounded version, not that its protocol or query had already
succeeded. Real scenario queries provide the stronger runtime evidence.

## Aggregate result

| Metric                                                                |                       Result |
| --------------------------------------------------------------------- | ---------------------------: |
| Strict pass                                                           |                       1 / 18 |
| Capabilities consumed, where an inspection was attempted              |                      16 / 17 |
| Inspection not attempted after capabilities                           |                       1 / 18 |
| Preferred route selected, where advertised                            |                      11 / 12 |
| Preferred route produced results, where advertised                    |                       2 / 12 |
| Correct bounded fallback, where applicable                            |                        0 / 6 |
| Useful final answer                                                   |                       5 / 18 |
| Completed run                                                         |                      12 / 18 |
| Workspace unchanged                                                   |                      18 / 18 |
| Restricted-policy block observed                                      |                        3 / 3 |
| Forced provider unavailability observed                               |                        3 / 3 |
| Scenario with an unexpected tool proposal                             |                       2 / 18 |
| First non-capability code-intelligence step latency, median / maximum |    55 / 714 ms (17 measured) |
| Provider process cleanup                                              | not measured by this harness |

The harness rejects effectful proposals, and no scenario changed its workspace.
Process ownership cannot be proven by a global host scan, so deterministic
provider-supervision and race tests remain the cleanup evidence.

## Scenario-family result

“Selected” below is the literal preferred operation attempt, independent of
whether it returned items. A useful answer recovered through targeted reads is
kept separate from proof that the preferred or fallback route worked.

| Scenario family     |           Capabilities consumed | Preferred selected | Preferred produced results | Correct fallback | Useful | Completed | Strict pass |
| ------------------- | ------------------------------: | -----------------: | -------------------------: | ---------------: | -----: | --------: | ----------: |
| Go semantic         |                           3 / 3 |              3 / 3 |                      1 / 3 |              n/a |  0 / 3 |     1 / 3 |       0 / 3 |
| TypeScript semantic | 2 / 2 measured; 1 no inspection |              2 / 3 |                      1 / 3 |              n/a |  2 / 3 |     3 / 3 |       1 / 3 |
| Python structural   |                           2 / 3 |              3 / 3 |                      0 / 3 |              n/a |  2 / 3 |     2 / 3 |       0 / 3 |
| Rust structural     |                           3 / 3 |              3 / 3 |                      0 / 3 |              n/a |  1 / 3 |     2 / 3 |       0 / 3 |
| Restricted posture  |                           3 / 3 |                n/a |                        n/a |            0 / 3 |  0 / 3 |     3 / 3 |       0 / 3 |
| Missing Go provider |                           3 / 3 |                n/a |                        n/a |            0 / 3 |  0 / 3 |     1 / 3 |       0 / 3 |

The sole strict pass was TypeScript semantic navigation, which returned 34
items and the expected identifier. One Go run returned 150 semantic items but
did not turn them into the expected answer. Across all six Python/Rust
scenarios, preferred structural calls produced zero items; three runs still
recovered useful answers through bounded reads.

## Bounded failure evidence

The scorecard stores only closed Hecate-generated validation reasons. Across 18
scenarios it observed:

| Closed reason                | Scenario observations |
| ---------------------------- | --------------------: |
| `structural_path_invalid`    |                     3 |
| `workspace_file_unavailable` |                     2 |
| `language_required`          |                     1 |
| `query_required`             |                     1 |
| `unsupported_operation`      |                     1 |

These codes established that request shape was a recurring problem; they do not
diagnose every zero-result call. They led to compact canonical call examples
and stronger existing-workspace-relative path wording. Those changes made the
guidance more explicit but did not make the local model reliable across
repeated runs.

## Harness finding

The first three-repeat attempt produced no scorecard because Project workspaces
used the host-global temporary root and accumulated across dogfood invocations,
eventually exhausting the volume during provisioning. The harness now sets
`TMPDIR`, `TMP`, and `TEMP` to one test-owned directory, stops both gateways
before cleanup, and removes the complete child workspace tree automatically.
The successful retry left zero dogfood clones in the global Hecate workspace
root.

This was a harness leak, not a production workspace-isolation failure. It was
still important dogfood evidence: a repeatable benchmark must clean up its own
large persistent-workspace fixtures.

## Earlier directional samples

Two one-repeat Ministral samples helped shape the scorer and guidance but are
not combined statistically with the final matrix because both implementation
and scoring semantics changed between them. They showed that clearer routing
guidance could move initial capability discovery and preferred-route attempts
in the right direction without improving end-to-end reliability.

An exploratory one-repeat run with the only other installed local model,
`llama3.1:8b`, completed all six scenarios but produced no useful answers and
barely used tools. It is not a stronger reference model and does not satisfy the
second-model graduation gate.

## Publication boundary and limitations

- This note contains no task, Run, or trace identifiers; prompts; paths;
  queries; source excerpts; final model text; raw provider errors; stderr;
  process arguments; or secrets.
- The primary sample is only three repeats with one small local model.
- Model output, tool arguments, and completion remain stochastic.
- Provider availability was `installed_unverified` before each real query.
- The primary runtime emitted a v1 scorecard. The aggregate figures above were
  recomputed under the final v2 semantics after auditing its allowlisted
  scenario observations: a capabilities-only Run is not counted as a failed
  capability-use measurement, and a later provider failure does not
  retroactively invalidate an earlier completed query. The v1 artifact did not
  persist the new completed-query model-call and step positions or distinguish
  a measured zero-millisecond query from an absent measurement, so a new v2 run
  is required for a fully self-contained reproducible scorecard.
- The scorecard does not measure provider process cleanup; deterministic tests
  own that assertion.
- This run itself used only the local model. The later
  [strong-model follow-up](code-intelligence-strong-model-dogfood-2026-08-10.md)
  records the cross-model result and its proxy-route limitations.

## Decision

Keep the current LSP + ast-grep + bounded-grep architecture. Do not add direct
Tree-sitter or more semantic providers from this evidence. The then-next action
was the same three-repeat matrix with a stronger tool-capable model. That gate
is now recorded in the
[strong-model follow-up](code-intelligence-strong-model-dogfood-2026-08-10.md),
whose less-confounded scenario-design decision supersedes this historical next
step.
