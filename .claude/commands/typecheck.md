---
description: Fast UI type check via tsgo — first sanity check after a UI edit
---

Run the UI type checker:

```
cd ui && bun run typecheck
```

This is `tsgo -b` under the hood — much faster than a full vitest run, so it's the right first sanity check after editing any `.ts` / `.tsx` file. It catches the most common mistake (missing prop, wrong type signature, stale type mirror against the Go API) in seconds.

If it passes, follow up with `bun run test` for behavior.
