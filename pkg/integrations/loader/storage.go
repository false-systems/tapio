package loader

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
)

// storeObservationEvents stores a batch of observation events in Neo4j
func (l *Loader) storeObservationEvents(ctx context.Context, events []*domain.ObservationEvent) (*StorageStats, error) {
	ctx, span := l.tracer.Start(ctx, "loader.store_observation_events")
	defer span.End()

	start := time.Now()
	stats := &StorageStats{
		BatchSize: len(events),
	}

	span.SetAttributes(
		attribute.Int("batch_size", len(events)),
	)

	// Execute batch storage in a single transaction
	err := l.neo4jClient.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
		// Create observation nodes
		nodesCreated, err := l.createObservationNodes(ctx, tx, events)
		if err != nil {
			return fmt.Errorf("failed to create observation nodes: %w", err)
		}
		stats.NodesCreated = nodesCreated

		// Create resource nodes (pods, services, etc.) if they don't exist
		resourceNodesCreated, err := l.createResourceNodes(ctx, tx, events)
		if err != nil {
			return fmt.Errorf("failed to create resource nodes: %w", err)
		}
		stats.NodesCreated += resourceNodesCreated

		// Create relationships between observations and resources
		relationshipsCreated, err := l.createObservationRelationships(ctx, tx, events)
		if err != nil {
			return fmt.Errorf("failed to create observation relationships: %w", err)
		}
		stats.RelationshipsCreated = relationshipsCreated

		// Create causal relationships between observations
		causalRelationships, err := l.createCausalRelationships(ctx, tx, events)
		if err != nil {
			return fmt.Errorf("failed to create causal relationships: %w", err)
		}
		stats.RelationshipsCreated += causalRelationships

		return nil
	})

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("failed to store observation events: %w", err)
	}

	stats.StorageTime = time.Since(start)

	span.SetAttributes(
		attribute.Int64("nodes_created", stats.NodesCreated),
		attribute.Int64("relationships_created", stats.RelationshipsCreated),
		attribute.Float64("storage_time_ms", stats.StorageTime.Seconds()*1000),
	)

	l.logger.Debug("Successfully stored observation events",
		zap.Int("batch_size", len(events)),
		zap.Int64("nodes_created", stats.NodesCreated),
		zap.Int64("relationships_created", stats.RelationshipsCreated),
		zap.Duration("storage_time", stats.StorageTime))

	return stats, nil
}

// createObservationNodes creates Observation nodes in Neo4j
func (l *Loader) createObservationNodes(ctx context.Context, tx neo4j.ManagedTransaction, events []*domain.ObservationEvent) (int64, error) {
	ctx, span := l.tracer.Start(ctx, "loader.create_observation_nodes")
	defer span.End()

	if len(events) == 0 {
		return 0, nil
	}

	// Build batch query for creating observation nodes
	query := `
		UNWIND $events AS event
		CREATE (o:Observation {
			id: event.id,
			timestamp: datetime({epochMillis: event.timestamp}),
			source: event.source,
			type: event.type,
			pid: event.pid,
			container_id: event.container_id,
			pod_name: event.pod_name,
			namespace: event.namespace,
			service_name: event.service_name,
			node_name: event.node_name,
			action: event.action,
			target: event.target,
			result: event.result,
			reason: event.reason,
			duration: event.duration,
			size: event.size,
			count: event.count,
			data: event.data,
			caused_by: event.caused_by,
			parent_id: event.parent_id,
			created_at: datetime()
		})
		RETURN count(o) AS nodes_created
	`

	// Convert events to parameter format
	eventParams := make([]map[string]interface{}, len(events))
	for i, event := range events {
		eventParams[i] = l.eventToParams(event)
	}

	params := map[string]interface{}{
		"events": eventParams,
	}

	result, err := tx.Run(ctx, query, params)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return 0, fmt.Errorf("failed to execute observation node creation query: %w", err)
	}

	// Get result
	record, err := result.Single(ctx)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return 0, fmt.Errorf("failed to get result: %w", err)
	}

	nodesCreated, ok := record.Get("nodes_created")
	if !ok {
		return 0, fmt.Errorf("nodes_created not found in result")
	}

	count, ok := nodesCreated.(int64)
	if !ok {
		return 0, fmt.Errorf("nodes_created is not int64: %T", nodesCreated)
	}

	span.SetAttributes(attribute.Int64("nodes_created", count))
	return count, nil
}

