# eBPF + Minikube Testing Suite

This directory contains comprehensive tests for Tapio's eBPF kernel-level monitoring capabilities.

## 🎯 **Test Scenarios**

### **Memory Leak Detection Tests**
- `memory-leak-app/` - Intentional memory leak applications
- `gradual-leak/` - Slow memory growth patterns  
- `spike-leak/` - Sudden memory allocation bursts

### **OOM Prediction Tests**
- `oom-prediction/` - Apps designed to hit memory limits
- `timing-accuracy/` - Validate prediction timing precision
- `confidence-scoring/` - Test confidence level accuracy

### **Process Correlation Tests**
- `multi-container/` - Test pod-to-process mapping
- `sidecar-apps/` - Complex container scenarios
- `job-completion/` - Job vs long-running process detection

## 🚀 **Quick Start**

```bash
# Setup test environment
./setup-test-env.sh

# Run all eBPF tests
./run-ebpf-tests.sh

# Run specific test scenario
./test-memory-leak.sh

# Validate predictions
./validate-predictions.sh
```

## 📊 **Expected Results**

Each test generates:
- **Tapio predictions** with confidence scores
- **Actual outcomes** for validation
- **Accuracy metrics** comparing predictions vs reality
- **Performance data** on detection speed

## 🧪 **Test Applications**

All test apps are designed to:
- Create **predictable resource patterns**
- Generate **measurable outcomes**
- Allow **timing validation**
- Produce **reproducible results**