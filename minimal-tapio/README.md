# Tapio Minimal - REST API Implementation

A minimal, working implementation of Tapio with REST API support.

## Features

- ✅ REST API Server
- ✅ Command-line Client
- ✅ Event Management
- ✅ Basic Correlation
- ✅ Health Monitoring
- ✅ Simple In-Memory Storage

## Quick Start

### 1. Install Dependencies

```bash
go mod download
```

### 2. Build

```bash
# Build server
go build -o tapio-server ./cmd/tapio-server

# Build CLI
go build -o tapio ./cmd/tapio
```

### 3. Run Server

```bash
./tapio-server --port 8080
```

### 4. Use CLI

```bash
# Check server health
./tapio health

# Submit an event
./tapio event submit --message "System started" --severity info

# List events
./tapio event list --limit 20

# Get findings
./tapio findings

# Check status
./tapio status
```

## API Endpoints

- `GET /health` - Health check
- `GET /api/v1/status` - Server status
- `POST /api/v1/events` - Submit event
- `GET /api/v1/events` - List events
- `GET /api/v1/findings` - Get findings
- `POST /api/v1/correlate` - Correlate events

## Architecture

```
minimal-tapio/
├── cmd/
│   ├── tapio/           # CLI binary
│   └── tapio-server/    # Server binary
├── pkg/
│   ├── domain/          # Core types
│   ├── server/          # Server implementation
│   │   └── api/         # REST API
│   └── client/          # REST client
└── internal/
    └── cli/             # CLI commands
```

## Example Usage

### Submit Events and Check Findings

```bash
# Start server
./tapio-server &

# Submit some error events
./tapio event submit --message "Database connection failed" --severity error
./tapio event submit --message "API timeout" --severity error
./tapio event submit --message "Memory usage high" --severity warning

# Check for findings (correlation detects error patterns)
./tapio findings
```

## Configuration

Environment variables:
- `TAPIO_SERVER` - Server URL (default: http://localhost:8080)

Command line flags:
- `--server` - Override server URL
- `--port` - Server port (default: 8080)

## Next Steps

This minimal implementation can be extended with:
- Persistent storage (PostgreSQL, etc.)
- Advanced correlation algorithms
- Authentication/Authorization
- Metrics and monitoring
- WebSocket support for real-time updates
- Kubernetes integration