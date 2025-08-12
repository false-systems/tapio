#!/bin/bash

# Branch Cleanup Helper Script
# This script helps identify branches that can be cleaned up

echo "=== Branch Cleanup Analysis ==="
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get current branch
CURRENT_BRANCH=$(git branch --show-current)
echo "Current branch: ${GREEN}$CURRENT_BRANCH${NC}"
echo ""

# List remote branches that have been merged into main
echo "=== Branches merged into main (safe to delete) ==="
git branch -r --merged origin/main | grep -v 'HEAD\|main' | sed 's/origin\///' | while read branch; do
    echo -e "${GREEN}✓${NC} $branch"
done
echo ""

# List remote branches NOT merged into main
echo "=== Branches NOT merged into main ==="
git branch -r --no-merged origin/main | grep -v 'HEAD\|main' | sed 's/origin\///' | while read branch; do
    # Get last commit date and author
    last_commit=$(git log -1 --pretty=format:"%ar by %an" origin/$branch 2>/dev/null || echo "Unknown")
    echo -e "${YELLOW}⚠${NC}  $branch - $last_commit"
done
echo ""

# List local branches
echo "=== Local branches ==="
git branch | grep -v "main\|$CURRENT_BRANCH" | while read branch; do
    # Check if branch exists on remote
    if git ls-remote --heads origin "$branch" >/dev/null 2>&1; then
        echo -e "${GREEN}↑${NC} $branch (exists on remote)"
    else
        echo -e "${RED}✗${NC} $branch (local only)"
    fi
done
echo ""

# Suggest branch naming convention
echo "=== Suggested Branch Naming Convention ==="
echo "feat/    - New features"
echo "fix/     - Bug fixes"
echo "docs/    - Documentation changes"
echo "test/    - Test additions/changes"
echo "refactor/- Code refactoring"
echo "chore/   - Maintenance tasks"
echo ""

# Commands to clean up
echo "=== Cleanup Commands ==="
echo "# Delete local branches merged into main:"
echo "git branch --merged main | grep -v main | xargs -n 1 git branch -d"
echo ""
echo "# Delete remote tracking branches that no longer exist:"
echo "git remote prune origin"
echo ""
echo "# Delete a specific remote branch:"
echo "# git push origin --delete <branch-name>"
echo ""
echo "# Delete a specific local branch:"
echo "# git branch -D <branch-name>"