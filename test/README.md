# Tapio Test Suite

This directory contains the comprehensive test suite for Tapio, organized into different test categories.

## Test Structure

```
test/
├── unit/              # Unit tests (in individual packages)
├── integration/       # Integration tests with mocked K8s
├── e2e/              # End-to-end tests with real clusters
└── benchmark/        # Performance benchmarks
```

## Running Tests

### Unit Tests
Run all unit tests:
```bash
task test:unit
# or
go test -v -race -short ./...
```

Run specific package tests:
```bash
task test:health
task test:simple
task test:k8s
task test:metrics
```

### Integration Tests
Run integration tests (requires build tag):
```bash
task test:integration
# or
go test -v -tags=integration ./test/integration/...
```

### E2E Tests
Run end-to-end tests (requires real/kind cluster):
```bash
task test:e2e
# or
go test -v -tags=e2e ./test/e2e/...
```

### Test Coverage
Generate coverage report:
```bash
task test:coverage
# Opens coverage.html in browser
```

## Writing Tests

### Unit Tests
- Test individual functions and methods
- Use table-driven tests for multiple scenarios
- Mock external dependencies
- Keep tests fast and isolated

Example:
```go
func TestAnalyzePod(t *testing.T) {
    tests := []struct {
        name     string
        pod      *v1.Pod
        expected Status
    }{
        // test cases...
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test implementation
        })
    }
}
```

### Integration Tests
- Test component interactions
- Use fake K8s clients
- Test complete workflows
- Verify metrics and outputs

### E2E Tests
- Test against real Kubernetes clusters
- Use Kind or Minikube for CI
- Test actual pod behaviors
- Verify end-user scenarios

## Test Dependencies

- `testify`: Assertions and mocking
- `fake`: Kubernetes fake client
- `kind`: Local Kubernetes clusters for E2E

## CI/CD Integration

Tests are automatically run in GitHub Actions:
- Unit tests on every PR
- Integration tests on PR approval
- E2E tests on merge to main
- Coverage reports uploaded to codecov

## Performance Testing

Run benchmarks:
```bash
task bench
# or
go test -bench=. -benchmem ./...
```

## Debugging Tests

Run tests with verbose output:
```bash
go test -v -run TestName ./pkg/...
```

Run with race detector:
```bash
go test -race ./...
```

## Test Guidelines

1. **Fast**: Unit tests should complete in milliseconds
2. **Isolated**: Tests should not depend on external services
3. **Repeatable**: Tests should produce consistent results
4. **Clear**: Test names should describe what they test
5. **Comprehensive**: Cover happy paths and error cases