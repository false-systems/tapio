#!/bin/bash

# create-module.sh - Create a new Tapio module with proper structure
# Usage: ./create-module.sh pkg/collectors/newcollector

set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 <module-path>"
    echo "Example: $0 pkg/collectors/docker"
    exit 1
fi

MODULE_PATH=$1
MODULE_NAME=$(basename "$MODULE_PATH")
MODULE_LEVEL=$(echo "$MODULE_PATH" | cut -d'/' -f2)
MODULE_IMPORT="github.com/falseyair/tapio/$MODULE_PATH"

echo "📦 Creating module: $MODULE_PATH"
echo "   Name: $MODULE_NAME"
echo "   Level: $MODULE_LEVEL"
echo "   Import: $MODULE_IMPORT"

# Create directory structure
mkdir -p "$MODULE_PATH"/{core,internal,linux,darwin,windows,stub,cmd,testdata}

# Create go.mod with appropriate dependencies
cd "$MODULE_PATH"

go mod init "$MODULE_IMPORT"

# Add dependencies based on level
case "$MODULE_LEVEL" in
    "domain")
        # Level 0: No dependencies
        ;;
    "collectors")
        # Level 1: Only domain
        go mod edit -require github.com/falseyair/tapio/pkg/domain@latest
        ;;
    "intelligence")
        # Level 2: Domain + collectors
        go mod edit -require github.com/falseyair/tapio/pkg/domain@latest
        # Note: Add specific collector dependencies as needed
        ;;
    "integrations")
        # Level 3: Domain + collectors + intelligence
        go mod edit -require github.com/falseyair/tapio/pkg/domain@latest
        # Note: Add specific dependencies as needed
        ;;
    "interfaces")
        # Level 4: All lower levels
        go mod edit -require github.com/falseyair/tapio/pkg/domain@latest
        # Note: Add specific dependencies as needed
        ;;
    *)
        echo "Warning: Unknown level $MODULE_LEVEL"
        ;;
esac

# Create core interfaces
cat > core/interfaces.go << 'EOF'
package core

import (
    "context"
)

// TODO: Define interfaces specific to this module
// Follow the patterns from Claude.md
EOF

# Create core types
cat > core/types.go << 'EOF'
package core

// TODO: Define types specific to this module
EOF

# Create core errors
cat > core/errors.go << 'EOF'
package core

import "fmt"

// TODO: Define errors specific to this module
var (
    ErrNotImplemented = fmt.Errorf("not implemented")
)
EOF

# Create platform stubs
for platform in linux darwin windows; do
    cat > "${platform}/implementation.go" << EOF
//go:build ${platform}

package ${platform}

// TODO: Platform-specific implementation for ${platform}
EOF
done

# Create stub implementation
cat > stub/implementation.go << EOF
//go:build !linux && !darwin && !windows

package stub

// Stub implementation for unsupported platforms
EOF

# Create README
cat > README.md << EOF
# ${MODULE_NAME}

This module is part of the Tapio ${MODULE_LEVEL} layer.

## Overview

TODO: Describe what this module does

## Architecture

This module is at Level $(echo "$MODULE_LEVEL" | sed 's/domain/0/;s/collectors/1/;s/intelligence/2/;s/integrations/3/;s/interfaces/4/') of the Tapio architecture.

### Dependencies

$(case "$MODULE_LEVEL" in
    "domain") echo "- None (Level 0)" ;;
    "collectors") echo "- pkg/domain (Level 0)" ;;
    "intelligence") echo "- pkg/domain (Level 0)\n- pkg/collectors/* (Level 1)" ;;
    "integrations") echo "- pkg/domain (Level 0)\n- pkg/collectors/* (Level 1)\n- pkg/intelligence/* (Level 2)" ;;
    "interfaces") echo "- pkg/domain (Level 0)\n- pkg/collectors/* (Level 1)\n- pkg/intelligence/* (Level 2)\n- pkg/integrations/* (Level 3)" ;;
esac)

## Usage

TODO: Add usage examples

## Testing

\`\`\`bash
go test ./...
\`\`\`

## Implementation Status

- [ ] Core interfaces defined
- [ ] Core types implemented
- [ ] Platform-specific implementations
- [ ] Unit tests (80% coverage)
- [ ] Integration tests
- [ ] Documentation complete
EOF

# Create a basic test file
cat > core/interfaces_test.go << EOF
package core_test

import (
    "testing"
)

func TestPlaceholder(t *testing.T) {
    // TODO: Implement tests
    t.Skip("Not implemented")
}
EOF

# Create .gitignore
cat > .gitignore << EOF
# Binaries
*.exe
*.dll
*.so
*.dylib
${MODULE_NAME}

# Test binary
*.test

# Output of go coverage
*.out

# Dependency directories
vendor/

# IDE
.idea/
.vscode/
*.swp
*.swo
*~
EOF

# Run go mod tidy
go mod tidy

cd - > /dev/null

echo "✅ Module created successfully at $MODULE_PATH"
echo ""
echo "Next steps:"
echo "1. Update the interfaces in $MODULE_PATH/core/interfaces.go"
echo "2. Implement the module functionality"
echo "3. Add tests with 80% coverage"
echo "4. Update the README.md"
echo "5. Tag the module when complete: git tag $MODULE_PATH/v1.0.0"