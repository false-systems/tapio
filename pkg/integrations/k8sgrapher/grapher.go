package k8sgrapher

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/yairfalse/tapio/pkg/integrations/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// K8sGrapher builds and maintains the Kubernetes relationship graph
type K8sGrapher struct {
	kubeClient      kubernetes.Interface
	neo4jDriver     neo4j.DriverWithContext
	logger          *zap.Logger
	instrumentation *telemetry.K8sGrapherInstrumentation

	// Informers for watching K8s resources
	informers map[string]cache.SharedIndexInformer
	stopCh    chan struct{}
	wg        sync.WaitGroup
	mu        sync.RWMutex

	// Configuration
	namespace       string // empty means all namespaces
	resyncPeriod    time.Duration
	graphUpdateChan chan graphUpdate
}

// Config holds K8sGrapher configuration
type Config struct {
	KubeClient      kubernetes.Interface
	Neo4jDriver     neo4j.DriverWithContext
	Logger          *zap.Logger
	Instrumentation *telemetry.K8sGrapherInstrumentation
	Namespace       string        // Watch specific namespace or "" for all
	ResyncPeriod    time.Duration // How often to resync with K8s API
}

// graphUpdate represents a change to be applied to the graph
type graphUpdate struct {
	operation string // create, update, delete
	nodeType  string // Service, Pod, ConfigMap, etc
	data      interface{}
}

// NewK8sGrapher creates a new Kubernetes relationship grapher
func NewK8sGrapher(config Config) (*K8sGrapher, error) {
	if config.KubeClient == nil {
		return nil, fmt.Errorf("kubeClient is required")
	}
	if config.Neo4jDriver == nil {
		return nil, fmt.Errorf("neo4jDriver is required")
	}
	if config.Logger == nil {
		config.Logger = zap.NewNop()
	}
	if config.Instrumentation == nil {
		return nil, fmt.Errorf("instrumentation is required")
	}
	if config.ResyncPeriod == 0 {
		config.ResyncPeriod = 30 * time.Minute
	}

	return &K8sGrapher{
		kubeClient:      config.KubeClient,
		neo4jDriver:     config.Neo4jDriver,
		logger:          config.Logger,
		instrumentation: config.Instrumentation,
		namespace:       config.Namespace,
		resyncPeriod:    config.ResyncPeriod,
		informers:       make(map[string]cache.SharedIndexInformer),
		stopCh:          make(chan struct{}),
		graphUpdateChan: make(chan graphUpdate, 1000),
	}, nil
}

// Start begins watching K8s resources and building the graph
func (g *K8sGrapher) Start(ctx context.Context) error {
	g.logger.Info("Starting K8sGrapher")

	// Initialize Neo4j schema
	if err := g.initializeSchema(ctx); err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Start informers for each resource type
	g.startServiceInformer()
	g.startPodInformer()
	g.startConfigMapInformer()
	g.startSecretInformer()
	g.startDeploymentInformer()
	g.startReplicaSetInformer()
	g.startPVCInformer()

	// Start graph update processor
	g.wg.Add(1)
	go g.processGraphUpdates(ctx)

	// Start all informers
	for name, informer := range g.informers {
		g.logger.Info("Starting informer", zap.String("resource", name))
		go informer.Run(g.stopCh)
	}

	// Wait for initial sync
	g.logger.Info("Waiting for cache sync")
	for name, informer := range g.informers {
		if !cache.WaitForCacheSync(g.stopCh, informer.HasSynced) {
			return fmt.Errorf("failed to sync cache for %s", name)
		}
	}

	g.logger.Info("K8sGrapher started successfully")
	return nil
}

// Stop gracefully shuts down the grapher
func (g *K8sGrapher) Stop() {
	g.logger.Info("Stopping K8sGrapher")
	close(g.stopCh)
	close(g.graphUpdateChan)
	g.wg.Wait()
	g.logger.Info("K8sGrapher stopped")
}

// initializeSchema creates indexes and constraints in Neo4j
func (g *K8sGrapher) initializeSchema(ctx context.Context) error {
	ctx, span := g.instrumentation.StartSpan(ctx, "initialize_schema")
	defer func() {
		g.instrumentation.EndSpan(span, time.Now(), nil, "initialize_schema")
	}()

	session := g.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	// Create constraints and indexes
	queries := []string{
		// Unique constraints
		"CREATE CONSTRAINT IF NOT EXISTS FOR (s:Service) REQUIRE (s.namespace, s.name) IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (p:Pod) REQUIRE (p.namespace, p.name) IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (cm:ConfigMap) REQUIRE (cm.namespace, cm.name) IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (sec:Secret) REQUIRE (sec.namespace, sec.name) IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (d:Deployment) REQUIRE (d.namespace, d.name) IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (rs:ReplicaSet) REQUIRE (rs.namespace, rs.name) IS UNIQUE",
		"CREATE CONSTRAINT IF NOT EXISTS FOR (pvc:PVC) REQUIRE (pvc.namespace, pvc.name) IS UNIQUE",

		// Indexes for performance
		"CREATE INDEX IF NOT EXISTS FOR (p:Pod) ON (p.labels)",
		"CREATE INDEX IF NOT EXISTS FOR (s:Service) ON (s.selector)",
		"CREATE INDEX IF NOT EXISTS FOR ()-[r:SELECTS]-() ON (r.matched)",
	}

	for _, query := range queries {
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			_, err := tx.Run(ctx, query, nil)
			return nil, err
		})
		if err != nil {
			g.logger.Warn("Failed to create constraint/index",
				zap.String("query", query),
				zap.Error(err))
			// Continue - some constraints might already exist
		}
	}

	return nil
}

// processGraphUpdates processes updates from the queue
func (g *K8sGrapher) processGraphUpdates(ctx context.Context) {
	defer g.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-g.graphUpdateChan:
			if !ok {
				return
			}

			start := time.Now()
			err := g.applyGraphUpdate(ctx, update)
			duration := time.Since(start).Seconds()

			g.instrumentation.GraphUpdateDuration.Record(ctx, duration,
				metric.WithAttributes(
					attribute.String("operation", update.operation),
					attribute.String("node_type", update.nodeType),
					attribute.Bool("success", err == nil),
				))

			if err != nil {
				g.logger.Error("Failed to apply graph update",
					zap.String("operation", update.operation),
					zap.String("node_type", update.nodeType),
					zap.Error(err))
			}
		}
	}
}

// applyGraphUpdate applies a single update to the graph
func (g *K8sGrapher) applyGraphUpdate(ctx context.Context, update graphUpdate) error {
	ctx, span := g.instrumentation.StartSpan(ctx, "apply_graph_update",
		trace.WithAttributes(
			attribute.String("operation", update.operation),
			attribute.String("node_type", update.nodeType),
		))
	defer func() {
		g.instrumentation.EndSpan(span, time.Now(), nil, "apply_graph_update")
	}()

	switch update.nodeType {
	case "Service":
		return g.updateServiceNode(ctx, update)
	case "Pod":
		return g.updatePodNode(ctx, update)
	case "ConfigMap":
		return g.updateConfigMapNode(ctx, update)
	case "Secret":
		return g.updateSecretNode(ctx, update)
	case "Deployment":
		return g.updateDeploymentNode(ctx, update)
	case "ReplicaSet":
		return g.updateReplicaSetNode(ctx, update)
	case "PVC":
		return g.updatePVCNode(ctx, update)
	default:
		return fmt.Errorf("unknown node type: %s", update.nodeType)
	}
}
