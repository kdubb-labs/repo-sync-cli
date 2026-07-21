---
name: repo-sync
description: Safely synchronize local Git repositories and clone missing repositories from a GitHub organization. Use for concise, scheduled repository refreshes; supports dry runs and compact JSON output.
---

# repo-sync

## Safe preview

```bash
repo-sync sync --root ~/git --org onyxpie --branch main --dry-run --json
```

## Apply

```bash
repo-sync sync --root ~/git --clone-dir ~/git/onyxpie --org onyxpie --branch main --json
```

The command skips dirty worktrees, originless repositories, archived organization repositories, and repositories that do not expose `origin/main`. It never resets, force-checks out, force-pushes, or commits.

Use `repo-sync agent-context` for the machine-readable command contract.
