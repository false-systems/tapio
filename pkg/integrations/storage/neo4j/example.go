package neo4j

import (
	"context"
	"os"

	"github.com/yairfalse/tapio/pkg/intelligence/graph"
	"go.uber.org/zap"
)

// Example shows how to use Neo4j storage with the correlation service
func Example() {
	logger, _ := zap.NewProduction()

	// Configure Neo4j connection
	config := graph.Config{
		URI:      os.Getenv("NEO4J_URI"),      // e.g., "bolt://neo4j:7687"
		Username: os.Getenv("NEO4J_USERNAME"), // e.g., "neo4j"
		Password: os.Getenv("NEO4J_PASSWORD"), // e.g., "password"
		Database: os.Getenv("NEO4J_DATABASE"), // e.g., "neo4j"
	}

	// Create Neo4j storage
	storage, err := NewStorage(config, logger)
	if err != nil {
		logger.Fatal("Failed to create Neo4j storage", zap.Error(err))
	}
	defer storage.Close(context.Background())

	// Now use this storage with the correlation engine
	// engineConfig := correlation.DefaultEngineConfig()
	// Pass storage instead of memory storage
	// engine, err := correlation.NewEngine(logger, engineConfig, clientset, storage)
}

// ExampleCorrelationService shows how to modify the correlation service to use Neo4j
var ExampleCorrelationService = `
// In cmd/correlation-service/main.go, replace:

// 1. Create storage
storageConfig := storage.DefaultMemoryStorageConfig()
memStorage := storage.NewMemoryStorage(logger, storageConfig)

// With:

// 1. Create Neo4j storage
neo4jConfig := graph.Config{
	URI:      os.Getenv("NEO4J_URI"),
	Username: os.Getenv("NEO4J_USERNAME"),
	Password: os.Getenv("NEO4J_PASSWORD"),
	Database: os.Getenv("NEO4J_DATABASE"),
}

neo4jStorage, err := neo4j.NewStorage(neo4jConfig, logger)
if err != nil {
	logger.Fatal("Failed to create Neo4j storage", zap.Error(err))
}
defer neo4jStorage.Close(context.Background())

// 2. Create correlation engine with Neo4j storage
engine, err := correlation.NewEngine(logger, engineConfig, clientset, neo4jStorage)
`

// ExampleKubernetesDeployment shows how to configure Neo4j storage in K8s
var ExampleKubernetesDeployment = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: correlation-service-config
  namespace: tapio-system
data:
  NEO4J_DATABASE: "neo4j"
---
apiVersion: v1
kind: Secret
metadata:
  name: neo4j-credentials
  namespace: tapio-system
type: Opaque
data:
  username: bmVvNGo=     # base64 encoded "neo4j"
  password: cGFzc3dvcmQ= # base64 encoded "password"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: correlation-service
  namespace: tapio-system
spec:
  template:
    spec:
      containers:
      - name: correlation-service
        env:
        - name: NEO4J_URI
          value: "bolt://neo4j.tapio-system:7687"
        - name: NEO4J_DATABASE
          valueFrom:
            configMapKeyRef:
              name: correlation-service-config
              key: NEO4J_DATABASE
        - name: NEO4J_USERNAME
          valueFrom:
            secretKeyRef:
              name: neo4j-credentials
              key: username
        - name: NEO4J_PASSWORD
          valueFrom:
            secretKeyRef:
              name: neo4j-credentials
              key: password
`
