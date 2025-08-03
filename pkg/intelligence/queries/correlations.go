package queries

import (
	"context"
	"fmt"
	"time"
)

// GraphClient interface for querying Neo4j
type GraphClient interface {
	ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) ([]map[string]interface{}, error)
}

// CorrelationQuery handles root cause analysis queries
type CorrelationQuery struct {
	client GraphClient
}

// NewCorrelationQuery creates a new correlation query handler
func NewCorrelationQuery(client GraphClient) *CorrelationQuery {
	return &CorrelationQuery{client: client}
}

// WhyDidPodFail finds the root cause of pod failure
func (q *CorrelationQuery) WhyDidPodFail(ctx context.Context, namespace, podName string, timeWindow time.Duration) (*RootCauseAnalysis, error) {
	query := `
		MATCH (p:Pod {namespace: $namespace, name: $podName})
		OPTIONAL MATCH path = (p)<-[:AFFECTS|CAUSED_BY|TRIGGERED_BY*1..5]-(root)
		WHERE root.timestamp > $startTime AND root.timestamp < p.timestamp
		WITH p, collect(path) as paths, collect(root) as roots
		
		// Also find related events
		OPTIONAL MATCH (e:Event)-[:AFFECTS]->(p)
		WHERE e.timestamp > $startTime
		
		// Find ownership chain
		OPTIONAL MATCH ownership = (p)-[:OWNED_BY*1..3]->(owner)
		
		RETURN p, paths, roots, collect(e) as events, collect(ownership) as ownerships
		ORDER BY size(paths) DESC
		LIMIT 1
	`

	now := time.Now()
	params := map[string]interface{}{
		"namespace": namespace,
		"podName":   podName,
		"startTime": now.Add(-timeWindow).Unix(),
	}

	results, err := q.client.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("correlation query failed: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("pod not found: %s/%s", namespace, podName)
	}

	// Parse results into RootCauseAnalysis
	return q.parseRootCause(results[0]), nil
}

// WhatImpactsService finds all resources affected by a service
func (q *CorrelationQuery) WhatImpactsService(ctx context.Context, namespace, serviceName string) (*ImpactAnalysis, error) {
	query := `
		MATCH (s:Service {namespace: $namespace, name: $serviceName})
		
		// Find pods selected by service
		OPTIONAL MATCH (s)-[:SELECTS]->(p:Pod)
		
		// Find deployments owning those pods
		OPTIONAL MATCH (p)-[:OWNED_BY*1..2]->(d:Deployment)
		
		// Find other services that might be affected
		OPTIONAL MATCH (p)-[:CONNECTS_TO]->(target:Pod)<-[:SELECTS]-(targetSvc:Service)
		
		// Find recent events
		OPTIONAL MATCH (e:Event)-[:AFFECTS]->(s)
		WHERE e.timestamp > $startTime
		
		RETURN s, 
		       collect(DISTINCT p) as pods,
		       collect(DISTINCT d) as deployments,
		       collect(DISTINCT targetSvc) as dependentServices,
		       collect(e) as recentEvents
	`

	params := map[string]interface{}{
		"namespace":   namespace,
		"serviceName": serviceName,
		"startTime":   time.Now().Add(-1 * time.Hour).Unix(),
	}

	results, err := q.client.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("impact query failed: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("service not found: %s/%s", namespace, serviceName)
	}

	return q.parseImpactAnalysis(results[0]), nil
}

// FindCascadingFailures identifies cascade patterns
func (q *CorrelationQuery) FindCascadingFailures(ctx context.Context, startTime time.Time) ([]*CascadePattern, error) {
	query := `
		// Find events that triggered multiple other events
		MATCH (trigger:Event)
		WHERE trigger.timestamp > $startTime
		
		MATCH (trigger)-[:CAUSED_BY|TRIGGERED_BY]->(effect:Event)
		WITH trigger, collect(effect) as effects
		WHERE size(effects) > 2
		
		// Find the affected resources
		OPTIONAL MATCH (trigger)-[:AFFECTS]->(resource)
		OPTIONAL MATCH (effect)-[:AFFECTS]->(affectedResource)
		
		RETURN trigger,
		       effects,
		       collect(DISTINCT resource) as triggerResources,
		       collect(DISTINCT affectedResource) as affectedResources
		ORDER BY size(effects) DESC
		LIMIT 10
	`

	params := map[string]interface{}{
		"startTime": startTime.Unix(),
	}

	results, err := q.client.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("cascade query failed: %w", err)
	}

	var patterns []*CascadePattern
	for _, result := range results {
		pattern := q.parseCascadePattern(result)
		if pattern != nil {
			patterns = append(patterns, pattern)
		}
	}

	return patterns, nil
}

// GetEventSequence gets temporal sequence of events for an entity
func (q *CorrelationQuery) GetEventSequence(ctx context.Context, entityUID string, timeWindow time.Duration) ([]*EventSequence, error) {
	query := `
		MATCH (entity {uid: $uid})
		MATCH (e:Event)-[:AFFECTS]->(entity)
		WHERE e.timestamp > $startTime
		
		// Find related events through causality
		OPTIONAL MATCH (e)-[:CAUSED_BY|TRIGGERED_BY*1..3]-(related:Event)
		
		WITH e, collect(DISTINCT related) as relatedEvents
		RETURN e, relatedEvents
		ORDER BY e.timestamp ASC
	`

	params := map[string]interface{}{
		"uid":       entityUID,
		"startTime": time.Now().Add(-timeWindow).Unix(),
	}

	results, err := q.client.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("sequence query failed: %w", err)
	}

	var sequences []*EventSequence
	for _, result := range results {
		seq := q.parseEventSequence(result)
		if seq != nil {
			sequences = append(sequences, seq)
		}
	}

	return sequences, nil
}

