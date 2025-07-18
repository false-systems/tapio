# Root Directory Cleanup Analysis

## Current State
- **41 files** in root directory (excluding directories)
- **22 MD files** - many are temporary work/analysis documents
- **3 shell scripts** in root

## Files to Keep

### Essential Documentation
- `README.md` - Main project documentation
- `Claude.md` - AI agent configuration (important!)
- `.gitignore` - Git configuration
- `.golangci.yml` - Go linting configuration
- `.pre-commit-config.yaml` - Pre-commit hooks
- `codecov.yml` - Code coverage configuration
- `docker-compose.build.yml` - Docker compose for builds
- `Taskfile.yml` - Task runner configuration
- `go.mod`, `go.sum` - Go module files
- `go.work`, `go.work.sum` - Go workspace files
- `Makefile` - Build automation

## Files to Remove

### Temporary Analysis Documents (can move to docs/archive/)
- `BUILD_ERRORS_AUDIT.md` - Old build analysis
- `BUILD_FIX_PROGRESS.md` - Old build fix tracking
- `BUILD_FIX_SUMMARY.md` - Old build fix summary
- `BUILD_STATUS_SUMMARY.md` - Old build status
- `BUILD_SYSTEM_ANALYSIS.md` - Old build system analysis
- `BUILD_SYSTEM_FIX_SUMMARY.md` - Old build system fix summary
- `CI_IMPLEMENTATION_SUMMARY.md` - CI implementation notes
- `CLEANUP_COMPLETED.md` - Old cleanup notes
- `CREATE_PR_COMMAND.md` - PR command notes
- `DECOUPLING_PLAN.md` - Old decoupling plan
- `MODULAR_BUILD_PLAN.md` - Old modular build plan
- `MODULAR_BUILD_SUCCESS.md` - Old build success notes
- `PR_READY.md` - Old PR notes
- `REPOSITORY_AUDIT.md` - Old repository audit
- `REST_API_IMPLEMENTATION.md` - API implementation notes
- `ROOT_CLEANUP_SUMMARY.md` - Previous cleanup summary
- `test-formatter.md` - Test formatter notes

### Recent Analysis Documents (keep temporarily, then archive)
- `HEALTH_METRICS_ANALYSIS.md` - Recent health analysis
- `INFRASTRUCTURE_CLEANUP_REPORT.md` - Recent infrastructure audit
- `CLEANUP_ACTION_PLAN.md` - Active cleanup plan

### Scripts to Move
- `format.sh` → `scripts/format.sh`
- `test_rest_integration.sh` → `scripts/test_rest_integration.sh`
- `test-modular-builds.sh` → `scripts/test-modular-builds.sh`

## Proposed Structure

```
tapio/
├── README.md
├── Claude.md
├── .gitignore
├── .golangci.yml
├── .pre-commit-config.yaml
├── codecov.yml
├── docker-compose.build.yml
├── Taskfile.yml
├── Makefile
├── go.mod, go.sum
├── go.work, go.work.sum
├── docs/
│   ├── architecture/
│   ├── development/
│   └── archive/         # Old analysis/planning docs
└── scripts/
    ├── format.sh
    ├── test_rest_integration.sh
    ├── test-modular-builds.sh
    └── infrastructure-audit.sh
```

## Cleanup Commands

```bash
# Create archive directory
mkdir -p docs/archive

# Move old analysis documents
mv BUILD_*.md CLEANUP_COMPLETED.md CREATE_PR_COMMAND.md DECOUPLING_PLAN.md \
   MODULAR_*.md PR_READY.md REPOSITORY_AUDIT.md REST_API_IMPLEMENTATION.md \
   ROOT_CLEANUP_SUMMARY.md test-formatter.md \
   docs/archive/

# Move recent analysis (keep for now)
mkdir -p docs/analysis
mv HEALTH_METRICS_ANALYSIS.md INFRASTRUCTURE_CLEANUP_REPORT.md CLEANUP_ACTION_PLAN.md \
   docs/analysis/

# Move scripts
mv format.sh test_rest_integration.sh test-modular-builds.sh scripts/
```

## Result
Root directory will be reduced from **41 files** to approximately **15 files** (only essential project files).