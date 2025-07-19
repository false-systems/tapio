# CLI Interface

This module provides the command-line interface for Tapio.

## Architecture

This interface is at Level 4 of Tapio's architecture and can import from all lower levels:

```
Dependencies:
- pkg/domain (Level 0) - Core types
- pkg/collectors/* (Level 1) - Direct collector access
- pkg/intelligence/* (Level 2) - Analysis capabilities
- pkg/integrations/* (Level 3) - External integrations
```

## Commands

### Core Commands

```bash
# System health check
tapio check [--all|--collectors|--intelligence|--integrations]

# Event collection
tapio collect --source=<source> [--filter=<filter>] [--output=<format>]

# Analysis
tapio analyze correlations --last=<duration>
tapio analyze patterns --type=<pattern>
tapio analyze predictions

# Configuration
tapio config show
tapio config validate
tapio config set <key> <value>
```

### Output Formats

All commands support multiple output formats:
- `--output=json` - JSON output
- `--output=yaml` - YAML output
- `--output=table` - Table format (default)
- `--output=human` - Human-readable descriptions

### Examples

```bash
# Check system health
tapio check --all

# Collect eBPF events for last hour
tapio collect --source=ebpf --last=1h --output=json

# Analyze correlations with human-readable output
tapio analyze correlations --last=30m --output=human

# Show current configuration
tapio config show
```

## Implementation Status

- [ ] Core CLI structure with cobra
- [ ] Check command
- [ ] Collect command
- [ ] Analyze command
- [ ] Config command
- [ ] Output formatters
- [ ] Interactive mode
- [ ] Shell completion