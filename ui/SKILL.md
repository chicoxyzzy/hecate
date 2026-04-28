---
name: hecate-ui
description: Use when working on the Hecate operator UI in `ui/`. This skill keeps frontend work aligned with Hecate's operator workflows, runtime-debugging focus, and React/Vite stack.
---

# Hecate UI Skill

This is not a marketing site. It is an operator-facing control surface for an AI gateway. The UI should help someone understand what the runtime is doing, what is misconfigured, what costs money, and what action to take next.

## Product Lens

The Hecate UI should feel like:

- an operator console
- a runtime control surface
- a debugging and inspection tool

It should not feel like:

- a generic SaaS dashboard full of cards
- a landing page with product-marketing copy
- a toy playground without production context

Default to utility, orientation, and workflow clarity.

## Visual Thesis

Calm, technical, and deliberate. Dense enough to be useful, sparse enough to scan. Strong hierarchy, minimal chrome, and one clear accent system.

Prefer:

- restrained layout
- strong type hierarchy
- clear sectioning without over-carding
- obvious status and health states
- meaningful spacing over decorative surfaces

Avoid:

- card mosaics
- oversized hero copy
- decorative gradients behind routine product UI
- multiple accent colors fighting for attention
- visual noise that hides runtime state

## UX Priorities

Every screen should answer the following quickly:

1. What system am I looking at?
2. What is its current state?
3. What can I do here?
4. What changed or failed?
5. Where do I go next?

When choosing between “pretty” and “operationally clear”, choose clarity.

## Information Architecture

Prefer organizing the UI around operator jobs:

- authenticate and understand current access
- inspect providers and models
- run and compare requests
- inspect trace and runtime metadata
- manage tenants, API keys, and policy-adjacent state
- inspect budget state and controls

Views should be task-shaped, not component-shaped.

If a page has too many concerns mixed together, split it into clearer sections or separate screens.

## Section Responsibilities

Each section should have exactly one job:

- orient
- inspect
- compare
- edit
- confirm

If a section tries to explain the whole system at once, it should be broken apart.

## Hecate-Specific UI Rules

- Authentication and identity context should be easy to find.
- Tenant context should always be visible when it affects behavior.
- Provider and model selection should expose local and cloud distinctions clearly.
- Runtime metadata should feel first-class, not tucked away in debug crumbs.
- Trace and failure details should be readable without scanning raw JSON first.
- Cost, cache, routing, and retry behavior should be visible in plain language.
- Dangerous or privileged actions should be visually separated from routine actions.

## Layout Guidance

Default app layout:

- top-level shell
- primary navigation or mode switch
- main workspace
- optional secondary inspector or detail pane

Prefer layout primitives over card wrappers.

Use cards only when the card itself is the interaction boundary, such as:

- a selectable provider target
- a tenant/API key record with contained actions
- a focused result panel that benefits from separation

## Copy Guidance

Use product UI copy, not marketing copy.

Good section labels:

- Session
- Playground
- Provider Routing
- Runtime Output
- Trace
- Budget State
- Tenant Access

Good supporting copy:

- explains scope
- explains freshness
- explains operator impact
- points to the next action

Bad supporting copy:

- hype
- mood statements
- abstract claims
- repeated explanations of what Hecate is

## Motion Guidance

Motion should support orientation, not decoration.

Allowed uses:

- view transitions that help users understand context shifts
- subtle reveal of runtime output or trace detail
- emphasis for status changes or async loading

Avoid ornamental motion or large attention-grabbing effects in core workflows.

## Technical Constraints

Current stack:

- React 19
- TypeScript
- Vite
- Vitest + Testing Library + jsdom
- Plain CSS with design tokens in `src/styles.css` (no CSS-in-JS framework)

### Toolchain: Bun, not Node

This project uses **Bun** (pinned via `packageManager` in `ui/package.json`) as the package manager and script runner. There is no `package-lock.json`; the lockfile is `bun.lock`. Always use Bun commands:

- `bun install` — install dependencies (not `npm install` / `yarn` / `pnpm`)
- `bun run build` / `bun run test` / `bun run dev` — invoke scripts (not `npm run …`)
- `bun add <pkg>` / `bun remove <pkg>` — manage dependencies
- `bun x <tool>` is the equivalent of `npx`