// createResourceNodes creates resource nodes (Pod, Service, Node) if they don't exist
func (l *Loader) createResourceNodes(ctx context.Context, tx neo4j.ManagedTransaction, events []*domain.ObservationEvent) (int64, error) {
	ctx, span := l.tracer.Start(ctx, "loader.create_resource_nodes")
	defer span.End()

	var totalCreated int64

	// Create Pod nodes
	podNodes, err := l.createPodNodes(ctx, tx, events)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return 0, fmt.Errorf("failed to create pod nodes: %w", err)
	}
	totalCreated += podNodes

	// Create Service nodes
	serviceNodes, err := l.createServiceNodes(ctx, tx, events)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return 0, fmt.Errorf("failed to create service nodes: %w", err)
	}
	totalCreated += serviceNodes

	// Create Node nodes
	nodeNodes, err := l.createNodeNodes(ctx, tx, events)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return 0, fmt.Errorf("failed to create node nodes: %w", err)
	}
	totalCreated += nodeNodes

	span.SetAttributes(
		attribute.Int64("pod_nodes_created", podNodes),
		attribute.Int64("service_nodes_created", serviceNodes),
		attribute.Int64("node_nodes_created", nodeNodes),
		attribute.Int64("total_resource_nodes_created", totalCreated),
	)

	return totalCreated, nil
}

// createPodNodes creates Pod nodes for unique pod references
func (l *Loader) createPodNodes(ctx context.Context, tx neo4j.ManagedTransaction, events []*domain.ObservationEvent) (int64, error) {
	// Collect unique pod references
	pods := make(map[string]map[string]string) // namespace -> pod_name -> uid

	for _, event := range events {
		if event.PodName != nil && event.Namespace != nil {
			namespace := *event.Namespace
			podName := *event.PodName

			if pods[namespace] == nil {
				pods[namespace] = make(map[string]string)
			}

			// Generate a UID if we don't have one (for observations)
			uid := fmt.Sprintf("obs-pod-%s-%s", namespace, podName)
			pods[namespace][podName] = uid
		}
	}

	if len(pods) == 0 {
		return 0, nil
	}

	// Build batch query
	query := `
		UNWIND $pods AS pod
		MERGE (p:Pod {uid: pod.uid})
		ON CREATE SET 
			p.name = pod.name,
			p.namespace = pod.namespace,
			p.created_at = datetime(),
			p.source = 'observation'
		RETURN count(p) AS nodes_created
	`

	var podParams []map[string]interface{}
	for namespace, nameToUID := range pods {
		for name, uid := range nameToUID {
			podParams = append(podParams, map[string]interface{}{
				"uid":       uid,
				"name":      name,
				"namespace": namespace,
			})
		}
	}

	params := map[string]interface{}{
		"pods": podParams,
	}

	result, err := tx.Run(ctx, query, params)
	if err != nil {
		return 0, fmt.Errorf("failed to execute pod creation query: %w", err)
	}

	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get pod creation result: %w", err)
	}

	nodesCreated, _ := record.Get("nodes_created")
	if count, ok := nodesCreated.(int64); ok {
		return count, nil
	}

	return 0, nil
}

