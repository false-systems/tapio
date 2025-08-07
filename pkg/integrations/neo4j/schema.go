package neo4j

import (
	"context"
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/yairfalse/tapio/pkg/domain"
)

// NodeType represents different K8s resource types in the graph
type NodeType string

const (
	NodePod        NodeType = "Pod"
	NodeService    NodeType = "Service"
	NodeDeployment NodeType = "Deployment"
	NodeReplicaSet NodeType = "ReplicaSet"
	NodeConfigMap  NodeType = "ConfigMap"
	NodeSecret     NodeType = "Secret"
	NodeNode       NodeType = "Node"
	NodeEvent      NodeType = "Event"
	NodeNamespace  NodeType = "Namespace"
)

// RelationType represents relationships between nodes
type RelationType string

const (
	RelOwnedBy     RelationType = "OWNED_BY"
	RelSelectedBy  RelationType = "SELECTED_BY"
	RelMounts      RelationType = "MOUNTS"
	RelRunsOn      RelationType = "RUNS_ON"
	RelCausedBy    RelationType = "CAUSED_BY"
	RelTriggeredBy RelationType = "TRIGGERED_BY"
	RelAffects     RelationType = "AFFECTS"
	RelConnectsTo  RelationType = "CONNECTS_TO"
	RelInNamespace RelationType = "IN_NAMESPACE"
)

// CreateOrUpdateNode creates or updates a K8s resource node
func (c *Client) CreateOrUpdateNode(ctx context.Context, event *domain.UnifiedEvent) error {
	if event.Entity == nil {
		return nil // Skip if no entity
	}

	query := `
		MERGE (n:%s {uid: $uid})
		SET n.name = $name,
		    n.namespace = $namespace,
		    n.kind = $kind,
		    n.timestamp = $timestamp,
		    n.labels = $labels,
		    n.annotations = $annotations,
		    n.resourceVersion = $resourceVersion
		RETURN n
	`

	nodeType := getNodeType(event.Entity.Type)
	query = fmt.Sprintf(query, nodeType)

	params := map[string]interface{}{
		"uid":             event.Entity.UID,
		"name":            event.Entity.Name,
		"namespace":       event.Entity.Namespace,
		"kind":            event.Entity.Type,
		"timestamp":       event.Timestamp.Unix(),
		"labels":          mapToStringArray(event.Entity.Labels),
		"annotations":     []string{}, // EntityContext doesn't have annotations
		"resourceVersion": "",         // EntityContext doesn't have resourceVersion
	}

	// If we have K8s context, use those values
	if event.K8sContext != nil {
		params["annotations"] = mapToStringArray(event.K8sContext.Annotations)
		params["resourceVersion"] = event.K8sContext.ResourceVersion
	}

	return c.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
		_, err := tx.Run(ctx, query, params)
		return err
	})
}

// CreateEvent creates an event node
func (c *Client) CreateEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	query := `
		CREATE (e:Event {
			id: $id,
			timestamp: $timestamp,
			type: $type,
			severity: $severity,
			message: $message,
			source: $source,
			traceId: $traceId,
			spanId: $spanId
		})
		RETURN e
	`

	params := map[string]interface{}{
		"id":        event.ID,
		"timestamp": event.Timestamp.Unix(),
		"type":      event.Type,
		"severity":  string(event.Severity),
		"message":   event.Message,
		"source":    event.Source,
		"traceId":   "",
		"spanId":    "",
	}

	if event.TraceContext != nil {
		params["traceId"] = event.TraceContext.TraceID
		params["spanId"] = event.TraceContext.SpanID
	}

	return c.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
		_, err := tx.Run(ctx, query, params)
		return err
	})
}

// CreateRelationship creates a relationship between nodes
func (c *Client) CreateRelationship(ctx context.Context, fromUID, toUID string, relType RelationType, properties map[string]interface{}) error {
	query := fmt.Sprintf(`
		MATCH (from {uid: $fromUID})
		MATCH (to {uid: $toUID})
		MERGE (from)-[r:%s]->(to)
		SET r.timestamp = $timestamp
	`, relType)

	// Add properties to relationship
	if len(properties) > 0 {
		for key := range properties {
			query += fmt.Sprintf(", r.%s = $%s", key, key)
		}
	}

	params := map[string]interface{}{
		"fromUID":   fromUID,
		"toUID":     toUID,
		"timestamp": time.Now().Unix(),
	}

	// Merge properties into params
	for k, v := range properties {
		params[k] = v
	}

	return c.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
		_, err := tx.Run(ctx, query, params)
		return err
	})
}

// CreateEventRelationship links an event to an entity
func (c *Client) CreateEventRelationship(ctx context.Context, eventID string, entityUID string, relType RelationType) error {
	query := fmt.Sprintf(`
		MATCH (e:Event {id: $eventID})
		MATCH (n {uid: $entityUID})
		CREATE (e)-[:%s]->(n)
	`, relType)

	params := map[string]interface{}{
		"eventID":   eventID,
		"entityUID": entityUID,
	}

	return c.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
		_, err := tx.Run(ctx, query, params)
		return err
	})
}

// LinkEventCausality creates CAUSED_BY relationships between events
func (c *Client) LinkEventCausality(ctx context.Context, effectEventID, causeEventID string, confidence float64) error {
	query := `
		MATCH (effect:Event {id: $effectID})
		MATCH (cause:Event {id: $causeID})
		CREATE (effect)-[:CAUSED_BY {confidence: $confidence, timestamp: $timestamp}]->(cause)
	`

	params := map[string]interface{}{
		"effectID":   effectEventID,
		"causeID":    causeEventID,
		"confidence": confidence,
		"timestamp":  time.Now().Unix(),
	}

	return c.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) error {
		_, err := tx.Run(ctx, query, params)
		return err
	})
}

// getNodeType converts entity type to graph node type
func getNodeType(entityType string) NodeType {
	switch entityType {
	case "pod":
		return NodePod
	case "service":
		return NodeService
	case "deployment":
		return NodeDeployment
	case "replicaset":
		return NodeReplicaSet
	case "configmap":
		return NodeConfigMap
	case "secret":
		return NodeSecret
	case "node":
		return NodeNode
	case "namespace":
		return NodeNamespace
	default:
		return NodeType(entityType)
	}
}

// mapToStringArray converts a map[string]string to an array of "key=value" strings
// Neo4j doesn't accept Go maps directly, so we convert to string arrays
func mapToStringArray(m map[string]string) []string {
	if m == nil {
		return []string{}
	}

	result := make([]string, 0, len(m))
	for key, value := range m {
		result = append(result, fmt.Sprintf("%s=%s", key, value))
	}
	return result
}
