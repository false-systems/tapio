package neo4j

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// EventProcessor transforms UnifiedEvents into Neo4j graph structure
type EventProcessor struct {
	client *Client
	logger *zap.Logger
}

// NewEventProcessor creates a new event processor
func NewEventProcessor(client *Client, logger *zap.Logger) (*EventProcessor, error) {
	if client == nil {
		return nil, fmt.Errorf("neo4j client is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	return &EventProcessor{
		client: client,
		logger: logger,
	}, nil
}

// ProcessEvent transforms a UnifiedEvent into graph nodes and relationships
func (p *EventProcessor) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	start := time.Now()

	p.logger.Debug("Processing event",
		zap.String("event_id", event.ID),
		zap.String("event_type", string(event.Type)),
		zap.String("source", event.Source))

	// Execute in a write transaction
	_, err := p.client.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		// 1. Create or update resource nodes based on event type
		if err := p.processResourceNodes(ctx, tx, event); err != nil {
			return nil, fmt.Errorf("failed to process resource nodes: %w", err)
		}

		// 2. Create event node
		if err := p.createEventNode(ctx, tx, event); err != nil {
			return nil, fmt.Errorf("failed to create event node: %w", err)
		}

		// 3. Create relationships
		if err := p.createEventRelationships(ctx, tx, event); err != nil {
			return nil, fmt.Errorf("failed to create relationships: %w", err)
		}

		// 4. Update resource state based on event
		if err := p.updateResourceState(ctx, tx, event); err != nil {
			return nil, fmt.Errorf("failed to update resource state: %w", err)
		}

		// 5. Detect and create causality relationships
		if err := p.detectCausality(ctx, tx, event); err != nil {
			return nil, fmt.Errorf("failed to detect causality: %w", err)
		}

		return nil, nil
	})

	if err != nil {
		p.logger.Error("Failed to process event",
			zap.String("event_id", event.ID),
			zap.Error(err))
		return err
	}

	p.logger.Info("Event processed successfully",
		zap.String("event_id", event.ID),
		zap.Duration("duration", time.Since(start)))

	return nil
}

// processResourceNodes creates or updates K8s resource nodes
func (p *EventProcessor) processResourceNodes(ctx context.Context, tx neo4j.ManagedTransaction, event *domain.UnifiedEvent) error {
	if event.K8sContext == nil {
		return nil // No K8s context to process
	}

	k8sCtx := event.K8sContext

	// Create/update main resource node based on Kind
	switch k8sCtx.Kind {
	case "Pod":
		return p.upsertPodNode(ctx, tx, k8sCtx, event)
	case "Service":
		return p.upsertServiceNode(ctx, tx, k8sCtx, event)
	case "Deployment", "StatefulSet", "DaemonSet":
		return p.upsertWorkloadNode(ctx, tx, k8sCtx, event)
	case "ConfigMap", "Secret":
		return p.upsertConfigNode(ctx, tx, k8sCtx, event)
	case "PersistentVolumeClaim":
		return p.upsertPVCNode(ctx, tx, k8sCtx, event)
	case "Node":
		return p.upsertNodeNode(ctx, tx, k8sCtx, event)
	default:
		// Generic resource node
		return p.upsertGenericResource(ctx, tx, k8sCtx, event)
	}
}

// upsertPodNode creates or updates a Pod node
func (p *EventProcessor) upsertPodNode(ctx context.Context, tx neo4j.ManagedTransaction, k8sCtx *domain.K8sContext, event *domain.UnifiedEvent) error {
	query := `
		MERGE (p:Pod {uid: $uid})
		SET p.name = $name,
		    p.namespace = $namespace,
		    p.cluster = $cluster,
		    p.node = $node,
		    p.updated_at = datetime($timestamp),
		    p.labels = $labels
		RETURN p
	`

	params := map[string]interface{}{
		"uid":       k8sCtx.UID,
		"name":      k8sCtx.Name,
		"namespace": k8sCtx.Namespace,
		"cluster":   k8sCtx.ClusterName,
		"node":      k8sCtx.NodeName,
		"timestamp": event.Timestamp.Format(time.RFC3339),
		"labels":    k8sCtx.Labels,
	}

	// Update pod-specific fields based on event type
	if event.Type == "pod_restart" || event.Type == "container_restart" {
		query = `
			MERGE (p:Pod {uid: $uid})
			SET p.name = $name,
			    p.namespace = $namespace,
			    p.cluster = $cluster,
			    p.node = $node,
			    p.updated_at = datetime($timestamp),
			    p.labels = $labels,
			    p.restart_count = COALESCE(p.restart_count, 0) + 1,
			    p.last_restart = datetime($timestamp)
			RETURN p
		`
	} else if event.Type == "pod_failed" {
		query = `
			MERGE (p:Pod {uid: $uid})
			SET p.name = $name,
			    p.namespace = $namespace,
			    p.cluster = $cluster,
			    p.node = $node,
			    p.updated_at = datetime($timestamp),
			    p.labels = $labels,
			    p.phase = 'Failed',
			    p.ready = false
			RETURN p
		`
	}

	_, err := tx.Run(ctx, query, params)
	return err
}

