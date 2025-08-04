package k8sgrapher

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// updateServiceNode updates a Service node and its relationships
func (g *K8sGrapher) updateServiceNode(ctx context.Context, update graphUpdate) error {
	svc := update.data.(*corev1.Service)

	session := g.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	switch update.operation {
	case "create", "update":
		// Create/update service node
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			// Create or update the Service node
			query := `
				MERGE (s:Service {namespace: $namespace, name: $name})
				SET s.uid = $uid,
					s.selector = $selector,
					s.type = $type,
					s.clusterIP = $clusterIP,
					s.lastUpdated = datetime(),
					s.labels = $labels
			`
			params := map[string]interface{}{
				"namespace": svc.Namespace,
				"name":      svc.Name,
				"uid":       string(svc.UID),
				"selector":  formatSelector(svc.Spec.Selector),
				"type":      string(svc.Spec.Type),
				"clusterIP": svc.Spec.ClusterIP,
				"labels":    formatLabels(svc.Labels),
			}

			if _, err := tx.Run(ctx, query, params); err != nil {
				return nil, err
			}

			// Update service->pod relationships
			if err := g.updateServicePodRelationships(ctx, tx, svc); err != nil {
				return nil, err
			}

			return nil, nil
		})

		if err == nil {
			g.instrumentation.RelationshipsDiscovered.Add(ctx, 1,
				metric.WithAttributes(attribute.String("type", "service")))
		}
		return err

	case "delete":
		// Delete service node and relationships
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			query := `
				MATCH (s:Service {namespace: $namespace, name: $name})
				DETACH DELETE s
			`
			params := map[string]interface{}{
				"namespace": svc.Namespace,
				"name":      svc.Name,
			}
			_, err := tx.Run(ctx, query, params)
			return nil, err
		})
		return err

	default:
		return fmt.Errorf("unknown operation: %s", update.operation)
	}
}

// updatePodNode updates a Pod node and its relationships
func (g *K8sGrapher) updatePodNode(ctx context.Context, update graphUpdate) error {
	pod := update.data.(*corev1.Pod)

	session := g.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	switch update.operation {
	case "create", "update":
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			// Create or update the Pod node
			query := `
				MERGE (p:Pod {namespace: $namespace, name: $name})
				SET p.uid = $uid,
					p.labels = $labels,
					p.phase = $phase,
					p.nodeName = $nodeName,
					p.ready = $ready,
					p.lastUpdated = datetime(),
					p.ownerKind = $ownerKind,
					p.ownerName = $ownerName
			`

			ownerKind, ownerName := getOwnerInfo(pod)
			params := map[string]interface{}{
				"namespace": pod.Namespace,
				"name":      pod.Name,
				"uid":       string(pod.UID),
				"labels":    formatLabels(pod.Labels),
				"phase":     string(pod.Status.Phase),
				"nodeName":  pod.Spec.NodeName,
				"ready":     isPodReady(pod),
				"ownerKind": ownerKind,
				"ownerName": ownerName,
			}

			if _, err := tx.Run(ctx, query, params); err != nil {
				return nil, err
			}

			// Update pod->configmap relationships
			if err := g.updatePodConfigMapRelationships(ctx, tx, pod); err != nil {
				return nil, err
			}

			// Update pod->secret relationships
			if err := g.updatePodSecretRelationships(ctx, tx, pod); err != nil {
				return nil, err
			}

			// Update pod->pvc relationships
			if err := g.updatePodPVCRelationships(ctx, tx, pod); err != nil {
				return nil, err
			}

			// Update ownership relationships
			if err := g.updatePodOwnershipRelationships(ctx, tx, pod); err != nil {
				return nil, err
			}

			return nil, nil
		})

		if err == nil {
			g.instrumentation.RelationshipsDiscovered.Add(ctx, 1,
				metric.WithAttributes(attribute.String("type", "pod")))
		}
		return err

	case "delete":
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			query := `
				MATCH (p:Pod {namespace: $namespace, name: $name})
				DETACH DELETE p
			`
			params := map[string]interface{}{
				"namespace": pod.Namespace,
				"name":      pod.Name,
			}
			_, err := tx.Run(ctx, query, params)
			return nil, err
		})
		return err

	default:
		return fmt.Errorf("unknown operation: %s", update.operation)
	}
}

