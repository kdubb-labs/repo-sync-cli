# repo-sync

`repo-sync` safely refreshes local Git repositories and discovers missing repositories from a GitHub organization. It is designed for scheduled, compact, agent-readable operation.

## What it does

For each Git worktree under `--root` (default: `~/git`), it:

1. reads the `origin` URL;
2. **skips dirty worktrees** without changing branch or fetching;
3. fetches `origin` with `--prune`;
4. verifies `origin/<branch>` exists (default: `origin/main`);
5. switches only clean worktrees to that branch; and
6. updates using `git pull --ff-only`.

It never uses `reset --hard`, forced checkout, force-push, or automatic commits.

It queries the GitHub organization through the authenticated `gh` CLI, skips archived repositories, and clones missing repositories into `--clone-dir` (default: `~/git/<org>`) using GitHub's HTTPS `clone_url`, which works with the existing `gh` Git credential helper. A repository already checked out elsewhere is recognized by its normalized GitHub remote, so it is not cloned twice.

> Repositories whose remote does not have `main` are reported as `skipped_no_origin_branch`. This is deliberate: the tool does not invent or rewrite remote branches. At present, `onyxpie/bookwise`, `onyxpie/onyx-social`, and `onyxpie/proofline` use `dev` as their default branch.

## Install

```bash
cd ~/git/cli/repo-sync
make install
```

This installs `repo-sync` to `~/.local/bin/repo-sync`.

## Usage

Preview the next run without modifying any repository:

```bash
repo-sync sync --root ~/git --org onyxpie --branch main --dry-run --json
```

Perform the synchronization:

```bash
repo-sync sync --root ~/git --clone-dir ~/git/onyxpie --org onyxpie --branch main --json
```

The JSON result is deliberately compact: counts plus only skipped or failed entries. Exit status `0` means the run completed with no failed Git/GitHub operation; dirty, non-main, originless, and diverged repositories are safely reported as skips. Exit `2` is invalid CLI usage; exit `5` means at least one Git or GitHub synchronization action failed.

For an agent-readable command contract:

```bash
repo-sync agent-context
```

## Daily automation

`scripts/daily-repo-sync.sh` uses explicit paths suitable for Hermes cron. It emits exactly one JSON report on standard output.

## Development

```bash
make ci
```

The project uses only the Go standard library. Tests inject the command runner, so they do not modify real repositories or call GitHub.
