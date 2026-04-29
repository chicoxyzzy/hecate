# Agent guide for Hecate UI

Scannable map for the React/Vite operator UI under `ui/`. Sibling
to the root [`AGENTS.md`](../AGENTS.md), which covers the Go side.

## Layout

```
ui/src/
  features/
    runs/         TasksView, TaskDetail, NewTaskSlideOver, TaskList
                  — agent task list + run replay (the headline UI)
    chats/        ChatView — interactive chat against the gateway
    overview/     ConnectYourClient, ObservabilityView
                  — request ledger + trace drilldown + Codex/Claude Code setup
    admin/        AdminView, PricebookTab — tenants, keys, pricebook
    providers/    ProvidersView — provider catalog + health
    shared/       ui.tsx — primitives, ProviderPicker, ModelPicker, useFloatingDropdownStyle
  lib/
    api.ts        fetch wrappers + streamTaskRun (SSE)
    markdown.ts, provider-utils.ts, runtime-utils.ts
  types/
    runtime.ts    TypeScript mirrors of the Go API types
  styles.css      design tokens, .dropdown-menu, animations
```

## Build / test commands

| Command | What it does | When to use |
|---|---|---|
| `bun run typecheck` | `tsgo -b` — fast type check, no test execution | First sanity check after edits |
| `bun run test` | `vitest run` — full test suite | Before committing |
| `bun run test:watch` | watch mode | During iteration |
| `bun run dev` (from repo root: `make ui-dev`) | Vite dev server on `:5173`, proxying API to `:8080` | Live UI iteration alongside `make dev` |

**Do not use `bun test`** — it skips the testing-library DOM setup and panics with `document[isPrepared]` errors. Always `bun run test` (which dispatches to vitest).

## Conventions

- **Match existing UI design.** Reuse design tokens (`var(--bg1)`, `var(--t0)`, `var(--radius)`, `var(--font-mono)` ...), reuse primitives from `features/shared/ui.tsx`, copy layout patterns from neighboring screens. Never invent new styles.
- **No duplicate summary surfaces by default.** If data is already visible on the page, prefer better ordering, clearer labels, or progressive disclosure over adding another panel that restates it.
- **Ask before adding new persistent UI surfaces.** Refining an existing view is fine; adding a new inspector, side rail, dashboard block, or always-visible panel needs explicit user approval first.
- **Keep provider ordering stable.** The Providers view should stay in its fixed alphabetical/preset order within each section. Do not re-rank providers by health, availability, or actionability unless the user asks for that behavior explicitly.
- **Admin tabs use short tab labels and more descriptive in-view headers.** Preserve that split: concise tabs for navigation, fuller section headers inside the active tab for context.
- **Docs-only AGENTS/SKILL updates are `chore:` commits.** When a change only adjusts local agent guidance or UI docs, propose a `chore(...)` Conventional Commit rather than `docs(...)`.
- **No emojis** in code or copy unless explicitly requested.
- **Tests use vitest** (`describe` / `it` / `expect`) with `@testing-library/react` + `@testing-library/user-event`. Pattern: setup function returns `{ props, user, render }`.
- **Type names mirror Go**: `runtime.ts` shapes match `pkg/types/` and `internal/api/` exactly. When the Go side adds a field, mirror it here in the same PR — otherwise the SSE stream consumers and detail panels start dropping data silently.
- **SSE streams reconnect via `Last-Event-ID`** — `streamTaskRun` (`lib/api.ts`) handles this. Don't retry on `AbortController` cancellation; treat it as graceful close.

## Test patterns

```tsx
function setup(overrides: Partial<React.ComponentProps<typeof TaskDetail>> = {}) {
  const props: React.ComponentProps<typeof TaskDetail> = {
    /* sane defaults including new fields like streamTurnCosts: new Map() */
    ...overrides,
  };
  const user = userEvent.setup();
  return { props, user, render: () => render(<TaskDetail {...props} />) };
}
```

When the Go side adds a required prop (e.g. `streamTurnCosts`), update the `setup` helper in the affected `*.test.tsx` files first — typescript will surface every test that needs the new value.

## Gotchas

- **`.dropdown-menu` has `left: 0` baked into `styles.css:208`.** When using `useFloatingDropdownStyle` with `align="right"`, the hook explicitly sets `left: "auto"` to override — don't remove that. Without it the dropdown stretches viewport-wide.
- **Slideover overflow clipping**: dropdowns inside `<NewTaskSlideOver>` get clipped by the slideover's overflow. Always use `useFloatingDropdownStyle` (which uses `position: fixed` to escape) for any dropdown that might appear inside a panel — see `ProviderPicker` / `ModelPicker` in `shared/ui.tsx` for the pattern.
- **404 on stale task IDs**: `localStorage` may hold a task ID from a prior gateway boot (memory backend resets on restart). `TasksView` handles this by dropping the dead row from the list and re-loading; don't propagate the 404 as an error toast.
- **`render1()` + `render2()` in the same `it` block**: don't. React Testing Library cleanup runs between tests, not within. Split into two `it`s if you need fresh mounts.
- **The cost-ceiling banner**: gates on `run.otel_status_message === "cost_ceiling_exceeded"` (the specific string). A regression that drops or rewords that string silently breaks the "Raise ceiling & resume" affordance.

## Recipes

### Add a new SSE-driven UI state field

1. Add the field to `types/runtime.ts` `TaskRunStreamEventData` (matching the Go `TaskRunStreamEventData` shape exactly)
2. Accumulate it in `TasksView` — new `useState`, populate inside `streamTaskRun`'s `onPayload` callback, reset in `resetRunDetail`
3. Drill via props to `TaskDetail` and any consumer
4. Add to the `setup` defaults in affected `*.test.tsx` files
5. Add a focused test asserting the prop reaches the rendered output (see `TaskDetail.test.tsx` `falls back to streamTurnCosts...` for a template)

### Add a paired provider+model picker

Reuse `ProviderPicker` + `ModelPicker` from `features/shared/ui.tsx`. Pass `modelWarnings` to surface capability hints (e.g. "model lacks tool-calling"). Both pickers use `useFloatingDropdownStyle` — drop them into a slideover with no extra wrapping.

### Refresh a snapshot test after a UI shape change

Run `bun run test -- -u` to update committed snapshots. Review the diff carefully — accidental snapshot churn is the most common silent regression vector.

## Canonical docs

UI behavior beyond this file:

- [`docs/architecture.md`](../docs/architecture.md) — request flow, what the UI is observing
- [`docs/runtime-api.md`](../docs/runtime-api.md) — task / run / approval endpoints the UI calls
- [`docs/events.md`](../docs/events.md) — every `/v1/events` event type with payload shapes
- [`docs/client-integration.md`](../docs/client-integration.md) — multi-modal content + cross-provider behavior visible in chat
- [`docs/development.md`](../docs/development.md) — UI hot reload, screenshot tooling
