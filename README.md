# 🌲 Tapio - The Forest Guardian for Kubernetes

Named after the Finnish god of forests, Tapio protects your digital forest by making complex Kubernetes cluster debugging simple and human-readable.

## ✨ Features

- **Beautiful Human Output**: Emojis, colors, and clear explanations
- **Predictive Analysis**: AI-powered failure prediction with confidence scores
- **Quick Fixes**: Actionable commands for immediate problem resolution
- **Multiple Output Formats**: Human, JSON, and YAML output
- **Smart Filtering**: Check specific apps, pods, or entire clusters
- **Zero Configuration**: Auto-detects your Kubernetes setup

## 🚀 Quick Start

### Installation

```bash
# Build from source
make build

# Install globally
make install
```

### Basic Usage

```bash
# Check current namespace
tapio check

# Check specific app
tapio check my-app

# Check specific pod
tapio check pod/my-app-7d4b9c8f-h2x9m

# Check entire cluster
tapio check --all

# Get JSON output
tapio check --output json
```

## 📊 Sample Output

```bash
$ tapio check
✅ 3 pods healthy
⚠️  1 pod has warnings

⚠️  pod/api-service-xyz: High restart count
   Container api has restarted 5 times
   🔮 Will fail in 15m (80% confidence)
   📋 Reason: Frequent restarts indicate unstable container

🔧 Quick fixes available:
  ⚡ kubectl logs api-service-xyz --previous
     Check logs for error patterns
  🚨 kubectl describe pod api-service-xyz
     Get detailed pod information
```

## 🏗️ Architecture

```
tapio/
├── cmd/tapio/              # Main CLI entry point
├── internal/
│   ├── cli/               # Cobra CLI commands
│   └── output/            # Output formatters
├── pkg/
│   ├── simple/            # Core health checker
│   └── types/             # Shared type definitions
└── test/                  # Unit and integration tests
```

## 🧪 Development

```bash
# Set up development environment
make dev-setup

# Build and test
make dev

# Run tests
make test

# Run linter
make lint

# Build for all platforms
make build-all
```

## 🎯 Roadmap

- [ ] eBPF integration for kernel-level insights
- [ ] Custom health check rules
- [ ] Slack/Discord notifications
- [ ] Prometheus metrics export
- [ ] Auto-healing capabilities
- [ ] Multi-cluster support

## 🤝 Contributing

Contributions welcome! This project follows the [Conventional Commits](https://www.conventionalcommits.org/) specification.

## 📜 License

MIT License - see [LICENSE](LICENSE) for details.

---

**🌲 Keep your Kubernetes forest healthy with Tapio!**
