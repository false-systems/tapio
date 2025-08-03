# Tapio Git Workflow Guide

## 🎯 Core Principles

1. **Keep `main` stable** - Always deployable
2. **Feature branches** - All work happens in branches
3. **Small, focused PRs** - Easier to review and merge
4. **Clean history** - Meaningful commits, no clutter

## 🌿 Branch Naming Convention

```
<type>/<short-description>
```

### Types:
- `feat/` - New features
- `fix/` - Bug fixes
- `docs/` - Documentation changes
- `test/` - Test additions/changes
- `refactor/` - Code refactoring
- `chore/` - Maintenance tasks
- `perf/` - Performance improvements

### Examples:
✅ Good:
- `feat/journald-systemd-collector`
- `fix/memory-leak-ebpf`
- `docs/update-architecture`

❌ Bad:
- `feature/nats-correlation-integration` (use `feat/`)
- `my-branch`
- `test123`

## 🔄 Workflow Steps

### 1. Start New Work
```bash
# Update main
git checkout main
git pull origin main

# Create feature branch
git checkout -b feat/your-feature-name
```

### 2. Make Changes
```bash
# Work on your feature
# Commit frequently with meaningful messages

# Before committing:
gofmt -w .                    # Format code
go test ./...                 # Run tests
go vet ./...                  # Check for issues
```

### 3. Commit Standards

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <subject>

<body>

<footer>
```

Examples:
```bash
git commit -m "feat(systemd): add journald log collection

- Added journal.go for minimal journald reader
- Integrated with existing systemd collector
- Maintains RawEvent architecture

Closes #123"
```

### 4. Keep Branch Updated
```bash
# Regularly sync with main
git fetch origin
git rebase origin/main
```

### 5. Push and Create PR
```bash
# Push your branch
git push -u origin feat/your-feature-name

# Create PR via GitHub
```

## 📋 PR Requirements

Before creating a PR, ensure:

- [ ] All tests pass: `go test ./...`
- [ ] Code is formatted: `gofmt -w .`
- [ ] No vet issues: `go vet ./...`
- [ ] Commits follow convention
- [ ] Branch is up-to-date with main
- [ ] PR description explains the change

## 🚫 What NOT to Do

1. **Never force push to main**
2. **Don't commit directly to main**
3. **Avoid large, unfocused PRs**
4. **Don't leave failing tests**
5. **No TODO/stub code in production**

## 🧹 Branch Hygiene

### After PR is Merged:
1. Delete feature branch on GitHub (automatic if configured)
2. Delete local branch:
   ```bash
   git checkout main
   git pull origin main
   git branch -d feat/your-feature-name
   ```

### Regular Cleanup:
```bash
# Remove merged branches
git branch --merged main | grep -v main | xargs -n 1 git branch -d

# Prune remote references
git remote prune origin

# Check branch status
./scripts/branch-cleanup.sh
```

## 🔒 Protected Branches

`main` branch protection includes:
- Require PR reviews (1+)
- Require status checks to pass
- Require branches to be up-to-date
- Include administrators in restrictions
- Automatically delete head branches

## 🎣 Git Hooks (Optional)

Install pre-commit hooks:
```bash
# Create pre-commit hook
cat > .git/hooks/pre-commit << 'EOF'
#!/bin/bash
# Format check
if ! gofmt -l . | grep -q .; then
    echo "❌ Code not formatted. Run: gofmt -w ."
    exit 1
fi

# Run tests
if ! go test ./... -short; then
    echo "❌ Tests failed"
    exit 1
fi

echo "✅ Pre-commit checks passed"
EOF

chmod +x .git/hooks/pre-commit
```

## 🆘 Common Issues

### Accidentally committed to main:
```bash
# Create branch from current state
git checkout -b feat/my-feature

# Reset main to origin
git checkout main
git reset --hard origin/main
```

### Need to undo last commit:
```bash
git reset --soft HEAD~1  # Keep changes
# or
git reset --hard HEAD~1  # Discard changes
```

### Merge conflicts:
```bash
# Update your branch
git fetch origin
git rebase origin/main

# Fix conflicts, then:
git add .
git rebase --continue
```

## 📊 Git Aliases (Recommended)

Add to `~/.gitconfig`:
```ini
[alias]
    st = status -sb
    co = checkout
    br = branch
    cm = commit -m
    unstage = reset HEAD --
    last = log -1 HEAD
    visual = !gitk
    cleanup = !git branch --merged main | grep -v main | xargs -n 1 git branch -d
```