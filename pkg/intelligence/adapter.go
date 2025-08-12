package intelligence

import (
	"context"
	"fmt"

	graph "github.com/yairfalse/tapio/pkg/integrations/neo4j"
)

// GraphClientAdapter adapts the Neo4j client to intelligence layer interfaces
// This maintains architecture boundaries by providing a generic interface
// while converting between typed Neo4j client and intelligence layer expectations
type GraphClientAdapter struct {
	client *graph.Client
}

// NewGraphClientAdapter creates an adapter for Neo4j client
func NewGraphClientAdapter(client *graph.Client) *GraphClientAdapter {
	return &GraphClientAdapter{client: client}
}

// ExecuteQuery executes a query and converts results to generic format
// This satisfies both queries.GraphClient and patterns.GraphClient interfaces
func (a *GraphClientAdapter) ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) ([]map[string]interface{}, error) {
	// Convert generic params to typed QueryParams
	typedParams := a.convertToQueryParams(params)

	// Execute query through Neo4j client
	result, err := a.client.ExecuteQuery(ctx, query, typedParams)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}

	// Convert typed QueryResult to generic format
	return a.convertToGenericResults(result), nil
}

// convertToQueryParams converts map[string]interface{} to graph.QueryParams
func (a *GraphClientAdapter) convertToQueryParams(params map[string]interface{}) graph.QueryParams {
	queryParams := graph.QueryParams{
		StringParams: make(map[string]string),
		IntParams:    make(map[string]int64),
		FloatParams:  make(map[string]float64),
		BoolParams:   make(map[string]bool),
	}

	for key, value := range params {
		switch v := value.(type) {
		case string:
			if key == "resourceType" {
				queryParams.ResourceType = v
			} else if key == "namespace" {
				queryParams.Namespace = v
			} else if key == "name" {
				queryParams.Name = v
			} else if key == "uid" {
				queryParams.UID = v
			} else {
				queryParams.StringParams[key] = v
			}
		case int:
			if key == "limit" {
				queryParams.Limit = int(v)
			} else if key == "offset" {
				queryParams.Offset = int(v)
			} else {
				queryParams.IntParams[key] = int64(v)
			}
		case int64:
			if key == "limit" {
				queryParams.Limit = int(v)
			} else if key == "offset" {
				queryParams.Offset = int(v)
			} else {
				queryParams.IntParams[key] = v
			}
		case float64:
			queryParams.FloatParams[key] = v
		case bool:
			queryParams.BoolParams[key] = v
		default:
			// Convert unknown types to string
			queryParams.StringParams[key] = fmt.Sprintf("%v", v)
		}
	}

	return queryParams
}

// convertToGenericResults converts graph.QueryResult to []map[string]interface{}
func (a *GraphClientAdapter) convertToGenericResults(result *graph.QueryResult) []map[string]interface{} {
	if result == nil || len(result.Records) == 0 {
		return []map[string]interface{}{}
	}

	genericResults := make([]map[string]interface{}, len(result.Records))

	for i, record := range result.Records {
		genericRecord := make(map[string]interface{})

		// Convert strongly typed values to generic format
		for k, v := range record.StringValues {
			genericRecord[k] = v
		}
		for k, v := range record.IntValues {
			genericRecord[k] = v
		}
		for k, v := range record.FloatValues {
			genericRecord[k] = v
		}
		for k, v := range record.BoolValues {
			genericRecord[k] = v
		}

		// Convert typed node objects to generic maps
		if record.Resource != nil {
			resourceMap := a.convertResourceToMap(record.Resource)
			// Merge resource fields or use specific key depending on query
			if len(genericRecord) == 0 || a.shouldMergeResource(genericRecord) {
				for k, v := range resourceMap {
					genericRecord[k] = v
				}
			} else {
				genericRecord["resource"] = resourceMap
			}
		}

		if record.Event != nil {
			eventMap := a.convertEventToMap(record.Event)
			if len(genericRecord) == 0 || a.shouldMergeEvent(genericRecord) {
				for k, v := range eventMap {
					genericRecord[k] = v
				}
			} else {
				genericRecord["event"] = eventMap
			}
		}

		if record.Correlation != nil {
			corrMap := a.convertCorrelationToMap(record.Correlation)
			if len(genericRecord) == 0 {
				for k, v := range corrMap {
					genericRecord[k] = v
				}
			} else {
				genericRecord["correlation"] = corrMap
			}
		}

		// Handle relationships
		if len(record.Relationships) > 0 {
			relationships := make([]map[string]interface{}, len(record.Relationships))
			for j, rel := range record.Relationships {
				relationships[j] = a.convertRelationshipToMap(rel)
			}
			genericRecord["relationships"] = relationships
		}

		// Handle aggregated data
		if record.Count > 0 {
			genericRecord["count"] = record.Count
		}
		if len(record.StringList) > 0 {
			genericRecord["string_list"] = record.StringList
		}

		genericResults[i] = genericRecord
	}

	return genericResults
}