// GetServiceDependencies maps service dependencies through pod connections
func (q *CorrelationQuery) GetServiceDependencies(ctx context.Context, namespace string) (*ServiceDependencyMap, error) {
	query := `
		MATCH (s:Service)
		WHERE s.namespace = $namespace OR $namespace = '*'
		
		// Find pods selected by each service
		MATCH (s)-[:SELECTS]->(p:Pod)
		
		// Find connections between pods
		OPTIONAL MATCH (p)-[:CONNECTS_TO]->(target:Pod)<-[:SELECTS]-(targetSvc:Service)
		
		WITH s, collect(DISTINCT targetSvc) as dependencies
		RETURN s.namespace as namespace,
		       s.name as service,
		       [dep IN dependencies | {namespace: dep.namespace, name: dep.name}] as dependencies
	`

	params := map[string]interface{}{
		"namespace": namespace,
	}

	results, err := q.client.ExecuteQuery(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("dependency query failed: %w", err)
	}

	return q.parseServiceDependencies(results), nil
}

// Helper parsing functions

func (q *CorrelationQuery) parseRootCause(result map[string]interface{}) *RootCauseAnalysis {
	analysis := &RootCauseAnalysis{
		Timestamp: time.Now(),
	}

	// Extract pod info
	if pod, ok := result["p"].(map[string]interface{}); ok {
		analysis.FailedEntity = EntityInfo{
			Type:      "Pod",
			Name:      getString(pod, "name"),
			Namespace: getString(pod, "namespace"),
			UID:       getString(pod, "uid"),
		}
	}

	// Extract root causes from paths
	if roots, ok := result["roots"].([]interface{}); ok {
		for _, root := range roots {
			if rootMap, ok := root.(map[string]interface{}); ok {
				analysis.RootCauses = append(analysis.RootCauses, CauseInfo{
					Type:      getString(rootMap, "type"),
					Message:   getString(rootMap, "message"),
					Timestamp: time.Unix(getInt64(rootMap, "timestamp"), 0),
				})
			}
		}
	}

	// Extract events
	if events, ok := result["events"].([]interface{}); ok {
		for _, event := range events {
			if eventMap, ok := event.(map[string]interface{}); ok {
				analysis.RelatedEvents = append(analysis.RelatedEvents, EventInfo{
					ID:        getString(eventMap, "id"),
					Type:      getString(eventMap, "type"),
					Message:   getString(eventMap, "message"),
					Severity:  getString(eventMap, "severity"),
					Timestamp: time.Unix(getInt64(eventMap, "timestamp"), 0),
				})
			}
		}
	}

	return analysis
}

func (q *CorrelationQuery) parseImpactAnalysis(result map[string]interface{}) *ImpactAnalysis {
	analysis := &ImpactAnalysis{
		Timestamp: time.Now(),
	}

	// Parse service
	if svc, ok := result["s"].(map[string]interface{}); ok {
		analysis.Service = EntityInfo{
			Type:      "Service",
			Name:      getString(svc, "name"),
			Namespace: getString(svc, "namespace"),
			UID:       getString(svc, "uid"),
		}
	}

	// Parse affected pods
	if pods, ok := result["pods"].([]interface{}); ok {
		for _, pod := range pods {
			if podMap, ok := pod.(map[string]interface{}); ok {
				analysis.AffectedPods = append(analysis.AffectedPods, EntityInfo{
					Type:      "Pod",
					Name:      getString(podMap, "name"),
					Namespace: getString(podMap, "namespace"),
					UID:       getString(podMap, "uid"),
				})
			}
		}
	}

	// Parse dependent services
	if services, ok := result["dependentServices"].([]interface{}); ok {
		for _, svc := range services {
			if svcMap, ok := svc.(map[string]interface{}); ok {
				analysis.DependentServices = append(analysis.DependentServices, EntityInfo{
					Type:      "Service",
					Name:      getString(svcMap, "name"),
					Namespace: getString(svcMap, "namespace"),
					UID:       getString(svcMap, "uid"),
				})
			}
		}
	}

	return analysis
}

func (q *CorrelationQuery) parseCascadePattern(result map[string]interface{}) *CascadePattern {
	// Implementation details for parsing cascade patterns
	return &CascadePattern{}
}

func (q *CorrelationQuery) parseEventSequence(result map[string]interface{}) *EventSequence {
	// Implementation details for parsing event sequences
	return &EventSequence{}
}

func (q *CorrelationQuery) parseServiceDependencies(results []map[string]interface{}) *ServiceDependencyMap {
	// Implementation details for parsing service dependencies
	return &ServiceDependencyMap{
		Dependencies: make(map[string][]string),
	}
}

// Helper functions
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key].(int64); ok {
		return v
	}
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	return 0
}
