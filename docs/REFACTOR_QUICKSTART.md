# 🚀 Quick Start: Refactor Mode

## Immediate Actions for Your Mega Refactor

### 1. Switch to Refactor Mode (NOW)

```bash
# Activate lightweight configuration
make refactor-mode

# Verify it worked
make show-changed-packages
```

**What this does:**
- ✅ Backs up your current pre-commit config
- ✅ Switches to lightweight validation (changed files only)
- ✅ Reinstalls pre-commit hooks with new config
- ⚡ Pre-commit now runs in ~5-30 seconds vs 5-15 minutes

### 2. Use During Development

```bash
# Quick check before each commit
make refactor-quick-check

# Build only what you changed
make refactor-build-changed

# Test only what you changed
make refactor-test-changed

# Full refactor validation (still fast)
make refactor-validate
```

### 3. CI Behavior Changes

**Your current branch (`fix/correlation-type-safety-hardcoded-data`):**
- ✅ Uses `ci-refactor.yml` automatically (2-5 minutes)
- ✅ Only validates changed packages
- ✅ Skips expensive full-codebase operations

**When you create PR to main:**
- ⚡ Full validation runs automatically
- ⚡ Ensures production readiness before merge

## 📊 Expected Performance Improvements

| Operation | Before | After | Improvement |
|-----------|---------|--------|-------------|
| Pre-commit | 5-15 min | 5-30 sec | **95% faster** |
| CI Pipeline | 15-30 min | 2-5 min | **85% faster** |
| Local validation | 10-20 min | 1-3 min | **90% faster** |

## 🔧 What's Temporarily Relaxed

During refactor mode, these are **NOT enforced**:
- ❌ TODO/FIXME checks (for refactor comments)
- ❌ 80% coverage requirement 
- ❌ Full codebase pattern matching
- ❌ Complete test suite execution

Still **ENFORCED** (essential safety):
- ✅ Code formatting (gofmt, goimports)
- ✅ Compilation (go build)
- ✅ Architecture hierarchy rules
- ✅ Basic go vet checks

## ⚠️ Important Reminders

1. **This is temporary** - Switch back after refactor
2. **Test changed packages** - Don't skip testing entirely
3. **Architecture still matters** - Dependency rules still apply
4. **Format as you go** - Keep code formatted

## 🔄 When to Switch Back

Run the post-refactor validation:

```bash
# Check if you're ready for production mode
./scripts/post-refactor-validation.sh

# If it passes, switch back
make production-mode

# Verify everything still works
make ci-local
```

**Switch back when:**
- ✅ Major refactor complete
- ✅ All TODOs resolved
- ✅ Coverage back to 80%+
- ✅ Post-refactor validation passes

## 🎯 Quick Commands Reference

```bash
# SETUP
make refactor-mode              # Switch to lightweight mode

# DAILY DEVELOPMENT  
make refactor-quick-check       # Fast pre-commit style check
make refactor-validate          # Full but fast validation

# MONITORING
make show-changed-packages      # See what would be tested
git status                      # Check what you've changed

# COMPLETION
./scripts/post-refactor-validation.sh  # Check if ready
make production-mode            # Switch back to full validation
```

## 💡 Pro Tips

1. **Commit frequently** - Small commits = faster validation
2. **Fix formatting immediately** - `make fmt` after changes
3. **Test incrementally** - Don't accumulate untested changes
4. **Monitor CI** - Check that refactor workflow is running
5. **Document TODOs** - Use them for refactor notes, clean up later

---

**You're all set! Start refactoring with fast feedback loops.** 🚀