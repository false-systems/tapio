#!/bin/bash

echo "🔍 Tapio Branch Analysis Report"
echo "==============================="
echo ""
echo "Generated on: $(date)"
echo ""

# Categories
merged=()
dependabot=()
feature=()
fix=()
old=()
active=()

# Analyze branches
while IFS= read -r branch; do
    branch_name=${branch#origin/}
    
    # Skip main
    if [[ "$branch_name" == "main" ]] || [[ "$branch_name" == "HEAD"* ]]; then
        continue
    fi
    
    # Check if merged
    if git branch -r --merged main | grep -q "$branch"; then
        merged+=("$branch_name")
    fi
    
    # Categorize
    if [[ "$branch_name" == dependabot/* ]]; then
        dependabot+=("$branch_name")
    elif [[ "$branch_name" == feature/* ]] || [[ "$branch_name" == feat/* ]]; then
        feature+=("$branch_name")
    elif [[ "$branch_name" == fix/* ]]; then
        fix+=("$branch_name")
    fi
    
    # Check age
    days=$(git log -1 --format="%ct" $branch)
    now=$(date +%s)
    diff=$((($now - $days) / 86400))
    
    if [ $diff -gt 30 ]; then
        age=$(git log -1 --format="%cr" $branch)
        old+=("$branch_name ($age)")
    elif [ $diff -lt 7 ]; then
        age=$(git log -1 --format="%cr" $branch)
        active+=("$branch_name ($age)")
    fi
done < <(git branch -r)

# Report
echo "📊 SUMMARY"
echo "----------"
echo "Total branches: $(git branch -r | grep -v HEAD | wc -l)"
echo "Merged branches: ${#merged[@]}"
echo "Dependabot branches: ${#dependabot[@]}"
echo "Feature branches: ${#feature[@]}"
echo "Fix branches: ${#fix[@]}"
echo "Branches >30 days old: ${#old[@]}"
echo "Active branches (<7 days): ${#active[@]}"
echo ""

echo "✅ SAFE TO DELETE (Already Merged)"
echo "-----------------------------------"
printf '%s\n' "${merged[@]}" | sort
echo ""

echo "🤖 DEPENDABOT BRANCHES"
echo "----------------------"
printf '%s\n' "${dependabot[@]}" | sort
echo ""

echo "⚡ ACTIVE BRANCHES (Last 7 days)"
echo "--------------------------------"
printf '%s\n' "${active[@]}" | sort
echo ""

echo "📅 OLD BRANCHES (>30 days)"
echo "--------------------------"
printf '%s\n' "${old[@]}" | sort
echo ""

echo "💡 RECOMMENDATIONS"
echo "------------------"
echo "1. Delete ${#merged[@]} merged branches (safe)"
echo "2. Delete ${#dependabot[@]} dependabot branches (likely safe)"
echo "3. Review ${#old[@]} old branches for deletion"
echo "4. Keep ${#active[@]} active branches"
echo ""

# Create deletion commands
echo "📝 DELETION COMMANDS"
echo "--------------------"
echo "# Delete all merged branches:"
echo "git branch -r --merged main | grep -v main | sed 's/origin\///' | xargs -I {} git push origin --delete {}"
echo ""
echo "# Delete dependabot branches:"
echo "git branch -r | grep dependabot | sed 's/origin\///' | xargs -I {} git push origin --delete {}"
echo ""
echo "# Delete specific branch:"
echo "git push origin --delete BRANCH_NAME"