#!/bin/bash

# move-package.sh - Move a package to a new location and update imports
# Usage: ./move-package.sh <source> <destination>

set -euo pipefail

if [[ $# -ne 2 ]]; then
    echo "Usage: $0 <source> <destination>"
    echo "Example: $0 pkg/otel pkg/integrations/otel"
    exit 1
fi

SOURCE=$1
DEST=$2
SOURCE_IMPORT="github.com/falseyair/tapio/$SOURCE"
DEST_IMPORT="github.com/falseyair/tapio/$DEST"

echo "📦 Moving package: $SOURCE → $DEST"
echo "   Old import: $SOURCE_IMPORT"
echo "   New import: $DEST_IMPORT"

# Check source exists
if [[ ! -d "$SOURCE" ]]; then
    echo "❌ Source directory not found: $SOURCE"
    exit 1
fi

# Check destination doesn't exist
if [[ -d "$DEST" ]]; then
    echo "❌ Destination already exists: $DEST"
    exit 1
fi

# Create destination directory
mkdir -p "$(dirname "$DEST")"

# Move the package
echo "📁 Moving files..."
mv "$SOURCE" "$DEST"

# Update imports in the moved package
echo "🔄 Updating self-imports in moved package..."
find "$DEST" -name "*.go" -type f -exec sed -i.bak "s|\"$SOURCE_IMPORT|\"$DEST_IMPORT|g" {} \;
find "$DEST" -name "*.go.bak" -type f -delete

# Update go.mod in the moved package if it exists
if [[ -f "$DEST/go.mod" ]]; then
    echo "📝 Updating go.mod..."
    cd "$DEST"
    go mod edit -module "$DEST_IMPORT"
    cd - > /dev/null
fi

# Find and update all imports in the codebase
echo "🔍 Finding and updating imports across codebase..."
IMPORT_COUNT=0

# Find all Go files that import the old path
for file in $(find . -name "*.go" -type f -exec grep -l "\"$SOURCE_IMPORT" {} \; 2>/dev/null || true); do
    echo "   Updating: $file"
    sed -i.bak "s|\"$SOURCE_IMPORT|\"$DEST_IMPORT|g" "$file"
    rm -f "${file}.bak"
    ((IMPORT_COUNT++))
done

# Update go.mod files that require the old module
for modfile in $(find . -name "go.mod" -type f -exec grep -l "$SOURCE_IMPORT" {} \; 2>/dev/null || true); do
    echo "   Updating go.mod: $modfile"
    sed -i.bak "s|$SOURCE_IMPORT|$DEST_IMPORT|g" "$modfile"
    rm -f "${modfile}.bak"
done

# Update replace directives if any
for modfile in $(find . -name "go.mod" -type f); do
    if grep -q "replace.*$SOURCE_IMPORT" "$modfile" 2>/dev/null; then
        echo "   Updating replace directive in: $modfile"
        sed -i.bak "s|$SOURCE_IMPORT|$DEST_IMPORT|g" "$modfile"
        rm -f "${modfile}.bak"
    fi
done

# Update go.work if it exists
if [[ -f "go.work" ]]; then
    echo "📝 Updating go.work..."
    sed -i.bak "s|./$SOURCE|./$DEST|g" go.work
    rm -f go.work.bak
fi

echo ""
echo "✅ Package moved successfully!"
echo "   Updated $IMPORT_COUNT import statements"
echo ""
echo "⚠️  Next steps:"
echo "1. Run 'go mod tidy' in affected modules"
echo "2. Update any documentation references"
echo "3. Update CI/CD configurations if needed"
echo "4. Commit the changes"

# Create a git commit message template
cat > .git/MOVE_COMMIT_MSG << EOF
refactor: Move $SOURCE to $DEST

- Moved package to align with architecture hierarchy
- Updated all import statements
- Part of architecture migration plan

Old location: $SOURCE
New location: $DEST
EOF

echo ""
echo "💡 Suggested commit message saved to .git/MOVE_COMMIT_MSG"