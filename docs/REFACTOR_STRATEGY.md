# Tapio Refactor Phase CI/CD Strategy

## 🎯 Current Problem

The existing CI/CD pipeline and pre-commit hooks are designed for production-grade code quality but create barriers during active refactoring:

- **Full codebase scanning**: All hooks check entire codebase regardless of changes
- **Expensive operations**: Complete builds, tests, and coverage for minor changes  
- **Blocking development**: Slow feedback loops during rapid iteration
- **No incremental validation**: No differentiation between changed and unchanged code

## 🚀 Refactor Phase Strategy

### Phase 1: Lightweight Development Checks (Current Branch)

**Activate with:**
```bash
# Switch to lightweight pre-commit config
cp .pre-commit-config-refactor.yaml .pre-commit-config.yaml
pre-commit install

# Use refactor-phase Makefile targets
make refactor-quick-check
make refactor-build-changed
```

**What it does:**
- ✅ Format checking on changed files only
- ✅ Import organization on changed files only  
- ✅ Basic compile check for changed packages
- ✅ Quick vet on changed packages
- ❌ No TODO/FIXME enforcement (temporarily relaxed)
- ❌ No full test suite (only changed packages)
- ❌ No coverage requirements (temporarily disabled)

### Phase 2: Incremental CI Pipeline

The `ci-refactor.yml` workflow activates automatically for feature/fix branches:

**Triggers:**
- Push to `fix/*`, `feat/*`, `refactor/*`, `chore/*` branches
- Pull requests with Go file changes

**What it validates:**
- 🔍 **Changed files only**: Formatting, imports, syntax
- 🏗️ **Incremental builds**: Only changed packages + dependencies  
- 🧪 **Targeted tests**: Only test suites for changed packages
- 🏗️ **Architecture compliance**: Hierarchy rules for changed code only
- ⚡ **Fast feedback**: ~2-5 minutes vs ~15-30 minutes

### Phase 3: Branch-Specific Enforcement

```yaml
# In ci-refactor.yml - already configured
on:
  push:
    branches: [fix/*, feat/*, refactor/*, chore/*]  # Lightweight CI
  pull_request:
    branches: [main, develop]                       # Full validation before merge
```

## 🛠️ Implementation Guide

### 1. Activate Refactor Mode

```bash
# Backup current config
cp .pre-commit-config.yaml .pre-commit-config-full.yaml

# Switch to lightweight config  
cp .pre-commit-config-refactor.yaml .pre-commit-config.yaml

# Reinstall hooks
pre-commit uninstall
pre-commit install
```

### 2. Use Refactor-Specific Make Targets

```bash
# Quick validation (changed files only)
make refactor-quick-check

# Build changed packages
make refactor-build-changed  

# Test changed packages
make refactor-test-changed

# Full refactor validation
make refactor-validate
```

### 3. CI Behavior

- **Feature branches**: Use `ci-refactor.yml` (fast, incremental)
- **Main/develop PRs**: Use full `ci.yml` (comprehensive validation)
- **Main branch**: Full production-grade validation

## 📋 New Makefile Targets

### Refactor-Specific Targets

```makefile
refactor-quick-check: ## Quick validation for refactor phase
refactor-build-changed: ## Build only changed packages  
refactor-test-changed: ## Test only changed packages
refactor-validate: ## Full refactor-phase validation
```

### Utility Targets

```makefile
refactor-mode: ## Switch to refactor-phase configuration
production-mode: ## Switch back to production configuration
show-changed-packages: ## Display packages that would be tested
```

## 🔄 Post-Refactor Strategy

### When to Switch Back

Switch back to full CI when:
- ✅ Major refactor is complete
- ✅ Architecture is stabilized  
- ✅ Ready for production validation
- ✅ All TODOs/FIXMEs are resolved

### Switch Back Process

```bash
# Restore full configuration
cp .pre-commit-config-full.yaml .pre-commit-config.yaml
pre-commit install

# Run full validation
make ci-local

# Ensure all checks pass
./verify.sh
```

## ⚠️ Temporary Relaxations During Refactor

**What's temporarily disabled:**
- TODO/FIXME enforcement (for refactor comments)
- 80% coverage requirement (during restructuring)
- Full codebase pattern matching
- Complete test suite execution
- Expensive linting rules

**What remains enforced:**
- Code formatting (gofmt, goimports)
- Compilation (go build)
- Architecture hierarchy rules
- Basic go vet checks
- No panic() in production code

## 🎯 Expected Benefits

### Development Speed
- **Before**: 5-15 minute pre-commit + 15-30 minute CI
- **After**: 30 second pre-commit + 2-5 minute CI

### Developer Experience
- Fast feedback on syntax/format issues
- No blocking on unrelated code quality issues
- Focus on structural changes vs. cosmetic fixes
- Maintains essential safety nets

### Quality Assurance
- Architecture compliance preserved
- Build integrity maintained
- Critical safety checks remain active
- Full validation before main branch merge

## 📊 Monitoring & Metrics

Track the impact during refactor phase:

```bash
# Measure CI duration
gh run list --workflow=ci-refactor.yml --json duration

# Monitor changed package ratio
git log --oneline --since="1 week ago" | wc -l

# Check pre-commit performance
time pre-commit run --all-files
```

## 🚨 When to Abort Strategy

Revert to full CI if:
- Architecture violations increase significantly
- Critical bugs slip through reduced validation
- Team productivity doesn't improve
- Merge conflicts become frequent due to formatting issues

## 🔧 Customization Options

### Adjust Validation Strictness

Edit `.pre-commit-config-refactor.yaml`:
```yaml
# Make warnings into errors
entry: bash -c '... || exit 1'  # Change from "|| true"

# Add package-specific rules
entry: bash -c 'if [[ "$pkg" == *"domain"* ]]; then ...; fi'
```

### Modify CI Triggers

Edit `ci-refactor.yml`:
```yaml
# Add more branch patterns
branches: [fix/*, feat/*, refactor/*, chore/*, experiment/*]

# Change file path filters  
paths: ['pkg/domain/**', 'pkg/collectors/**']
```

---

**💡 Remember**: This is a temporary strategy for active development. Always return to full production-grade validation before merging to main.