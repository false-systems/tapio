# Tapio Test Environment

All test-related files are organized here to keep the main codebase clean.

## Structure

```
test/
├── README.md                 # This file
├── setup.sh                  # Main setup script
├── run-tests.sh             # Test runner
├── view-logs.sh             # Log viewer
├── docker/                  # Test-specific Dockerfiles
│   ├── Dockerfile.correlation
│   └── Dockerfile.collector
├── k8s/                     # Test K8s manifests
│   ├── correlation-service.yaml
│   └── collectors.yaml
└── scenarios/               # Test scenarios
    ├── oom-killer.yaml
    ├── crash-loop.yaml
    ├── cpu-stress.yaml
    ├── network-failure.yaml
    └── disk-filler.yaml
```

## Quick Start

```bash
cd test/
./setup.sh                   # Setup test environment
./run-tests.sh all          # Run all test scenarios
./view-logs.sh              # View logs
```

## Test Scenarios

1. **OOM Kill** - Memory exhaustion leading to pod termination
2. **Crash Loop** - Application repeatedly crashing and restarting
3. **CPU Stress** - High CPU usage and resource contention
4. **Network Failure** - Connection failures and timeouts
5. **Disk Pressure** - Storage exhaustion

Each scenario is designed to trigger specific patterns that Tapio should detect.