#!/bin/bash
set -e

echo "🔧 Setting up pre-commit hooks for local quality checks..."

# Check if pre-commit is installed
if ! command -v pre-commit &> /dev/null; then
    echo "📦 Installing pre-commit..."
    
    # Try different installation methods
    if command -v pip3 &> /dev/null; then
        pip3 install pre-commit
    elif command -v pip &> /dev/null; then
        pip install pre-commit
    elif command -v brew &> /dev/null; then
        brew install pre-commit
    else
        echo "❌ Could not find pip or brew to install pre-commit"
        echo "Please install pre-commit manually: https://pre-commit.com/#installation"
        exit 1
    fi
fi

# Install the git hook scripts
echo "🎣 Installing pre-commit hooks..."
pre-commit install

# Run hooks on all files to test setup
echo "🧪 Testing hooks on all files..."
pre-commit run --all-files || {
    echo "⚠️ Some hooks failed, but that's expected on first run"
    echo "The hooks are now installed and will run on future commits"
}

echo "✅ Pre-commit hooks setup complete!"
echo ""
echo "📋 Local-first development workflow:"
echo "1. Make changes to your code"
echo "2. Run 'make agent-check' before committing"
echo "3. Commit your changes (hooks will run automatically)"
echo "4. Push to remote repository"
echo ""
echo "🛠️ Manual hook execution:"
echo "- Run all hooks: pre-commit run --all-files"
echo "- Run specific hook: pre-commit run <hook-id>"
echo "- Skip hooks: git commit --no-verify"