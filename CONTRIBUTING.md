# Contributing to Tapio

Thank you for your interest in contributing to Tapio! This guide will help you get started.

## 📋 Table of Contents

1. [Code of Conduct](#code-of-conduct)
2. [Getting Started](#getting-started)
3. [Development Process](#development-process)
4. [Git Workflow](#git-workflow)
5. [Code Standards](#code-standards)
6. [Testing](#testing)
7. [Documentation](#documentation)

## Code of Conduct

Please be respectful and constructive in all interactions.

## Getting Started

1. Fork the repository
2. Clone your fork:
   ```bash
   git clone git@github.com:your-username/tapio.git
   cd tapio
   ```
3. Add upstream remote:
   ```bash
   git remote add upstream git@github.com:yairfalse/tapio.git
   ```
4. Install dependencies:
   ```bash
   go mod download
   ```

## Development Process

1. **Check existing issues** - Look for existing issues or create a new one
2. **Discuss large changes** - For significant changes, discuss first in an issue
3. **Follow architecture** - Respect the 5-level architecture hierarchy
4. **Write tests** - Maintain 80%+ test coverage
5. **Document changes** - Update relevant documentation

## Git Workflow

See [docs/GIT_WORKFLOW.md](docs/GIT_WORKFLOW.md) for detailed git workflow.

### Quick Reference:

1. **Branch naming**: `<type>/<description>`
   - `feat/` - New features
   - `fix/` - Bug fixes
   - `docs/` - Documentation
   - `test/` - Tests
   - `refactor/` - Refactoring
   - `chore/` - Maintenance

2. **Commit messages**: Follow [Conventional Commits](https://www.conventionalcommits.org/)
   ```
   feat(systemd): add journald log collection
   
   - Added journal.go for minimal journald reader
   - Integrated with existing systemd collector
   
   Closes #123
   ```

3. **PR process**:
   - Create feature branch from `main`
   - Make changes with meaningful commits
   - Ensure all checks pass
   - Create PR using template
   - Wait for review

## Code Standards

### Go Code Style

1. **Format code**: Always run `gofmt -w .`
2. **Lint code**: Run `go vet ./...`
3. **No stubs**: No TODO comments or unimplemented functions
4. **Error handling**: Always handle errors with context
5. **No `interface{}`**: Use proper types

### Architecture Rules

Follow the 5-level hierarchy strictly:

```
Level 0: pkg/domain/       # Zero dependencies
Level 1: pkg/collectors/   # Domain only
Level 2: pkg/intelligence/ # Domain + L1
Level 3: pkg/integrations/ # Domain + L1 + L2
Level 4: pkg/interfaces/   # All above
```

Components can ONLY import from lower levels.

### Collector Standards

All collectors must:
- Implement the `collectors.Collector` interface
- Return `RawEvent` through `Events()` channel
- Have NO business logic
- Include K8s metadata when relevant
- Achieve 80%+ test coverage

## Testing

### Unit Tests
```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific package
go test ./pkg/collectors/systemd/...
```

### Integration Tests
```bash
# Run integration tests
./test/run-tests.sh
```

### Test Requirements
- Minimum 80% coverage for new code
- Test both success and failure cases
- Use table-driven tests where appropriate
- Mock external dependencies

## Documentation

### Code Documentation
- Add godoc comments to all exported types and functions
- Include examples for complex functionality
- Keep comments concise and clear

### README Files
- Each package should have a README.md
- Include purpose, usage, and examples
- Keep documentation up-to-date

### Architecture Documentation
- Update diagrams when changing architecture
- Document design decisions
- Include rationale for changes

## Verification Before PR

Run these commands before creating a PR:

```bash
# Format code
gofmt -w .

# Check formatting
gofmt -l . | grep -v vendor | wc -l  # Should output: 0

# Lint code
go vet ./...

# Run tests
go test ./...

# Build
go build ./...
```

## Getting Help

- Create an issue for bugs or feature requests
- Join discussions in existing issues
- Check [docs/](docs/) for more documentation

Thank you for contributing to Tapio! 🎉