// updateConfigMapNode updates a ConfigMap node
func (g *K8sGrapher) updateConfigMapNode(ctx context.Context, update graphUpdate) error {
	cm := update.data.(*corev1.ConfigMap)

	session := g.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	switch update.operation {
	case "create", "update":
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			query := `
				MERGE (cm:ConfigMap {namespace: $namespace, name: $name})
				SET cm.uid = $uid,
					cm.dataKeys = $dataKeys,
					cm.lastUpdated = datetime(),
					cm.resourceVersion = $resourceVersion
			`
			params := map[string]interface{}{
				"namespace":       cm.Namespace,
				"name":            cm.Name,
				"uid":             string(cm.UID),
				"dataKeys":        getMapKeys(cm.Data),
				"resourceVersion": cm.ResourceVersion,
			}
			_, err := tx.Run(ctx, query, params)
			return nil, err
		})

		if err == nil {
			g.instrumentation.RelationshipsDiscovered.Add(ctx, 1,
				metric.WithAttributes(attribute.String("type", "configmap")))
		}
		return err

	case "delete":
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			query := `
				MATCH (cm:ConfigMap {namespace: $namespace, name: $name})
				DETACH DELETE cm
			`
			params := map[string]interface{}{
				"namespace": cm.Namespace,
				"name":      cm.Name,
			}
			_, err := tx.Run(ctx, query, params)
			return nil, err
		})
		return err

	default:
		return fmt.Errorf("unknown operation: %s", update.operation)
	}
}

// updateSecretNode updates a Secret node
func (g *K8sGrapher) updateSecretNode(ctx context.Context, update graphUpdate) error {
	secret := update.data.(*corev1.Secret)

	session := g.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	switch update.operation {
	case "create", "update":
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			query := `
				MERGE (s:Secret {namespace: $namespace, name: $name})
				SET s.uid = $uid,
					s.type = $type,
					s.dataKeys = $dataKeys,
					s.lastUpdated = datetime()
			`
			params := map[string]interface{}{
				"namespace": secret.Namespace,
				"name":      secret.Name,
				"uid":       string(secret.UID),
				"type":      string(secret.Type),
				"dataKeys":  getSecretKeys(secret.Data),
			}
			_, err := tx.Run(ctx, query, params)
			return nil, err
		})

		if err == nil {
			g.instrumentation.RelationshipsDiscovered.Add(ctx, 1,
				metric.WithAttributes(attribute.String("type", "secret")))
		}
		return err

	case "delete":
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			query := `
				MATCH (s:Secret {namespace: $namespace, name: $name})
				DETACH DELETE s
			`
			params := map[string]interface{}{
				"namespace": secret.Namespace,
				"name":      secret.Name,
			}
			_, err := tx.Run(ctx, query, params)
			return nil, err
		})
		return err

	default:
		return fmt.Errorf("unknown operation: %s", update.operation)
	}
}

// updateDeploymentNode updates a Deployment node
func (g *K8sGrapher) updateDeploymentNode(ctx context.Context, update graphUpdate) error {
	deploy := update.data.(*appsv1.Deployment)

	session := g.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	switch update.operation {
	case "create", "update":
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			query := `
				MERGE (d:Deployment {namespace: $namespace, name: $name})
				SET d.uid = $uid,
					d.replicas = $replicas,
					d.selector = $selector,
					d.lastUpdated = datetime()
			`
			params := map[string]interface{}{
				"namespace": deploy.Namespace,
				"name":      deploy.Name,
				"uid":       string(deploy.UID),
				"replicas":  *deploy.Spec.Replicas,
				"selector":  formatLabelSelector(deploy.Spec.Selector),
			}
			_, err := tx.Run(ctx, query, params)
			return nil, err
		})

		if err == nil {
			g.instrumentation.RelationshipsDiscovered.Add(ctx, 1,
				metric.WithAttributes(attribute.String("type", "deployment")))
		}
		return err

	case "delete":
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			query := `
				MATCH (d:Deployment {namespace: $namespace, name: $name})
				DETACH DELETE d
			`
			params := map[string]interface{}{
				"namespace": deploy.Namespace,
				"name":      deploy.Name,
			}
			_, err := tx.Run(ctx, query, params)
			return nil, err
		})
		return err

	default:
		return fmt.Errorf("unknown operation: %s", update.operation)
	}
}

