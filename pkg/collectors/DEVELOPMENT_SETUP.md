# Tapio Development Setup

## TL;DR
Tapio is a Linux-only eBPF platform. Period. We develop on Mac but test on Linux.

## Development Environments

### 1. Mac (Quick Iteration)
```bash
# Set mock mode for local development
export TAPIO_MOCK_MODE=true
go run ./cmd/collectors -config config.yaml
```

### 2. Colima VM (Real eBPF Testing)
```bash
# Start Colima with mounted repo
colima start --cpu 4 --memory 8 --mount $HOME/projects/tapio:w

# SSH into VM
colima ssh

# Inside VM - run with real eBPF
cd /Users/yair/projects/tapio
sudo go run ./cmd/collectors -config config.yaml
```

### 3. Ubuntu Machine (Production Testing)
```bash
# Sync code
rsync -av . ubuntu-box:~/tapio/

# Run on Ubuntu
ssh ubuntu-box
cd ~/tapio
sudo go run ./cmd/collectors -config config.yaml
```

## Code Structure

### No More Stubs!
We're Linux-only. Each collector has:
- `collector.go` - Main implementation with `//go:build linux`
- `init.go` - Registration
- `config.go` - Configuration
- NO STUB FILES!

### Mock Mode for Mac Development
```go
func (c *Collector) Start(ctx context.Context) error {
    if os.Getenv("TAPIO_MOCK_MODE") == "true" {
        c.logger.Info("Running in mock mode")
        go c.generateMockEvents()
        return nil
    }
    
    // Real eBPF code here
    return c.startEBPF()
}
```

## Why This Works

1. **Honest**: We're a Linux platform, code shows it
2. **Simple**: No stub files, no confusion  
3. **Practical**: Mock mode for quick Mac testing
4. **Real Testing**: Colima for actual eBPF validation

## Typical Development Flow

1. Write code on Mac
2. Test compilation: `go build ./...` (will fail without mock mode)
3. Test with mocks: `TAPIO_MOCK_MODE=true go run ./...`
4. Test real eBPF: `colima ssh` then run
5. Push to Ubuntu for production testing

## Setting Up Colima for eBPF

```bash
# Install Colima
brew install colima

# Start with proper settings for eBPF
colima start \
  --cpu 4 \
  --memory 8 \
  --mount $HOME/projects/tapio:w \
  --runtime docker \
  --vm-type vz

# Install dependencies in VM
colima ssh
sudo apt-get update
sudo apt-get install -y linux-headers-$(uname -r) build-essential golang-go
```

## No More Platform Confusion!

- Production: Linux with eBPF
- Development: Mac with mocks OR Colima with real eBPF  
- CI/CD: Linux only
- Documentation: "Linux-only platform"

Simple. Honest. Clean.