// createServiceNodes creates Service nodes for unique service references
func (l *Loader) createServiceNodes(ctx context.Context, tx neo4j.ManagedTransaction, events []*domain.ObservationEvent) (int64, error) {
	// Collect unique service references
	services := make(map[string]map[string]string) // namespace -> service_name -> uid

	for _, event := range events {
		if event.ServiceName != nil && event.Namespace != nil {
			namespace := *event.Namespace
			serviceName := *event.ServiceName

			if services[namespace] == nil {
				services[namespace] = make(map[string]string)
			}

			// Generate a UID if we don't have one
			uid := fmt.Sprintf("obs-svc-%s-%s", namespace, serviceName)
			services[namespace][serviceName] = uid
		}
	}

	if len(services) == 0 {
		return 0, nil
	}

	// Build batch query
	query := `
		UNWIND $services AS service
		MERGE (s:Service {uid: service.uid})
		ON CREATE SET 
			s.name = service.name,
			s.namespace = service.namespace,
			s.created_at = datetime(),
			s.source = 'observation'
		RETURN count(s) AS nodes_created
	`

	var serviceParams []map[string]interface{}
	for namespace, nameToUID := range services {
		for name, uid := range nameToUID {
			serviceParams = append(serviceParams, map[string]interface{}{
				"uid":       uid,
				"name":      name,
				"namespace": namespace,
			})
		}
	}

	params := map[string]interface{}{
		"services": serviceParams,
	}

	result, err := tx.Run(ctx, query, params)
	if err != nil {
		return 0, fmt.Errorf("failed to execute service creation query: %w", err)
	}

	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get service creation result: %w", err)
	}

	nodesCreated, _ := record.Get("nodes_created")
	if count, ok := nodesCreated.(int64); ok {
		return count, nil
	}

	return 0, nil
}

// createNodeNodes creates Node nodes for unique node references
func (l *Loader) createNodeNodes(ctx context.Context, tx neo4j.ManagedTransaction, events []*domain.ObservationEvent) (int64, error) {
	// Collect unique node references
	nodes := make(map[string]bool)

	for _, event := range events {
		if event.NodeName != nil {
			nodes[*event.NodeName] = true
		}
	}

	if len(nodes) == 0 {
		return 0, nil
	}

	// Build batch query
	query := `
		UNWIND $nodes AS node
		MERGE (n:Node {name: node.name})
		ON CREATE SET 
			n.created_at = datetime(),
			n.source = 'observation'
		RETURN count(n) AS nodes_created
	`

	var nodeParams []map[string]interface{}
	for nodeName := range nodes {
		nodeParams = append(nodeParams, map[string]interface{}{
			"name": nodeName,
		})
	}

	params := map[string]interface{}{
		"nodes": nodeParams,
	}

	result, err := tx.Run(ctx, query, params)
	if err != nil {
		return 0, fmt.Errorf("failed to execute node creation query: %w", err)
	}

	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get node creation result: %w", err)
	}

	nodesCreated, _ := record.Get("nodes_created")
	if count, ok := nodesCreated.(int64); ok {
		return count, nil
	}

	return 0, nil
}

// createObservationRelationships creates relationships between observations and resources
func (l *Loader) createObservationRelationships(ctx context.Context, tx neo4j.ManagedTransaction, events []*domain.ObservationEvent) (int64, error) {
	ctx, span := l.tracer.Start(ctx, "loader.create_observation_relationships")
	defer span.End()

	var totalCreated int64

	// Create BELONGS_TO relationships with Pods
	podRels, err := l.createObservationPodRelationships(ctx, tx, events)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return 0, fmt.Errorf("failed to create observation-pod relationships: %w", err)
	}
	totalCreated += podRels

	// Create AFFECTS relationships with Services
	serviceRels, err := l.createObservationServiceRelationships(ctx, tx, events)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return 0, fmt.Errorf("failed to create observation-service relationships: %w", err)
	}
	totalCreated += serviceRels

	// Create OCCURS_ON relationships with Nodes
	nodeRels, err := l.createObservationNodeRelationships(ctx, tx, events)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return 0, fmt.Errorf("failed to create observation-node relationships: %w", err)
	}
	totalCreated += nodeRels

	span.SetAttributes(
		attribute.Int64("pod_relationships_created", podRels),
		attribute.Int64("service_relationships_created", serviceRels),
		attribute.Int64("node_relationships_created", nodeRels),
		attribute.Int64("total_relationships_created", totalCreated),
	)

	return totalCreated, nil
}