// convertResourceToMap converts ResourceNode to map[string]interface{}
func (a *GraphClientAdapter) convertResourceToMap(resource *graph.ResourceNode) map[string]interface{} {
	resourceMap := map[string]interface{}{
		"uid":         resource.UID,
		"name":        resource.Name,
		"namespace":   resource.Namespace,
		"type":        resource.Type,
		"kind":        resource.Kind,
		"ready":       resource.Ready,
		"created_at":  resource.CreatedAt.Unix(),
		"updated_at":  resource.UpdatedAt.Unix(),
		"status":      resource.Status,
		"phase":       resource.Phase,
		"api_version": resource.APIVersion,
	}

	// Add resource-specific fields if set
	if resource.Replicas > 0 {
		resourceMap["replicas"] = resource.Replicas
	}
	if resource.ReadyReplicas > 0 {
		resourceMap["ready_replicas"] = resource.ReadyReplicas
	}
	if resource.NodeName != "" {
		resourceMap["node_name"] = resource.NodeName
	}
	if resource.ClusterIP != "" {
		resourceMap["cluster_ip"] = resource.ClusterIP
	}
	if resource.PodIP != "" {
		resourceMap["pod_ip"] = resource.PodIP
	}

	// Add labels and annotations if present
	if len(resource.Labels) > 0 {
		resourceMap["labels"] = resource.Labels
	}
	if len(resource.Annotations) > 0 {
		resourceMap["annotations"] = resource.Annotations
	}

	return resourceMap
}

// convertEventToMap converts EventNode to map[string]interface{}
func (a *GraphClientAdapter) convertEventToMap(event *graph.EventNode) map[string]interface{} {
	eventMap := map[string]interface{}{
		"id":           event.ID,
		"type":         event.Type,
		"source":       event.Source,
		"timestamp":    event.Timestamp.Unix(),
		"severity":     string(event.Severity),
		"message":      event.Message,
		"reason":       event.Reason,
		"category":     event.Category,
		"component":    event.Component,
		"resource_uid": event.ResourceUID,
		"trace_id":     event.TraceID,
		"span_id":      event.SpanID,
	}

	if len(event.Metadata) > 0 {
		eventMap["metadata"] = event.Metadata
	}

	return eventMap
}

// convertCorrelationToMap converts CorrelationNode to map[string]interface{}
func (a *GraphClientAdapter) convertCorrelationToMap(corr *graph.CorrelationNode) map[string]interface{} {
	corrMap := map[string]interface{}{
		"id":         corr.ID,
		"type":       corr.Type,
		"trace_id":   corr.TraceID,
		"created_at": corr.CreatedAt.Unix(),
		"confidence": corr.Confidence,
		"summary":    corr.Summary,
		"details":    corr.Details,
		"start_time": corr.StartTime.Unix(),
		"end_time":   corr.EndTime.Unix(),
		"root_cause": corr.RootCause,
		"impact":     corr.Impact,
		"severity":   corr.Severity,
	}

	if len(corr.Evidence) > 0 {
		corrMap["evidence"] = corr.Evidence
	}

	return corrMap
}

// convertRelationshipToMap converts Relationship to map[string]interface{}
func (a *GraphClientAdapter) convertRelationshipToMap(rel graph.Relationship) map[string]interface{} {
	relMap := map[string]interface{}{
		"type":          string(rel.Type),
		"start_node_id": rel.StartNodeID,
		"end_node_id":   rel.EndNodeID,
		"created_at":    rel.Properties.CreatedAt.Unix(),
	}

	// Add relationship-specific properties
	if rel.Properties.Weight > 0 {
		relMap["weight"] = rel.Properties.Weight
	}
	if rel.Properties.Confidence > 0 {
		relMap["confidence"] = rel.Properties.Confidence
	}
	if rel.Properties.Port > 0 {
		relMap["port"] = rel.Properties.Port
	}
	if rel.Properties.Protocol != "" {
		relMap["protocol"] = rel.Properties.Protocol
	}
	if rel.Properties.Direction != "" {
		relMap["direction"] = rel.Properties.Direction
	}
	if rel.Properties.Controller {
		relMap["controller"] = rel.Properties.Controller
	}
	if rel.Properties.BlockOwnerDeletion {
		relMap["block_owner_deletion"] = rel.Properties.BlockOwnerDeletion
	}
	if rel.Properties.Latency > 0 {
		relMap["latency"] = rel.Properties.Latency.Milliseconds()
	}
	if rel.Properties.Count > 0 {
		relMap["count"] = rel.Properties.Count
	}

	return relMap
}

// shouldMergeResource determines if resource fields should be merged into root
func (a *GraphClientAdapter) shouldMergeResource(record map[string]interface{}) bool {
	// If record has typical resource fields, merge resource data
	_, hasUID := record["uid"]
	_, hasName := record["name"]
	_, hasNamespace := record["namespace"]
	return hasUID || hasName || hasNamespace
}

// shouldMergeEvent determines if event fields should be merged into root
func (a *GraphClientAdapter) shouldMergeEvent(record map[string]interface{}) bool {
	// If record has typical event fields, merge event data
	_, hasID := record["id"]
	_, hasType := record["type"]
	_, hasTimestamp := record["timestamp"]
	return hasID || hasType || hasTimestamp
}

// ConvertPropertyValues converts map[string]interface{} to map[string]PropertyValue
// This is used when the intelligence layer needs to create relationships
func (a *GraphClientAdapter) ConvertPropertyValues(properties map[string]interface{}) map[string]graph.PropertyValue {
	if properties == nil {
		return nil
	}

	converted := make(map[string]graph.PropertyValue)
	for key, value := range properties {
		var propVal graph.PropertyValue

		switch v := value.(type) {
		case string:
			propVal.StringVal = &v
		case int:
			int64Val := int64(v)
			propVal.IntVal = &int64Val
		case int64:
			propVal.IntVal = &v
		case float64:
			propVal.FloatVal = &v
		case bool:
			propVal.BoolVal = &v
		default:
			// Convert unknown types to string
			strVal := fmt.Sprintf("%v", v)
			propVal.StringVal = &strVal
		}

		converted[key] = propVal
	}

	return converted
}
