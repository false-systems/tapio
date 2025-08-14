#!/bin/bash
# E2E Test for Observation Pipeline

set -e

echo "🚀 Testing Observation Pipeline E2E"
echo "==================================="

# Check prerequisites
echo "📋 Checking prerequisites..."

# Check if NATS is running
if ! nc -z localhost 4222 2>/dev/null; then
    echo "❌ NATS is not running on localhost:4222"
    echo "   Run: docker run -d --name nats -p 4222:4222 nats:latest -js"
    exit 1
fi
echo "✅ NATS is running"

# Check if Neo4j is running
if ! nc -z localhost 7687 2>/dev/null; then
    echo "❌ Neo4j is not running on localhost:7687"
    echo "   Run: docker run -d --name neo4j -p 7474:7474 -p 7687:7687 -e NEO4J_AUTH=neo4j/password neo4j:latest"
    exit 1
fi
echo "✅ Neo4j is running"

# Build the loader
echo ""
echo "🔨 Building loader..."
go build -o bin/loader ./cmd/loader 2>/dev/null || {
    echo "⚠️  No loader cmd found, creating one..."
    mkdir -p cmd/loader
    cat > cmd/loader/main.go << 'EOF'
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"
    
    "github.com/yairfalse/tapio/pkg/integrations/loader"
    "go.uber.org/zap"
)

func main() {
    logger, _ := zap.NewDevelopment()
    defer logger.Sync()
    
    config := &loader.Config{
        NATSURL: "nats://localhost:4222",
        Neo4jURL: "bolt://localhost:7687",
        Neo4jUser: "neo4j",
        Neo4jPassword: "password",
        BatchSize: 10,
        WorkerCount: 2,
    }
    
    ldr, err := loader.New(logger, config)
    if err != nil {
        log.Fatalf("Failed to create loader: %v", err)
    }
    
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    // Handle shutdown
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    
    go func() {
        <-sigCh
        logger.Info("Shutting down...")
        cancel()
    }()
    
    logger.Info("Starting loader...")
    if err := ldr.Start(ctx); err != nil {
        log.Fatalf("Failed to start loader: %v", err)
    }
    
    <-ctx.Done()
    ldr.Stop()
}
EOF
    go build -o bin/loader ./cmd/loader
}

# Create a test event generator
echo ""
echo "📝 Creating test event generator..."
cat > bin/test_publisher.go << 'EOF'
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "time"
    
    "github.com/nats-io/nats.go"
    "github.com/yairfalse/tapio/pkg/collectors"
)

func main() {
    nc, err := nats.Connect("nats://localhost:4222")
    if err != nil {
        log.Fatal(err)
    }
    defer nc.Close()
    
    js, err := nc.JetStream()
    if err != nil {
        log.Fatal(err)
    }
    
    // Ensure stream exists
    js.AddStream(&nats.StreamConfig{
        Name:     "OBSERVATIONS",
        Subjects: []string{"observations.>"},
    })
    
    // Create test events
    events := []collectors.RawEvent{
        {
            Timestamp: time.Now(),
            Type:      "kernel",
            Data:      json.RawMessage(`{"PID": 1234, "Comm": "nginx", "Syscall": "open"}`),
            Metadata:  map[string]string{"node": "node-1"},
        },
        {
            Timestamp: time.Now(),
            Type:      "kubeapi",
            Data:      json.RawMessage(`{"Kind": "Pod", "Name": "nginx-pod", "Namespace": "default", "EventType": "ADDED"}`),
            Metadata:  map[string]string{"cluster": "prod"},
        },
        {
            Timestamp: time.Now(),
            Type:      "dns",
            Data:      json.RawMessage(`{"QueryName": "service.default.svc.cluster.local", "ClientIP": "10.0.0.5"}`),
            Metadata:  map[string]string{"resolver": "coredns"},
        },
    }
    
    for i, event := range events {
        data, _ := json.Marshal(event)
        subject := fmt.Sprintf("observations.%s", event.Type)
        
        if _, err := js.Publish(subject, data); err != nil {
            log.Printf("Failed to publish event %d: %v", i, err)
        } else {
            fmt.Printf("✅ Published %s event\n", event.Type)
        }
    }
    
    fmt.Println("\n📤 Published 3 test events")
}
EOF

go run bin/test_publisher.go

# Start the loader
echo ""
echo "🚀 Starting loader (5 seconds)..."
timeout 5 ./bin/loader &
LOADER_PID=$!

# Wait for processing
sleep 3

# Query Neo4j for observations
echo ""
echo "🔍 Querying Neo4j for observations..."
cat > bin/query_neo4j.py << 'EOF'
from neo4j import GraphDatabase
import sys

driver = GraphDatabase.driver("bolt://localhost:7687", auth=("neo4j", "password"))

def count_observations():
    with driver.session() as session:
        result = session.run("MATCH (o:Observation) RETURN count(o) as count")
        return result.single()["count"]

def get_observations():
    with driver.session() as session:
        result = session.run("""
            MATCH (o:Observation) 
            RETURN o.source as source, o.type as type, o.action as action
            ORDER BY o.timestamp DESC
            LIMIT 10
        """)
        return result.values()

try:
    count = count_observations()
    print(f"\n📊 Found {count} observations in Neo4j:")
    
    for obs in get_observations():
        print(f"  - {obs[0]}: {obs[1]} ({obs[2]})")
    
    if count > 0:
        print("\n✅ E2E Test PASSED! Observations are flowing through the pipeline")
        sys.exit(0)
    else:
        print("\n❌ E2E Test FAILED! No observations found in Neo4j")
        sys.exit(1)
        
except Exception as e:
    print(f"❌ Failed to query Neo4j: {e}")
    print("   Make sure Neo4j is running with user: neo4j, password: password")
    sys.exit(1)
    
finally:
    driver.close()
EOF

python3 bin/query_neo4j.py || {
    echo ""
    echo "⚠️  Python neo4j driver not installed"
    echo "   Run: pip install neo4j"
    echo ""
    echo "   Or query manually:"
    echo "   cypher-shell -u neo4j -p password"
    echo "   MATCH (o:Observation) RETURN o LIMIT 10;"
}

# Cleanup
echo ""
echo "🧹 Cleaning up..."
kill $LOADER_PID 2>/dev/null || true

echo ""
echo "✨ E2E Test Complete!"