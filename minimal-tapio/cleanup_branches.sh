#!/bin/bash

echo "🧹 Git Branch Cleanup Script for Tapio"
echo "======================================"
echo ""

# Fetch latest
echo "📡 Fetching latest branch information..."
git fetch --prune

# Show current status
echo ""
echo "📊 Current branch statistics:"
echo "Total remote branches: $(git branch -r | wc -l)"
echo "Merged branches: $(git branch -r --merged main | grep -v main | wc -l)"
echo ""

# List merged branches
echo "📋 Branches already merged into main:"
git branch -r --merged main | grep -v main | sed 's/origin\///' | sort
echo ""

# Dependabot branches
echo "🤖 Dependabot branches (usually safe to delete):"
git branch -r | grep dependabot | sed 's/origin\///' | sort
echo ""

# Old branches (>30 days)
echo "📅 Branches older than 30 days:"
for branch in $(git branch -r | grep -v HEAD | grep -v main); do 
    age=$(git log -1 --format="%cr" $branch)
    days=$(git log -1 --format="%ct" $branch)
    now=$(date +%s)
    diff=$((($now - $days) / 86400))
    if [ $diff -gt 30 ]; then
        echo "  ${branch#origin/} ($age)"
    fi
done
echo ""

# Interactive cleanup
read -p "Would you like to delete all merged branches? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "🗑️  Deleting merged branches..."
    git branch -r --merged main | grep -v main | sed 's/origin\///' | xargs -I {} git push origin --delete {}
    echo "✅ Merged branches deleted!"
fi

echo ""
read -p "Would you like to delete dependabot branches? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "🗑️  Deleting dependabot branches..."
    git branch -r | grep dependabot | sed 's/origin\///' | xargs -I {} git push origin --delete {}
    echo "✅ Dependabot branches deleted!"
fi

echo ""
echo "✨ Cleanup complete!"
echo "Remaining branches: $(git branch -r | wc -l)"