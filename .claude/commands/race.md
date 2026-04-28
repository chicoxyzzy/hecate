---
description: Run the full Go race suite — the canonical "ready to commit" check
---

Run the full race-detector test suite for this repo:

```
go test ./... -race -count=1
```

Report whether it passed cleanly. If anything fails, surface the package + test name and the first error line; don't dump the full trace unless asked.

Run from the repo root. This usually takes ~60-90s on a warm machine.
