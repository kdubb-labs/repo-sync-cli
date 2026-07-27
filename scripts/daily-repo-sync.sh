#!/usr/bin/env bash
set -euo pipefail

export PATH="/opt/homebrew/bin:/usr/local/bin:/Users/tuvok/.local/bin:/usr/bin:/bin"
exec /Users/tuvok/.local/bin/repo-sync sync \
  --root /Users/tuvok/git \
  --clone-dir /Users/tuvok/git/onyxpie \
  --org onyxpie \
  --branch main \
  --json \
  "$@"