// upsertServiceNode creates or updates a Service node
func (p *EventProcessor) upsertServiceNode(ctx context.Context, tx neo4j.ManagedTransaction, k8sCtx *domain.K8sContext, event *domain.UnifiedEvent) error {
	query := `
		MERGE (s:Service {uid: $uid})
		SET s.name = $name,
		    s.namespace = $namespace,
		    s.cluster = $cluster,
		    s.updated_at = datetime($timestamp),
		    s.labels = $labels,
		    s.selector = $selector
		RETURN s
	`

	params := map[string]interface{}{
		"uid":       k8sCtx.UID,
		"name":      k8sCtx.Name,
		"namespace": k8sCtx.Namespace,
		"cluster":   k8sCtx.ClusterName,
		"timestamp": event.Timestamp.Format(time.RFC3339),
		"labels":    k8sCtx.Labels,
		"selector":  k8sCtx.Selectors,
	}

	_, err := tx.Run(ctx, query, params)
	return err
}

// upsertWorkloadNode creates or updates a workload controller node
func (p *EventProcessor) upsertWorkloadNode(ctx context.Context, tx neo4j.ManagedTransaction, k8sCtx *domain.K8sContext, event *domain.UnifiedEvent) error {
	query := fmt.Sprintf(`
		MERGE (w:%s {uid: $uid})
		SET w.name = $name,
		    w.namespace = $namespace,
		    w.cluster = $cluster,
		    w.updated_at = datetime($timestamp),
		    w.labels = $labels
		RETURN w
	`, k8sCtx.Kind)

	params := map[string]interface{}{
		"uid":       k8sCtx.UID,
		"name":      k8sCtx.Name,
		"namespace": k8sCtx.Namespace,
		"cluster":   k8sCtx.ClusterName,
		"timestamp": event.Timestamp.Format(time.RFC3339),
		"labels":    k8sCtx.Labels,
	}

	_, err := tx.Run(ctx, query, params)
	return err
}

// upsertConfigNode creates or updates a ConfigMap/Secret node
func (p *EventProcessor) upsertConfigNode(ctx context.Context, tx neo4j.ManagedTransaction, k8sCtx *domain.K8sContext, event *domain.UnifiedEvent) error {
	nodeType := k8sCtx.Kind
	query := fmt.Sprintf(`
		MERGE (c:%s {uid: $uid})
		SET c.name = $name,
		    c.namespace = $namespace,
		    c.cluster = $cluster,
		    c.updated_at = datetime($timestamp),
		    c.labels = $labels
		RETURN c
	`, nodeType)

	// If this is a config change event, mark it as modified
	if event.Type == "config_changed" || event.Type == "secret_changed" {
		query = fmt.Sprintf(`
			MERGE (c:%s {uid: $uid})
			SET c.name = $name,
			    c.namespace = $namespace,
			    c.cluster = $cluster,
			    c.updated_at = datetime($timestamp),
			    c.last_modified = datetime($timestamp),
			    c.labels = $labels,
			    c.version = COALESCE(c.version, 0) + 1
			RETURN c
		`, nodeType)
	}

	params := map[string]interface{}{
		"uid":       k8sCtx.UID,
		"name":      k8sCtx.Name,
		"namespace": k8sCtx.Namespace,
		"cluster":   k8sCtx.ClusterName,
		"timestamp": event.Timestamp.Format(time.RFC3339),
		"labels":    k8sCtx.Labels,
	}

	_, err := tx.Run(ctx, query, params)
	return err
}