Scripts in `package.json` already assume Bun internally (e.g. `build` runs `bun run typecheck && vite build`), and helper scripts under `../scripts/` are invoked as `bun run …`. Don't introduce npm/yarn/pnpm-specific lockfiles, configs, or workflow steps; CI and local dev both expect Bun.

Use the existing stack unless there is a strong reason to add something new.

Keep dependencies light. Favor composition and small local abstractions over adding UI frameworks. Style via the design tokens (`var(--bg1)`, `var(--t0)`, `var(--accent)`, `var(--radius)`, `var(--font-mono)`, etc.) rather than per-component utility-class systems — the repo deliberately avoids the cascade of a class-based framework.

## Code Organization

The actual layout (mirror it when adding features):

- `src/app/`: app shell, top-level orchestration, route/mode switching
- `src/features/<area>/`: feature-shaped folders, one per operator job — `runs/`, `playground/`, `overview/`, `admin/`, `providers/`
- `src/features/shared/`: cross-feature primitives — `ui.tsx` (consolidated `ProviderPicker`, `ModelPicker`, `useFloatingDropdownStyle`), shared layout helpers
- `src/lib/`: API helpers (`api.ts`, including `streamTaskRun` SSE consumer), formatting (`markdown.ts`), runtime helpers (`provider-utils.ts`, `runtime-utils.ts`)
- `src/types/runtime.ts`: TypeScript mirrors of the Go API types — keep in lockstep with `pkg/types/` and `internal/api/`
- `src/test/`: shared test setup
- `src/styles.css`: design tokens, dropdown-menu rule, animations

There is no `src/components/`. Reusable primitives live in `src/features/shared/ui.tsx`; feature-specific components live with their feature.

When a file gets crowded, split by responsibility, not by arbitrary line count.

Good splits:

- view shell vs data hooks
- presentation components vs transport helpers
- domain formatting vs generic utilities

## State and Data Rules

- Keep remote data shapes close to the API contracts.
- Normalize only where the UI benefits from it.
- Make loading, empty, and error states explicit.
- Prefer derived display helpers over inline formatting logic scattered across JSX.
- Avoid giant top-level components that fetch, normalize, render, and mutate everything at once.

## Testing Expectations

**Always add unit tests for new behavior.** Not "when practical" — as a default. Vitest + Testing Library + jsdom is already wired (`bun run test`); a change without tests is incomplete. The bar is: a future contributor should be able to refactor the implementation and have the tests catch a regression in user-visible behavior. If a component is hard to test, the abstraction is probably wrong (too much fetching mixed with rendering, props that aren't clearly inputs, etc.).

**Add e2e tests where they make sense.** Playwright is wired (`bun run test:e2e`, with a UI mode via `test:e2e:ui`). Reach for an e2e test when the change:

- spans multiple operator screens (auth → tenant context → run detail, etc.)
- depends on the real gateway responding (SSE stream rendering, run-event replay)
- introduces a new operator workflow (a new task creation path, a new approval flow)
- changes navigation, routing, or top-level shell behavior
- mutates anything that's hard to fake convincingly in a unit test (real focus management, scroll restoration, multi-tab drag-and-drop)

Unit tests prove that one component does the right thing in isolation. E2e tests prove the operator journey actually works end-to-end.

Prioritize unit tests for:

- data-to-UI transformation
- conditional rendering of critical states
- form-input parsing and submit-payload shaping
- trace/runtime inspection behavior

Prefer testing behavior and outcomes over implementation details.

## Workflow Before Editing

Before making a substantial UI change, write down:

- visual thesis: the mood and clarity target
- content plan: what the screen must help the operator do
- interaction thesis: what transitions, states, or emphasis patterns matter

If the design starts feeling messy, return to those three statements and simplify.

## Done Criteria

A UI change is in good shape when:

- the main workflow is obvious in a few seconds
- the current state of the system is visible
- labels and actions read like product UI
- runtime/debug details are accessible without cluttering the default path
- the layout still works on mobile and desktop
- tests cover the risky logic or behavior
