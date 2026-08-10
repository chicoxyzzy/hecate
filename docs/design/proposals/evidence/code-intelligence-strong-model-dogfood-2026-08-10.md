# Code Intelligence Strong-Model Dogfood — 2026-08-10

Follow-up evidence for
[`native-code-intelligence.md`](../../implemented/native-code-intelligence.md)
and the first
[`local-model scorecard`](code-intelligence-model-dogfood-2026-08-10.md).
The run used source revision
`278da6bb3f30a213b37546d17fcd443b23a0093a`, the v2
`project-agent-loop-v1` harness, and three repeats of six Project-assignment
scenarios.

## Summary verdict

**The stronger model completed the coding lookups reliably, but this matrix
does not establish reliable code-intelligence adoption.** All 18 Runs completed,
returned the expected identifier, proposed no unexpected tools, and left their
workspaces unchanged. Three of 18 passed every strict routing check.

The model discovered capabilities in every Run, but it used the exact path
provided by each prompt to read the target file in every Run. It selected the
advertised semantic or structural route in three of 12 applicable scenarios,
and two of those queries produced results. Unlike the local-model baseline,
there were no closed invalid-request reasons or provider-query failures.

This is evidence that the stronger model and the Hecate Project workflow are
operationally reliable. It is not evidence that the multiplexed
`code_intelligence` schema is defective: the current questions make a bounded
`read_file` call the cheapest rational route. The next gate is a harness-only
scenario revision that separates navigation-fit tasks from an exact-path
direct-read control. Do not add Tree-sitter, more language servers, pooling, or
write-side operations from this result.

## Setup

| Setting                          | Value                                                                                                                                                                                 |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Runtime source                   | `278da6bb3f30a213b37546d17fcd443b23a0093a`                                                                                                                                            |
| Model route, operator-observed   | Fireworks Kimi K2.7 Code through a local Hecate-compatible proxy                                                                                                                      |
| Scorecard route label            | `hecateapp` / `accounts/fireworks/models/kimi-k2p7-code`                                                                                                                              |
| Proxy runtime, operator-observed | installed Hecate 0.6.0, loopback only                                                                                                                                                 |
| Repeats                          | 3                                                                                                                                                                                     |
| Scenarios per repeat             | 6                                                                                                                                                                                     |
| Platform                         | macOS arm64, `sandbox-exec`                                                                                                                                                           |
| Go semantic provider             | gopls 0.23.0, `installed_unverified` baseline                                                                                                                                         |
| TypeScript semantic provider     | tsc 7.0.2, `installed_unverified` baseline                                                                                                                                            |
| Structural provider              | ast-grep 0.45.0, `installed_unverified` baseline                                                                                                                                      |
| Score semantics                  | native v2 scorecard                                                                                                                                                                   |
| Task posture                     | tools enabled; 15 write-capable and 3 read-only restricted scenarios; effectful proposals approval-gated and rejected; network, browser, Project memory, and context sources disabled |
| Workspace posture                | isolated, revision-pinned, test-owned temporary clones                                                                                                                                |

The loopback proxy let the isolated scorecard gateway use the desktop app's
configured Fireworks route without extracting the credential or forwarding it
to the scorecard gateway.
A read-only operator-side catalog check immediately before the run returned the
selected model only under Fireworks; that inner route is not captured by the
sanitized scorecard. The scorecard therefore measures model behavior through
two Hecate gateways; it is not direct Fireworks-provider or cost evidence.

## Aggregate result

| Metric                                                                |                       Result |
| --------------------------------------------------------------------- | ---------------------------: |
| Strict pass                                                           |                       3 / 18 |
| Capabilities consumed before inspection                               |                      15 / 18 |
| Preferred route selected, where advertised                            |                       3 / 12 |
| Preferred route produced results, where advertised                    |                       2 / 12 |
| Correct qualifying fallback, where applicable                         |                        2 / 6 |
| Useful final answer                                                   |                      18 / 18 |
| Completed Run                                                         |                      18 / 18 |
| Workspace unchanged                                                   |                      18 / 18 |
| Restricted-policy block observed                                      |                        3 / 3 |
| Forced provider unavailability observed                               |                        3 / 3 |
| Scenario with an unexpected tool proposal                             |                       0 / 18 |
| First non-capability code-intelligence step latency, median / maximum |   42 / 2,937 ms (5 measured) |
| Provider process cleanup                                              | not measured by this harness |