// createObservationPodRelationships creates BELONGS_TO relationships between observations and pods
func (l *Loader) createObservationPodRelationships(ctx context.Context, tx neo4j.ManagedTransaction, events []*domain.ObservationEvent) (int64, error) {
	var relationships []map[string]interface{}

	for _, event := range events {
		if event.PodName != nil && event.Namespace != nil {
			uid := fmt.Sprintf("obs-pod-%s-%s", *event.Namespace, *event.PodName)
			relationships = append(relationships, map[string]interface{}{
				"observation_id": event.ID,
				"pod_uid":        uid,
				"timestamp":      event.Timestamp.UnixMilli(),
			})
		}
	}

	if len(relationships) == 0 {
		return 0, nil
	}

	query := `
		UNWIND $relationships AS rel
		MATCH (o:Observation {id: rel.observation_id})
		MATCH (p:Pod {uid: rel.pod_uid})
		CREATE (o)-[:BELONGS_TO {created_at: datetime({epochMillis: rel.timestamp})}]->(p)
		RETURN count(*) AS relationships_created
	`

	params := map[string]interface{}{
		"relationships": relationships,
	}

	result, err := tx.Run(ctx, query, params)
	if err != nil {
		return 0, fmt.Errorf("failed to execute observation-pod relationship query: %w", err)
	}

	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get observation-pod relationship result: %w", err)
	}

	relationshipsCreated, _ := record.Get("relationships_created")
	if count, ok := relationshipsCreated.(int64); ok {
		return count, nil
	}

	return 0, nil
}

// createObservationServiceRelationships creates AFFECTS relationships between observations and services
func (l *Loader) createObservationServiceRelationships(ctx context.Context, tx neo4j.ManagedTransaction, events []*domain.ObservationEvent) (int64, error) {
	var relationships []map[string]interface{}

	for _, event := range events {
		if event.ServiceName != nil && event.Namespace != nil {
			uid := fmt.Sprintf("obs-svc-%s-%s", *event.Namespace, *event.ServiceName)
			relationships = append(relationships, map[string]interface{}{
				"observation_id": event.ID,
				"service_uid":    uid,
				"timestamp":      event.Timestamp.UnixMilli(),
			})
		}
	}

	if len(relationships) == 0 {
		return 0, nil
	}

	query := `
		UNWIND $relationships AS rel
		MATCH (o:Observation {id: rel.observation_id})
		MATCH (s:Service {uid: rel.service_uid})
		CREATE (o)-[:AFFECTS {created_at: datetime({epochMillis: rel.timestamp})}]->(s)
		RETURN count(*) AS relationships_created
	`

	params := map[string]interface{}{
		"relationships": relationships,
	}

	result, err := tx.Run(ctx, query, params)
	if err != nil {
		return 0, fmt.Errorf("failed to execute observation-service relationship query: %w", err)
	}

	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get observation-service relationship result: %w", err)
	}

	relationshipsCreated, _ := record.Get("relationships_created")
	if count, ok := relationshipsCreated.(int64); ok {
		return count, nil
	}

	return 0, nil
}

// createObservationNodeRelationships creates OCCURS_ON relationships between observations and nodes
func (l *Loader) createObservationNodeRelationships(ctx context.Context, tx neo4j.ManagedTransaction, events []*domain.ObservationEvent) (int64, error) {
	var relationships []map[string]interface{}

	for _, event := range events {
		if event.NodeName != nil {
			relationships = append(relationships, map[string]interface{}{
				"observation_id": event.ID,
				"node_name":      *event.NodeName,
				"timestamp":      event.Timestamp.UnixMilli(),
			})
		}
	}

	if len(relationships) == 0 {
		return 0, nil
	}

	query := `
		UNWIND $relationships AS rel
		MATCH (o:Observation {id: rel.observation_id})
		MATCH (n:Node {name: rel.node_name})
		CREATE (o)-[:OCCURS_ON {created_at: datetime({epochMillis: rel.timestamp})}]->(n)
		RETURN count(*) AS relationships_created
	`

	params := map[string]interface{}{
		"relationships": relationships,
	}

	result, err := tx.Run(ctx, query, params)
	if err != nil {
		return 0, fmt.Errorf("failed to execute observation-node relationship query: %w", err)
	}

	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get observation-node relationship result: %w", err)
	}

	relationshipsCreated, _ := record.Get("relationships_created")
	if count, ok := relationshipsCreated.(int64); ok {
		return count, nil
	}

	return 0, nil
}

