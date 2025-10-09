# PR Title
<!-- One-line summary: feat(scope): what was done -->

## 🎯 Why This Change?
<!-- The rationale - why did we need this? What problem does it solve? -->

**Problem:**


**Solution:**


**Impact:**


## 🔧 What Changed?
<!-- High-level summary of what was implemented -->

**Key Components:**
-
-
-

**Architecture Changes:**
<!-- Any changes to system architecture, dependencies, or interfaces -->


## 🏗️ How It Works
<!-- Technical implementation details -->

**Implementation Details:**


**Design Decisions:**
<!-- Why did you choose this approach over alternatives? -->


**Trade-offs:**
<!-- What did you optimize for? What did you sacrifice? -->


## 🔍 What Should Reviewers Check?
<!-- Guide reviewers on what to focus on -->

**Critical Paths:**
1.
2.

**Edge Cases to Verify:**
-
-

**Performance Considerations:**
-

**Security Concerns:**
-

## ✅ Testing & Verification
<!-- How did you verify this works? -->

**Test Coverage:**
```bash
# Test results
$ go test ./... -cover
# <paste coverage results>
```

**Manual Testing:**
<!-- What did you test manually? -->


**Load/Performance Testing:**
<!-- If applicable -->


## 📊 Metrics & Observability
<!-- What metrics are exposed? How do we monitor this? -->

**New Metrics:**
-

**Dashboards/Alerts:**


## 🚀 Deployment & Migration
<!-- What needs to happen when this is deployed? -->

**Breaking Changes:**
<!-- None, or describe what breaks and how to migrate -->


**Configuration Changes:**
<!-- Any new env vars, config files, or flags? -->


**Dependencies:**
<!-- New dependencies added or version bumps -->


## 🔗 Related Work
<!-- Context and related PRs/issues -->

**Depends On:**
-

**Blocks:**
-

**Related:**
-

Closes #

---

## 📝 Verification Checklist
<!-- Quick sanity checks - mark with 'x' when done -->
- [ ] All tests pass (`go test ./...`)
- [ ] Coverage >= 80% per package
- [ ] No TODOs, FIXMEs, or stubs
- [ ] No `map[string]interface{}` (typed structs only)
- [ ] Architecture hierarchy followed (5-level)
- [ ] OTEL metrics properly instrumented
- [ ] Error handling with context wrapping
- [ ] Resources properly cleaned up (defer, Close())
- [ ] Race detector clean (`go test -race`)
- [ ] `make verify-full` passes

🤖 Generated with [Claude Code](https://claude.com/claude-code)
