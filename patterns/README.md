# K8s Behavior Patterns

This is the **SINGLE SOURCE OF TRUTH** for all behavior patterns.

## Directory Structure

```
patterns/
└── behavior/          # K8s behavior patterns
    ├── oom-cascade.yaml
    ├── config-drift.yaml
    ├── network-partition.yaml
    └── storage-saturation.yaml
```

## Pattern Format

All patterns are YAML files following this structure:

```yaml
id: unique-pattern-id
name: Human Readable Name
category: memory|config|network|storage
severity: low|medium|high|critical
enabled: true
conditions:
  - event_type: kernel|metric|system
    match:
      type: exact|regex|contains|threshold
      field: field.path.to.check
      value: expected_value
prediction_template:
  type: failure|degradation
  message: What will happen
  time_horizon: 10m
```

## Adding New Patterns

1. Create a new YAML file in `patterns/behavior/`
2. Follow the structure above
3. The engine hot-reloads patterns automatically (no restart needed)

## NO DUPLICATES

**DO NOT** create patterns in:
- `configs/patterns/` ❌
- `pkg/intelligence/behavior/patterns/` ❌
- Any other location ❌

**ONLY HERE** ✅