// createCausalRelationships creates CAUSED relationships between observations
func (l *Loader) createCausalRelationships(ctx context.Context, tx neo4j.ManagedTransaction, events []*domain.ObservationEvent) (int64, error) {
	var relationships []map[string]interface{}

	for _, event := range events {
		if event.CausedBy != nil {
			relationships = append(relationships, map[string]interface{}{
				"effect_id": event.ID,
				"cause_id":  *event.CausedBy,
				"timestamp": event.Timestamp.UnixMilli(),
			})
		}
		if event.ParentID != nil {
			relationships = append(relationships, map[string]interface{}{
				"child_id":  event.ID,
				"parent_id": *event.ParentID,
				"timestamp": event.Timestamp.UnixMilli(),
			})
		}
	}

	if len(relationships) == 0 {
		return 0, nil
	}

	query := `
		UNWIND $relationships AS rel
		OPTIONAL MATCH (cause:Observation {id: rel.cause_id})
		OPTIONAL MATCH (effect:Observation {id: rel.effect_id})
		OPTIONAL MATCH (parent:Observation {id: rel.parent_id})
		OPTIONAL MATCH (child:Observation {id: rel.child_id})
		WITH rel, cause, effect, parent, child
		WHERE (cause IS NOT NULL AND effect IS NOT NULL) OR (parent IS NOT NULL AND child IS NOT NULL)
		FOREACH (_ IN CASE WHEN cause IS NOT NULL AND effect IS NOT NULL THEN [1] ELSE [] END |
			CREATE (cause)-[:CAUSED {created_at: datetime({epochMillis: rel.timestamp})}]->(effect)
		)
		FOREACH (_ IN CASE WHEN parent IS NOT NULL AND child IS NOT NULL THEN [1] ELSE [] END |
			CREATE (parent)-[:PARENT_OF {created_at: datetime({epochMillis: rel.timestamp})}]->(child)
		)
		RETURN count(*) AS relationships_created
	`

	params := map[string]interface{}{
		"relationships": relationships,
	}

	result, err := tx.Run(ctx, query, params)
	if err != nil {
		return 0, fmt.Errorf("failed to execute causal relationship query: %w", err)
	}

	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get causal relationship result: %w", err)
	}

	relationshipsCreated, _ := record.Get("relationships_created")
	if count, ok := relationshipsCreated.(int64); ok {
		return count, nil
	}

	return 0, nil
}

// eventToParams converts an ObservationEvent to Neo4j parameters
func (l *Loader) eventToParams(event *domain.ObservationEvent) map[string]interface{} {
	params := map[string]interface{}{
		"id":        event.ID,
		"timestamp": event.Timestamp.UnixMilli(),
		"source":    event.Source,
		"type":      event.Type,
	}

	// Optional fields - only include if not nil
	if event.PID != nil {
		params["pid"] = *event.PID
	}
	if event.ContainerID != nil {
		params["container_id"] = *event.ContainerID
	}
	if event.PodName != nil {
		params["pod_name"] = *event.PodName
	}
	if event.Namespace != nil {
		params["namespace"] = *event.Namespace
	}
	if event.ServiceName != nil {
		params["service_name"] = *event.ServiceName
	}
	if event.NodeName != nil {
		params["node_name"] = *event.NodeName
	}
	if event.Action != nil {
		params["action"] = *event.Action
	}
	if event.Target != nil {
		params["target"] = *event.Target
	}
	if event.Result != nil {
		params["result"] = *event.Result
	}
	if event.Reason != nil {
		params["reason"] = *event.Reason
	}
	if event.Duration != nil {
		params["duration"] = *event.Duration
	}
	if event.Size != nil {
		params["size"] = *event.Size
	}
	if event.Count != nil {
		params["count"] = *event.Count
	}
	if event.CausedBy != nil {
		params["caused_by"] = *event.CausedBy
	}
	if event.ParentID != nil {
		params["parent_id"] = *event.ParentID
	}

	// Convert data map to Neo4j format
	if len(event.Data) > 0 {
		params["data"] = event.Data
	}

	return params
}
