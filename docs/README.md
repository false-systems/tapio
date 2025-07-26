# Tapio Documentation

Welcome to the Tapio observability platform documentation. This guide provides comprehensive information about architecture, development, operations, and usage.

## 📋 Table of Contents

### 🏗️ [Architecture](./architecture/)
Core system design and architectural decisions
- [Overview](./ARCHITECTURE.md) - Main architecture document
- [Pipeline Design](./architecture/pipeline-design.md) - Event processing pipeline
- [Correlation Engine](./architecture/correlation-engine-architecture.md) - AI-powered correlation system
- [Performance Strategies](./architecture/performance-optimization-strategies.md) - Performance optimization approaches
- [Multi-source Integration](./architecture/multi-source-integration-design.md) - Multiple data source handling

### 🔧 [Development](./development/)
Developer guides and setup instructions
- [Setup Guide](./DEVELOPMENT_SETUP.md) - Development environment setup
- [CI Optimization](./CI_OPTIMIZATION.md) - Continuous integration best practices
- [Cross-platform Support](./cross-platform-support.md) - Multi-platform development
- [Docker Build](./docker-build.md) - Container build processes

### 📊 [Data & Events](./data/)
Event structures and data handling
- [Unified Event Design](./UNIFIED_EVENT_DESIGN.md) - Core event structure
- [Event Comparison](./UNIFIED_EVENT_COMPARISON.md) - Event format analysis
- [Enhanced Data Structures](./UNIFIED_EVENT_ENHANCED_DATA_STRUCTURES.md) - Advanced event features
- [K8s Context Design](./UNIFIED_EVENT_K8S_CONTEXT_DESIGN.md) - Kubernetes-specific events
- [Data Philosophy](./data_philosophy.md) - Data handling principles

### 🔗 [Correlation & Intelligence](./correlation/)
AI-powered correlation and analysis
- [Correlation Service](./CORRELATION_SERVICE.md) - Correlation service overview
- [Deep Dive](./CORRELATION_ENGINE_DEEP_DIVE.md) - Technical deep dive
- [Detailed Process](./CORRELATION_PROCESS_DETAILED.md) - Step-by-step correlation process
- [Philosophy](./correlation_philosophy.md) - Correlation principles
- [Multidimensional K8s Engine](./MULTIDIMENSIONAL_K8S_CORRELATION_ENGINE.md) - K8s-specific correlation

### 📡 [Collectors](./collectors/)
Data collection components
- [Kubernetes Collector](./collectors/k8s.md) - K8s event collection
- [SystemD Collector](./collectors/systemd.md) - System service monitoring
- [eBPF Collector](./collectors/ebpf.md) - Kernel-level event collection
- [CNI Collector](./collectors/cni.md) - Container network monitoring

### 🔌 [Integrations](./integrations/)
External system integrations
- [API Integration](./api-correlation-integration.md) - REST API integration
- [gRPC Integration](./GRPC_REST_API_ANALYSIS.md) - gRPC service analysis
- [OpenTelemetry](./otel-enhanced-tracing.md) - OTel enhanced tracing

### 🚀 [Operations](./operations/)
Deployment and operational guides
- [Deployment Runbook](./operations/deployment-runbook.md) - Production deployment
- [Monitoring Guide](./operations/monitoring-runbook.md) - System monitoring
- [Performance Tuning](./operations/performance-tuning.md) - Performance optimization
- [Incident Response](./operations/incident-response.md) - Incident handling procedures
- [Maintenance](./operations/maintenance-procedures.md) - Routine maintenance

### 🧪 [Testing](./testing/)
Testing strategies and frameworks
- [Black Box Testing](./BLACK_BOX_TESTING_STRATEGY.md) - External testing approach
- [Performance Benchmarks](./performance/benchmarks.md) - Performance testing

### 🏛️ [Architecture History](./architecture-history/)
Historical architecture decisions and migrations
- [Modular Architecture](./architecture-history/ADR-001-modular-architecture.md) - Core architectural decision
- [Current State](./architecture-history/current-state.md) - Current architecture status
- [Migration History](./architecture-history/MIGRATION_TRACKER.md) - Major migrations

### 📋 [Planning & Roadmaps](./planning/)
Future development plans and enhancements
- [Mission Statement](./TAPIO_MISSION_AND_CURRENT_STATE.md) - Project mission and current state
- [Enhancement Roadmap](./ENHANCEMENT_ROADMAP_AND_NEW_COLLECTORS.md) - Future enhancements
- [K8s Context Plans](./K8S_CONTEXT_CORRELATION_IMPLEMENTATION_PLAN_V2.md) - K8s correlation planning
- [Philosophical Foundation](./PHILOSOPHICAL_FOUNDATION.md) - Core principles

### 🔧 [Technical Specifications](./specs/)
Detailed technical specifications
- [eBPF Design](./DUAL_LAYER_EBPF_DESIGN.md) - Dual-layer eBPF architecture
- [Pipeline Architecture](./PIPELINE_ARCHITECTURE.md) - Processing pipeline design
- [Storage Design](./STORAGE_DESIGN.md) - Data storage architecture
- [Persistence Design](./PERSISTENCE_DESIGN.md) - Data persistence layer

## 🚀 Quick Start

1. **New to Tapio?** Start with [Mission Statement](./TAPIO_MISSION_AND_CURRENT_STATE.md)
2. **Setting up development?** Follow the [Development Setup](./DEVELOPMENT_SETUP.md)
3. **Deploying to production?** Use the [Deployment Runbook](./operations/deployment-runbook.md)
4. **Understanding the architecture?** Read the [Architecture Overview](./ARCHITECTURE.md)

## 📖 Documentation Standards

- All new features should include documentation updates
- Use clear, concise language
- Include code examples where appropriate
- Link to related documentation
- Keep documentation up-to-date with code changes

## 🤝 Contributing

When contributing to documentation:
1. Follow the existing structure
2. Update the main README if adding new sections
3. Use consistent formatting and style
4. Include appropriate diagrams and examples
5. Link between related documents

## 📞 Support

For questions about Tapio documentation:
- Check existing documentation first
- Review architecture history for context
- Submit issues for documentation improvements
- Contribute improvements via pull requests