#!/bin/bash

dir="$(git rev-parse --show-toplevel)"
cd "$dir"

if [ -d ".git/rebase-merge" ] || [ -d ".git/rebase-apply" ]; then
    echo "Rebase already in progress. Resolve conflicts, then run this script again."
    exit 1
fi

echo "Rebasing onto main with per-commit config materialization..."
git rebase main \
  --exec 'make -C config materialize && git add config/rendered && git commit --amend --no-edit --no-verify'

if [ $? -ne 0 ]; then
    echo "Rebase paused. Resolve conflicts, run 'make -C config materialize', stage rendered files, then 'git rebase --continue'."
    exit 1
fi

# Final materialize + amend to ensure HEAD is clean
make -C config materialize
if [ -n "$(git status --short config/rendered)" ]; then
    echo "Amending HEAD with final materialized config..."
    git add config/rendered
    git commit --amend --no-edit --no-verify
fi

echo "Rebase complete. Rendered configs regenerated per commit."
