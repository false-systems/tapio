# Development Documentation

This section contains guides and resources for Tapio development.

## 📋 Contents

### Setup & Environment
- [Development Setup](./DEVELOPMENT_SETUP.md) - Complete development environment setup
- [Docker Build](./docker-build.md) - Container build processes and configuration
- [Cross-platform Support](./cross-platform-support.md) - Multi-platform development considerations

### CI/CD & Automation
- [CI Optimization](./CI_OPTIMIZATION.md) - Continuous integration best practices and optimization

### Component Development
- [Lightweight Collector](./lightweight-collector.md) - Building lightweight data collectors
- [eBPF Decoupling](./ebpf-decoupling.md) - eBPF component architecture and decoupling strategies
- [eBPF Parsing Fixes](./ebpf-parsing-fixes.md) - eBPF data parsing and processing improvements

## 🚀 Quick Start for New Developers

1. **Environment Setup**: Start with [Development Setup](./DEVELOPMENT_SETUP.md)
2. **Build System**: Review [Docker Build](./docker-build.md) for containerization
3. **CI/CD**: Understand [CI Optimization](./CI_OPTIMIZATION.md) for efficient development
4. **Component Development**: Choose relevant component guides based on your work area

## 🔧 Development Standards

- Follow the 5-level architecture hierarchy
- Ensure independent package builds (`cd pkg/X && go build ./...`)
- Maintain test coverage (minimum 80%)
- Use proper error handling with context
- No stubs or placeholder implementations in main branch

## 📞 Getting Help

- Check existing documentation first
- Review architecture guides for context
- Submit issues for development environment problems
- Contribute improvements to development processes