#!/bin/bash
set -e

AGENT_ID=${1:-"unknown"}
COMPONENT=${2:-"general"}
ACTION=${3:-"work"}

if [ "$AGENT_ID" = "unknown" ]; then
    echo "❌ Usage: $0 <agent-id> <component> <action>"
    echo "   Example: $0 agent-1 cli enhancement"
    exit 1
fi

BRANCH_NAME="feature/${AGENT_ID}/${COMPONENT}-${ACTION}"

echo "🌿 Creating branch: $BRANCH_NAME"

# Switch to develop and update
git checkout develop || git checkout main
git pull origin $(git branch --show-current)

# Create or switch to branch
if git show-ref --verify --quiet refs/heads/${BRANCH_NAME}; then
    echo "⚠️  Branch exists, switching..."
    git checkout ${BRANCH_NAME}
    git merge develop --no-edit
else
    echo "✨ Creating new branch..."
    git checkout -b ${BRANCH_NAME}
fi

# Create task tracking
TASK_FILE=".agent-work/task-${AGENT_ID}-${COMPONENT}-${ACTION}.md"
cat > "$TASK_FILE" << TASKEOF
# Task: ${COMPONENT} ${ACTION}

**Agent**: ${AGENT_ID}
**Branch**: ${BRANCH_NAME}
**Started**: $(date)

## Success Criteria
- [ ] Code follows standards
- [ ] Tests passing (>70% coverage)
- [ ] \`make agent-check\` passes
- [ ] PR ready (< 200 lines)

## Commands Before Done
\`\`\`bash
make fmt
make agent-check
git add .
git commit -m "feat(${COMPONENT}): ${ACTION}"
git push origin ${BRANCH_NAME}
\`\`\`
TASKEOF

echo "✅ Branch setup complete!"
echo "📋 Task file: $TASK_FILE"
echo "🚀 Ready to work on: $COMPONENT $ACTION"