// updateReplicaSetNode updates a ReplicaSet node
func (g *K8sGrapher) updateReplicaSetNode(ctx context.Context, update graphUpdate) error {
	rs := update.data.(*appsv1.ReplicaSet)

	session := g.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	switch update.operation {
	case "create", "update":
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			// Create ReplicaSet node
			query := `
				MERGE (rs:ReplicaSet {namespace: $namespace, name: $name})
				SET rs.uid = $uid,
					rs.replicas = $replicas,
					rs.selector = $selector,
					rs.lastUpdated = datetime()
			`
			params := map[string]interface{}{
				"namespace": rs.Namespace,
				"name":      rs.Name,
				"uid":       string(rs.UID),
				"replicas":  *rs.Spec.Replicas,
				"selector":  formatLabelSelector(rs.Spec.Selector),
			}
			if _, err := tx.Run(ctx, query, params); err != nil {
				return nil, err
			}

			// Create ownership relationship to Deployment
			if len(rs.OwnerReferences) > 0 {
				for _, owner := range rs.OwnerReferences {
					if owner.Kind == "Deployment" {
						ownerQuery := `
							MATCH (d:Deployment {namespace: $namespace, name: $ownerName})
							MATCH (rs:ReplicaSet {namespace: $namespace, name: $rsName})
							MERGE (d)-[:OWNS]->(rs)
						`
						ownerParams := map[string]interface{}{
							"namespace": rs.Namespace,
							"ownerName": owner.Name,
							"rsName":    rs.Name,
						}
						if _, err := tx.Run(ctx, ownerQuery, ownerParams); err != nil {
							g.logger.Warn("Failed to create deployment->replicaset relationship",
								zap.Error(err))
						}
					}
				}
			}

			return nil, nil
		})

		if err == nil {
			g.instrumentation.RelationshipsDiscovered.Add(ctx, 1,
				metric.WithAttributes(attribute.String("type", "replicaset")))
			g.instrumentation.OwnershipChains.Add(ctx, 1)
		}
		return err

	case "delete":
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			query := `
				MATCH (rs:ReplicaSet {namespace: $namespace, name: $name})
				DETACH DELETE rs
			`
			params := map[string]interface{}{
				"namespace": rs.Namespace,
				"name":      rs.Name,
			}
			_, err := tx.Run(ctx, query, params)
			return nil, err
		})
		if err == nil {
			g.instrumentation.OwnershipChains.Add(ctx, -1)
		}
		return err

	default:
		return fmt.Errorf("unknown operation: %s", update.operation)
	}
}

// updatePVCNode updates a PVC node
func (g *K8sGrapher) updatePVCNode(ctx context.Context, update graphUpdate) error {
	pvc := update.data.(*corev1.PersistentVolumeClaim)

	session := g.neo4jDriver.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	switch update.operation {
	case "create", "update":
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			query := `
				MERGE (pvc:PVC {namespace: $namespace, name: $name})
				SET pvc.uid = $uid,
					pvc.storageClass = $storageClass,
					pvc.phase = $phase,
					pvc.capacity = $capacity,
					pvc.lastUpdated = datetime()
			`

			var storageClass string
			if pvc.Spec.StorageClassName != nil {
				storageClass = *pvc.Spec.StorageClassName
			}

			var capacity string
			if pvc.Status.Capacity != nil {
				if storage, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
					capacity = storage.String()
				}
			}

			params := map[string]interface{}{
				"namespace":    pvc.Namespace,
				"name":         pvc.Name,
				"uid":          string(pvc.UID),
				"storageClass": storageClass,
				"phase":        string(pvc.Status.Phase),
				"capacity":     capacity,
			}
			_, err := tx.Run(ctx, query, params)
			return nil, err
		})

		if err == nil {
			g.instrumentation.RelationshipsDiscovered.Add(ctx, 1,
				metric.WithAttributes(attribute.String("type", "pvc")))
		}
		return err

	case "delete":
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
			query := `
				MATCH (pvc:PVC {namespace: $namespace, name: $name})
				DETACH DELETE pvc
			`
			params := map[string]interface{}{
				"namespace": pvc.Namespace,
				"name":      pvc.Name,
			}
			_, err := tx.Run(ctx, query, params)
			return nil, err
		})
		return err

	default:
		return fmt.Errorf("unknown operation: %s", update.operation)
	}
}
