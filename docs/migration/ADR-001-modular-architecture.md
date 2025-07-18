# ADR-001: Adopt Strict Modular Architecture

## Status
Accepted

## Context
Tapio has grown organically, resulting in:
- 6 different correlation engine implementations (~77K lines)
- Circular dependencies between components
- Unclear architectural boundaries
- Difficulty in maintaining and testing components
- "Agents doing what they want" without architectural enforcement

## Decision
We will adopt a strict 5-level modular architecture with compiler-enforced boundaries:

```
Level 0: pkg/domain/          # Zero dependencies
Level 1: pkg/collectors/      # Only domain
Level 2: pkg/intelligence/    # Only domain + collectors
Level 3: pkg/integrations/    # Only domain + collectors + intelligence
Level 4: pkg/interfaces/      # All lower levels
```

Each component will:
- Have its own go.mod file
- Be independently versioned
- Build and test in isolation
- Follow strict dependency rules enforced by the Go compiler

## Consequences

### Positive
- **Automatic enforcement**: Violations fail at compile time
- **Clear boundaries**: No ambiguity about where code belongs
- **Independent development**: Teams can work on components without conflicts
- **Better testing**: Each component can be tested in isolation
- **Gradual migration**: Can migrate one component at a time

### Negative
- **Initial complexity**: More go.mod files to manage
- **Learning curve**: Developers need to understand the architecture
- **Migration effort**: Significant work to reorganize existing code
- **Versioning overhead**: Must manage versions between components

### Mitigation
- Use go.work for local development
- Automate version management with scripts
- Provide clear documentation and examples
- Implement gradually over 7 weeks

## Implementation Plan
See TAPIO_ARCHITECTURE_MIGRATION_PLAN.md for detailed phases.

## References
- Claude.md - Architecture constraints
- Go modules documentation
- "A Story of Monorepos in Go" article