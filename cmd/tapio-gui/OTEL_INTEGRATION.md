# Tapio GUI - OTEL Integration

## 🚀 Overview

The Tapio GUI now features **native OTEL (OpenTelemetry) visualization**, combining human-readable stories with deep technical traces in one unified interface.

## ✨ Features

### 1. **Unified Dashboard**
- **Stories Tab**: Human-readable insights about your cluster
- **Traces Tab**: Technical OTEL traces with full span hierarchy
- **Seamless Navigation**: Switch between views with one click

### 2. **OTEL Trace Visualization**
- **Trace List**: See all traces with service filtering
- **Span Timeline**: Visual representation of span durations
- **Hierarchical View**: Parent-child span relationships
- **Rich Metadata**: Tags, logs, and Tapio intelligence attributes

### 3. **Story-Trace Linking**
- **Automatic Correlation**: Stories linked to their underlying traces
- **Bidirectional Navigation**: Jump from story to trace and back
- **Context Preservation**: See both human and technical views

### 4. **Span Details**
- **Full Attributes**: All span tags and metadata
- **Tapio Intelligence**: Severity, patterns, correlations
- **Logs & Events**: Timestamped span events
- **Business Impact**: When available

## 🛠️ Setup

### Quick Start
```bash
# From the tapio-gui directory
./run-with-otel.sh
```

This script will:
1. Start Jaeger if not running
2. Build the GUI if needed
3. Launch Tapio GUI with OTEL enabled

### Manual Setup

1. **Start OTEL Backend** (Jaeger)
```bash
docker run -d \
  --name jaeger \
  -p 16686:16686 \
  -p 4317:4317 \
  jaegertracing/all-in-one:latest
```

2. **Build the GUI**
```bash
cd cmd/tapio-gui
./build.sh
```

3. **Run Tapio GUI**
```bash
./build/bin/tapio-gui
```

## 🎯 Usage

### Viewing Traces

1. Click the **"Traces"** tab in the navigation
2. Traces will load automatically from your OTEL backend
3. Use the service selector to filter by service
4. Click on any trace to expand and see spans

### Exploring Spans

1. Click on a trace to see its span hierarchy
2. Click on individual spans for detailed view
3. View attributes, logs, and Tapio metadata
4. Navigate child spans in the tree view

### Linking with Stories

1. In the Stories view, related traces are shown
2. Click "Find Traces" to discover OTEL data
3. Jump directly to trace view for technical details

## 🔧 Configuration

### Backend Connection
The GUI connects to OTEL backends on:
- **Jaeger UI**: http://localhost:16686
- **OTLP Endpoint**: localhost:4317

### Environment Variables
```bash
# Custom Jaeger endpoint
export TAPIO_OTEL_ENDPOINT=http://my-jaeger:16686

# Custom OTLP endpoint  
export TAPIO_OTLP_ENDPOINT=my-collector:4317
```

## 🏗️ Architecture

### Components

1. **Backend** (`otel.go`)
   - Fetches traces from Jaeger Query API
   - Links stories to traces
   - Provides mock data for development

2. **Frontend Components**
   - `OTELTraceView.vue` - Main trace list view
   - `OTELSpanTree.vue` - Hierarchical span display
   - `SpanNode.vue` - Individual span rendering
   - `StoryTraceLink.vue` - Story-trace correlation

3. **Data Flow**
```
Jaeger/Tempo → Backend API → Wails Bridge → Vue Components
                    ↓
              Story Correlation
```

## 🎨 UI Features

### Trace List
- Service filtering
- Duration display
- Span count badges
- Auto-refresh capability

### Span Timeline
- Visual duration bars
- Relative timing display
- Parent-child relationships
- Severity color coding

### Span Details Modal
- Full attribute display
- Tapio intelligence metadata
- Log entries with timestamps
- Navigation to related stories

## 🚧 Development

### Mock Data
When OTEL backend is unavailable, the GUI uses mock data from `frontend/src/mocks/otel.ts`.

### Adding New Features
1. Extend backend methods in `otel.go`
2. Update Vue components as needed
3. Regenerate Wails bindings: `wails generate`

### Debugging
- Check browser console for frontend errors
- Backend logs appear in terminal
- Jaeger UI: http://localhost:16686

## 🎉 What's Next

- **Real-time Updates**: WebSocket for live traces
- **Advanced Filtering**: By time, tags, patterns
- **Trace Comparison**: Side-by-side analysis
- **Export Options**: Save traces for analysis
- **Performance Metrics**: Latency distributions

---

This integration makes Tapio the **ultimate Kubernetes observability platform** - combining human intelligence with technical depth!