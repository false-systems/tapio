package telemetry

import (
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// K8sGrapherInstrumentation provides telemetry for K8s relationship graph builder
type K8sGrapherInstrumentation struct {
	*ServiceInstrumentation

	// Metrics for relationship discovery
	RelationshipsDiscovered metric.Int64Counter       // Total relationships found
	GraphUpdateDuration     metric.Float64Histogram   // Time to update graph
	K8sWatchEvents          metric.Int64Counter       // K8s API watch events
	GraphQueryDuration      metric.Float64Histogram   // Neo4j query times
	ActiveRelationships     metric.Int64UpDownCounter // Current relationship count

	// Detailed metrics by type
	ServicePodMappings metric.Int64UpDownCounter
	ConfigMapMounts    metric.Int64UpDownCounter
	SecretReferences   metric.Int64UpDownCounter
	PVCBindings        metric.Int64UpDownCounter
	OwnershipChains    metric.Int64UpDownCounter
}

// NewK8sGrapherInstrumentation creates instrumentation for K8s grapher
func NewK8sGrapherInstrumentation(logger *zap.Logger) (*K8sGrapherInstrumentation, error) {
	base, err := NewServiceInstrumentation("k8s-grapher", logger)
	if err != nil {
		return nil, err
	}

	meter := base.meter

	// Create metrics
	relationshipsDiscovered, err := meter.Int64Counter(
		"tapio.grapher.relationships.discovered",
		metric.WithDescription("Total K8s relationships discovered"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	graphUpdateDuration, err := meter.Float64Histogram(
		"tapio.grapher.graph.update.duration",
		metric.WithDescription("Time to update Neo4j graph"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	k8sWatchEvents, err := meter.Int64Counter(
		"tapio.grapher.k8s.watch.events",
		metric.WithDescription("K8s API watch events received"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	graphQueryDuration, err := meter.Float64Histogram(
		"tapio.grapher.neo4j.query.duration",
		metric.WithDescription("Neo4j query execution time"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	activeRelationships, err := meter.Int64UpDownCounter(
		"tapio.grapher.relationships.active",
		metric.WithDescription("Current number of active relationships"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	// Detailed metrics by relationship type
	servicePodMappings, err := meter.Int64UpDownCounter(
		"tapio.grapher.mappings.service_pod",
		metric.WithDescription("Service to Pod mappings"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	configMapMounts, err := meter.Int64UpDownCounter(
		"tapio.grapher.mounts.configmap",
		metric.WithDescription("ConfigMap mount relationships"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	secretReferences, err := meter.Int64UpDownCounter(
		"tapio.grapher.references.secret",
		metric.WithDescription("Secret reference relationships"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	pvcBindings, err := meter.Int64UpDownCounter(
		"tapio.grapher.bindings.pvc",
		metric.WithDescription("PVC binding relationships"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	ownershipChains, err := meter.Int64UpDownCounter(
		"tapio.grapher.chains.ownership",
		metric.WithDescription("Ownership chain relationships"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	return &K8sGrapherInstrumentation{
		ServiceInstrumentation:  base,
		RelationshipsDiscovered: relationshipsDiscovered,
		GraphUpdateDuration:     graphUpdateDuration,
		K8sWatchEvents:          k8sWatchEvents,
		GraphQueryDuration:      graphQueryDuration,
		ActiveRelationships:     activeRelationships,
		ServicePodMappings:      servicePodMappings,
		ConfigMapMounts:         configMapMounts,
		SecretReferences:        secretReferences,
		PVCBindings:             pvcBindings,
		OwnershipChains:         ownershipChains,
	}, nil
}
