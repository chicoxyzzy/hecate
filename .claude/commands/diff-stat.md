---
description: Snapshot of what's changed in the working tree (status + diff stat)
---

Show a compact view of what's changed in the working tree:

```
git status --short && git diff --stat
```

Use this before proposing a commit message — gives you both the file list (untracked vs modified) and the line-count distribution per file. Together they answer: "is this commit cohesive, or did it accidentally pick up unrelated drift?"