// upsertPVCNode creates or updates a PVC node
func (p *EventProcessor) upsertPVCNode(ctx context.Context, tx neo4j.ManagedTransaction, k8sCtx *domain.K8sContext, event *domain.UnifiedEvent) error {
	query := `
		MERGE (pvc:PVC {uid: $uid})
		SET pvc.name = $name,
		    pvc.namespace = $namespace,
		    pvc.cluster = $cluster,
		    pvc.updated_at = datetime($timestamp),
		    pvc.labels = $labels
		RETURN pvc
	`

	params := map[string]interface{}{
		"uid":       k8sCtx.UID,
		"name":      k8sCtx.Name,
		"namespace": k8sCtx.Namespace,
		"cluster":   k8sCtx.ClusterName,
		"timestamp": event.Timestamp.Format(time.RFC3339),
		"labels":    k8sCtx.Labels,
	}

	_, err := tx.Run(ctx, query, params)
	return err
}

// upsertNodeNode creates or updates a K8s Node node
func (p *EventProcessor) upsertNodeNode(ctx context.Context, tx neo4j.ManagedTransaction, k8sCtx *domain.K8sContext, event *domain.UnifiedEvent) error {
	query := `
		MERGE (n:Node {name: $name, cluster: $cluster})
		SET n.updated_at = datetime($timestamp),
		    n.labels = $labels,
		    n.zone = $zone,
		    n.region = $region
		RETURN n
	`

	params := map[string]interface{}{
		"name":      k8sCtx.Name,
		"cluster":   k8sCtx.ClusterName,
		"timestamp": event.Timestamp.Format(time.RFC3339),
		"labels":    k8sCtx.Labels,
		"zone":      k8sCtx.Zone,
		"region":    k8sCtx.Region,
	}

	_, err := tx.Run(ctx, query, params)
	return err
}

// upsertGenericResource creates or updates a generic resource node
func (p *EventProcessor) upsertGenericResource(ctx context.Context, tx neo4j.ManagedTransaction, k8sCtx *domain.K8sContext, event *domain.UnifiedEvent) error {
	query := `
		MERGE (r:Resource {uid: $uid})
		SET r.name = $name,
		    r.namespace = $namespace,
		    r.cluster = $cluster,
		    r.kind = $kind,
		    r.api_version = $api_version,
		    r.updated_at = datetime($timestamp),
		    r.labels = $labels
		RETURN r
	`

	params := map[string]interface{}{
		"uid":         k8sCtx.UID,
		"name":        k8sCtx.Name,
		"namespace":   k8sCtx.Namespace,
		"cluster":     k8sCtx.ClusterName,
		"kind":        k8sCtx.Kind,
		"api_version": k8sCtx.APIVersion,
		"timestamp":   event.Timestamp.Format(time.RFC3339),
		"labels":      k8sCtx.Labels,
	}

	_, err := tx.Run(ctx, query, params)
	return err
}

// createEventNode creates an event node in the graph
func (p *EventProcessor) createEventNode(ctx context.Context, tx neo4j.ManagedTransaction, event *domain.UnifiedEvent) error {
	// Serialize event attributes as JSON
	dataJSON, err := json.Marshal(event.Attributes)
	if err != nil {
		dataJSON = []byte("{}")
	}

	query := `
		CREATE (e:Event {
			id: $id,
			type: $type,
			severity: $severity,
			message: $message,
			timestamp: datetime($timestamp),
			source: $source,
			data: $data
		})
		RETURN e
	`

	params := map[string]interface{}{
		"id":        event.ID,
		"type":      string(event.Type),
		"severity":  string(event.Severity),
		"message":   event.Message,
		"timestamp": event.Timestamp.Format(time.RFC3339),
		"source":    event.Source,
		"data":      string(dataJSON),
	}

	_, err = tx.Run(ctx, query, params)
	return err
}