The 18 scenarios used 64 model calls in total. Every Run called
`code_intelligence` with `operation=capabilities`, and every Run used
`read_file`. Only five non-capability code-intelligence calls were observed.
The report's zero cost is an unpriced proxy artifact and must not be interpreted
as zero provider spend.

## Scenario-family result

| Scenario family     | Capabilities consumed | Preferred selected | Preferred produced results | Correct fallback | Useful | Completed | Strict pass |
| ------------------- | --------------------: | -----------------: | -------------------------: | ---------------: | -----: | --------: | ----------: |
| Go semantic         |                 2 / 3 |              1 / 3 |                      1 / 3 |              n/a |  3 / 3 |     3 / 3 |       1 / 3 |
| TypeScript semantic |                 3 / 3 |              1 / 3 |                      1 / 3 |              n/a |  3 / 3 |     3 / 3 |       1 / 3 |
| Python structural   |                 3 / 3 |              1 / 3 |                      0 / 3 |              n/a |  3 / 3 |     3 / 3 |       0 / 3 |
| Rust structural     |                 3 / 3 |              0 / 3 |                      0 / 3 |              n/a |  3 / 3 |     3 / 3 |       0 / 3 |
| Restricted posture  |                 2 / 3 |                n/a |                        n/a |            0 / 3 |  3 / 3 |     3 / 3 |       0 / 3 |
| Missing Go provider |                 2 / 3 |                n/a |                        n/a |            2 / 3 |  3 / 3 |     3 / 3 |       1 / 3 |

The successful preferred queries were a TypeScript `document_symbols` query
with 34 items and a Go `document_symbols` query with 50 items. One Python
structural query completed with zero items. A missing-gopls scenario recovered
through structural search with 12 items; another recovered through grep with 12
matches but failed capability-timing discipline because `list_dir` was proposed
in parallel with capability discovery.

## Directional comparison

The first scorecard used Ollama `ministral-3:latest`. Its primary artifact was
v1 and its published figures were reconstructed under v2 semantics, while this
strong-model artifact is natively v2 and uses a later source revision. The
comparison is directional rather than a controlled model benchmark.

| Metric                           | Ministral | Kimi through Hecate proxy |
| -------------------------------- | --------: | ------------------------: |
| Strict pass                      |    1 / 18 |                    3 / 18 |
| Capabilities consumed, measured  |   16 / 17 |                   15 / 18 |
| Preferred route selected         |   11 / 12 |                    3 / 12 |
| Preferred route produced results |    2 / 12 |                    2 / 12 |
| Correct qualifying fallback      |     0 / 6 |                     2 / 6 |
| Useful final answer              |    5 / 18 |                   18 / 18 |
| Completed Run                    |   12 / 18 |                   18 / 18 |
| Workspace unchanged              |   18 / 18 |                   18 / 18 |
| Unexpected-tool scenarios        |    2 / 18 |                    0 / 18 |

Kimi eliminated the eight closed request-shape observations recorded in the
local-model baseline and turned every scenario into a useful completed answer.
Its lower preferred-route selection cannot be read as a simple regression: the
same exact-path questions reward direct reads, and Kimi solved all of them that
way. Query latency is also not comparable because the Kimi run made only five
measured non-capability code-intelligence calls, versus 17 in the baseline.

## Publication boundary and limitations

- This curated note contains no task, Run, or trace identifiers; prompts;
  paths; queries; source excerpts; final model text; raw provider errors;
  stderr; process arguments; or secrets.
- The sample is three repeats with one stronger model on one macOS host.
- The exact-path prompts and small targets confound task success with route
  adoption; `read_file` was a valid and efficient way to answer them.
- The proxy route preserves credential isolation but does not capture its inner
  provider identity, binary revision, token usage, or billed cost in the
  scorecard. Zero reported cost means unknown, not free.
- Provider baselines were `installed_unverified` before real queries.
- Useful-result scoring is a bounded marker check; excluded final answers cannot
  be independently graded from the curated artifact.
- Process cleanup remains outside this harness and is covered by deterministic
  provider-supervision tests.

## Decision

Keep the current LSP + ast-grep + bounded-grep architecture and the single
production dispatcher. Before prototyping a new model-facing schema, add paired
navigation-fit scenarios where the target file is not supplied and cross-file
semantic or structural navigation has real value, plus an exact-path control
where direct reading is the expected route. Score route appropriateness by
scenario type and repeat the three-run Kimi matrix.

Only if the stronger model still discovers capabilities but bypasses code
intelligence on navigation-fit tasks should Hecate A/B operation-specific
read-only schemas against the current multiplexed tool. This result provides no
case for embedded Tree-sitter or additional semantic providers.