// createEventRelationships creates relationships between event and resources
func (p *EventProcessor) createEventRelationships(ctx context.Context, tx neo4j.ManagedTransaction, event *domain.UnifiedEvent) error {
	if event.K8sContext == nil {
		return nil
	}

	// Create AFFECTS relationship from event to main resource
	query := `
		MATCH (e:Event {id: $event_id})
		MATCH (r {uid: $resource_uid})
		CREATE (e)-[:AFFECTS {timestamp: datetime($timestamp)}]->(r)
	`

	params := map[string]interface{}{
		"event_id":     event.ID,
		"resource_uid": event.K8sContext.UID,
		"timestamp":    event.Timestamp.Format(time.RFC3339),
	}

	_, err := tx.Run(ctx, query, params)
	if err != nil {
		// Try without UID if that fails (for resources like Node that use name)
		if event.K8sContext.Kind == "Node" {
			query = `
				MATCH (e:Event {id: $event_id})
				MATCH (n:Node {name: $name, cluster: $cluster})
				CREATE (e)-[:AFFECTS {timestamp: datetime($timestamp)}]->(n)
			`
			params = map[string]interface{}{
				"event_id":  event.ID,
				"name":      event.K8sContext.Name,
				"cluster":   event.K8sContext.ClusterName,
				"timestamp": event.Timestamp.Format(time.RFC3339),
			}
			_, err = tx.Run(ctx, query, params)
		}
	}

	return err
}

// updateResourceState updates resource state based on event
func (p *EventProcessor) updateResourceState(ctx context.Context, tx neo4j.ManagedTransaction, event *domain.UnifiedEvent) error {
	// State updates are already handled in upsert methods
	// This is a placeholder for more complex state tracking
	return nil
}

// detectCausality looks for causal relationships between events
func (p *EventProcessor) detectCausality(ctx context.Context, tx neo4j.ManagedTransaction, event *domain.UnifiedEvent) error {
	// Look for recent related events that might have caused this one
	switch event.Type {
	case "pod_restart", "pod_failed":
		return p.detectPodCausality(ctx, tx, event)
	case "service_unavailable":
		return p.detectServiceCausality(ctx, tx, event)
	}
	return nil
}

// detectPodCausality finds events that might have caused pod issues
func (p *EventProcessor) detectPodCausality(ctx context.Context, tx neo4j.ManagedTransaction, event *domain.UnifiedEvent) error {
	if event.K8sContext == nil {
		return nil
	}

	// Look for config changes in the last 10 minutes that might have caused the restart
	query := `
		MATCH (pod:Pod {uid: $pod_uid})
		MATCH (pod)-[:MOUNTS|USES_SECRET]->(config)
		WHERE config:ConfigMap OR config:Secret
		MATCH (config)<-[:AFFECTS]-(config_event:Event)
		WHERE config_event.timestamp > datetime($timestamp) - duration({minutes: 10})
		  AND config_event.timestamp < datetime($timestamp)
		  AND config_event.type IN ['config_changed', 'secret_changed']
		MATCH (current_event:Event {id: $event_id})
		CREATE (config_event)-[:TRIGGERED {
			delay: duration.between(config_event.timestamp, current_event.timestamp),
			confidence: 0.8
		}]->(current_event)
	`

	params := map[string]interface{}{
		"pod_uid":   event.K8sContext.UID,
		"event_id":  event.ID,
		"timestamp": event.Timestamp.Format(time.RFC3339),
	}

	_, err := tx.Run(ctx, query, params)
	return err
}

// detectServiceCausality finds events that might have caused service issues
func (p *EventProcessor) detectServiceCausality(ctx context.Context, tx neo4j.ManagedTransaction, event *domain.UnifiedEvent) error {
	if event.K8sContext == nil {
		return nil
	}

	// Look for pod failures that might have caused service unavailability
	query := `
		MATCH (svc:Service {uid: $svc_uid})
		MATCH (svc)-[:SELECTS]->(pod:Pod)
		MATCH (pod)<-[:AFFECTS]-(pod_event:Event)
		WHERE pod_event.timestamp > datetime($timestamp) - duration({minutes: 5})
		  AND pod_event.timestamp < datetime($timestamp)
		  AND pod_event.type IN ['pod_failed', 'pod_crash']
		MATCH (current_event:Event {id: $event_id})
		CREATE (pod_event)-[:TRIGGERED {
			delay: duration.between(pod_event.timestamp, current_event.timestamp),
			confidence: 0.7
		}]->(current_event)
	`

	params := map[string]interface{}{
		"svc_uid":   event.K8sContext.UID,
		"event_id":  event.ID,
		"timestamp": event.Timestamp.Format(time.RFC3339),
	}

	_, err := tx.Run(ctx, query, params)
	return err
}
