package k8sgrapher

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// updateServicePodRelationships updates SELECTS relationships between Service and Pods
func (g *K8sGrapher) updateServicePodRelationships(ctx context.Context, tx neo4j.ManagedTransaction, svc *corev1.Service) error {
	// First, remove existing relationships
	deleteQuery := `
		MATCH (s:Service {namespace: $namespace, name: $name})-[r:SELECTS]->()
		DELETE r
	`
	if _, err := tx.Run(ctx, deleteQuery, map[string]interface{}{
		"namespace": svc.Namespace,
		"name":      svc.Name,
	}); err != nil {
		return fmt.Errorf("failed to delete old relationships: %w", err)
	}

	// If no selector, no pods to select
	if len(svc.Spec.Selector) == 0 {
		return nil
	}

	// Create new relationships
	selector := labels.SelectorFromSet(svc.Spec.Selector)
	query := `
		MATCH (s:Service {namespace: $namespace, name: $name})
		MATCH (p:Pod {namespace: $namespace})
		WHERE p.labels IS NOT NULL
		WITH s, p
		WHERE ALL(key IN keys($selector) WHERE p.labels[key] = $selector[key])
		MERGE (s)-[r:SELECTS]->(p)
		SET r.matched = true, r.selector = $selectorStr
	`
	params := map[string]interface{}{
		"namespace":   svc.Namespace,
		"name":        svc.Name,
		"selector":    svc.Spec.Selector,
		"selectorStr": selector.String(),
	}

	result, err := tx.Run(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to create service->pod relationships: %w", err)
	}

	summary, err := result.Consume(ctx)
	if err != nil {
		return err
	}

	if summary.Counters().RelationshipsCreated() > 0 {
		g.instrumentation.ServicePodMappings.Add(ctx, int64(summary.Counters().RelationshipsCreated()))
		g.logger.Debug("Created service->pod relationships",
			zap.String("service", fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)),
			zap.Int64("count", int64(summary.Counters().RelationshipsCreated())))
	}

	return nil
}

// updatePodConfigMapRelationships updates MOUNTS relationships between Pod and ConfigMaps
func (g *K8sGrapher) updatePodConfigMapRelationships(ctx context.Context, tx neo4j.ManagedTransaction, pod *corev1.Pod) error {
	// First, remove existing relationships
	deleteQuery := `
		MATCH (p:Pod {namespace: $namespace, name: $name})-[r:MOUNTS]->(:ConfigMap)
		DELETE r
	`
	if _, err := tx.Run(ctx, deleteQuery, map[string]interface{}{
		"namespace": pod.Namespace,
		"name":      pod.Name,
	}); err != nil {
		return fmt.Errorf("failed to delete old configmap relationships: %w", err)
	}

	relationshipsCreated := int64(0)

	// Check volumes
	for _, volume := range pod.Spec.Volumes {
		if volume.ConfigMap != nil {
			query := `
				MATCH (p:Pod {namespace: $namespace, name: $podName})
				MATCH (cm:ConfigMap {namespace: $namespace, name: $cmName})
				MERGE (p)-[r:MOUNTS {volumeName: $volumeName, mountType: 'volume'}]->(cm)
			`
			params := map[string]interface{}{
				"namespace":  pod.Namespace,
				"podName":    pod.Name,
				"cmName":     volume.ConfigMap.Name,
				"volumeName": volume.Name,
			}
			if _, err := tx.Run(ctx, query, params); err != nil {
				g.logger.Warn("Failed to create pod->configmap volume relationship",
					zap.String("pod", pod.Name),
					zap.String("configmap", volume.ConfigMap.Name),
					zap.Error(err))
			} else {
				relationshipsCreated++
			}
		}
	}

	// Check env from configmap
	for _, container := range pod.Spec.Containers {
		for _, envFrom := range container.EnvFrom {
			if envFrom.ConfigMapRef != nil {
				query := `
					MATCH (p:Pod {namespace: $namespace, name: $podName})
					MATCH (cm:ConfigMap {namespace: $namespace, name: $cmName})
					MERGE (p)-[r:MOUNTS {container: $container, mountType: 'envFrom'}]->(cm)
				`
				params := map[string]interface{}{
					"namespace": pod.Namespace,
					"podName":   pod.Name,
					"cmName":    envFrom.ConfigMapRef.Name,
					"container": container.Name,
				}
				if _, err := tx.Run(ctx, query, params); err != nil {
					g.logger.Warn("Failed to create pod->configmap env relationship",
						zap.String("pod", pod.Name),
						zap.String("configmap", envFrom.ConfigMapRef.Name),
						zap.Error(err))
				} else {
					relationshipsCreated++
				}
			}
		}

		// Check individual env vars from configmap
		for _, env := range container.Env {
			if env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil {
				query := `
					MATCH (p:Pod {namespace: $namespace, name: $podName})
					MATCH (cm:ConfigMap {namespace: $namespace, name: $cmName})
					MERGE (p)-[r:MOUNTS {container: $container, key: $key, mountType: 'env', envVar: $envVar}]->(cm)
				`
				params := map[string]interface{}{
					"namespace": pod.Namespace,
					"podName":   pod.Name,
					"cmName":    env.ValueFrom.ConfigMapKeyRef.Name,
					"container": container.Name,
					"key":       env.ValueFrom.ConfigMapKeyRef.Key,
					"envVar":    env.Name,
				}
				if _, err := tx.Run(ctx, query, params); err != nil {
					g.logger.Warn("Failed to create pod->configmap env var relationship",
						zap.String("pod", pod.Name),
						zap.String("configmap", env.ValueFrom.ConfigMapKeyRef.Name),
						zap.Error(err))
				} else {
					relationshipsCreated++
				}
			}
		}
	}

	if relationshipsCreated > 0 {
		g.instrumentation.ConfigMapMounts.Add(ctx, relationshipsCreated)
	}

	return nil
}

// updatePodSecretRelationships updates USES_SECRET relationships between Pod and Secrets
func (g *K8sGrapher) updatePodSecretRelationships(ctx context.Context, tx neo4j.ManagedTransaction, pod *corev1.Pod) error {
	// First, remove existing relationships
	deleteQuery := `
		MATCH (p:Pod {namespace: $namespace, name: $name})-[r:USES_SECRET]->(:Secret)
		DELETE r
	`
	if _, err := tx.Run(ctx, deleteQuery, map[string]interface{}{
		"namespace": pod.Namespace,
		"name":      pod.Name,
	}); err != nil {
		return fmt.Errorf("failed to delete old secret relationships: %w", err)
	}

	relationshipsCreated := int64(0)

	// Check volumes
	for _, volume := range pod.Spec.Volumes {
		if volume.Secret != nil {
			query := `
				MATCH (p:Pod {namespace: $namespace, name: $podName})
				MATCH (s:Secret {namespace: $namespace, name: $secretName})
				MERGE (p)-[r:USES_SECRET {volumeName: $volumeName, mountType: 'volume'}]->(s)
			`
			params := map[string]interface{}{
				"namespace":  pod.Namespace,
				"podName":    pod.Name,
				"secretName": volume.Secret.SecretName,
				"volumeName": volume.Name,
			}
			if _, err := tx.Run(ctx, query, params); err != nil {
				g.logger.Warn("Failed to create pod->secret volume relationship",
					zap.String("pod", pod.Name),
					zap.String("secret", volume.Secret.SecretName),
					zap.Error(err))
			} else {
				relationshipsCreated++
			}
		}
	}

	// Check imagePullSecrets
	for _, ips := range pod.Spec.ImagePullSecrets {
		query := `
			MATCH (p:Pod {namespace: $namespace, name: $podName})
			MATCH (s:Secret {namespace: $namespace, name: $secretName})
			MERGE (p)-[r:USES_SECRET {mountType: 'imagePull'}]->(s)
		`
		params := map[string]interface{}{
			"namespace":  pod.Namespace,
			"podName":    pod.Name,
			"secretName": ips.Name,
		}
		if _, err := tx.Run(ctx, query, params); err != nil {
			g.logger.Warn("Failed to create pod->secret imagePull relationship",
				zap.String("pod", pod.Name),
				zap.String("secret", ips.Name),
				zap.Error(err))
		} else {
			relationshipsCreated++
		}
	}

	// Check env from secrets
	for _, container := range pod.Spec.Containers {
		for _, envFrom := range container.EnvFrom {
			if envFrom.SecretRef != nil {
				query := `
					MATCH (p:Pod {namespace: $namespace, name: $podName})
					MATCH (s:Secret {namespace: $namespace, name: $secretName})
					MERGE (p)-[r:USES_SECRET {container: $container, mountType: 'envFrom'}]->(s)
				`
				params := map[string]interface{}{
					"namespace":  pod.Namespace,
					"podName":    pod.Name,
					"secretName": envFrom.SecretRef.Name,
					"container":  container.Name,
				}
				if _, err := tx.Run(ctx, query, params); err != nil {
					g.logger.Warn("Failed to create pod->secret env relationship",
						zap.String("pod", pod.Name),
						zap.String("secret", envFrom.SecretRef.Name),
						zap.Error(err))
				} else {
					relationshipsCreated++
				}
			}
		}

		// Check individual env vars from secrets
		for _, env := range container.Env {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
				query := `
					MATCH (p:Pod {namespace: $namespace, name: $podName})
					MATCH (s:Secret {namespace: $namespace, name: $secretName})
					MERGE (p)-[r:USES_SECRET {container: $container, key: $key, mountType: 'env', envVar: $envVar}]->(s)
				`
				params := map[string]interface{}{
					"namespace":  pod.Namespace,
					"podName":    pod.Name,
					"secretName": env.ValueFrom.SecretKeyRef.Name,
					"container":  container.Name,
					"key":        env.ValueFrom.SecretKeyRef.Key,
					"envVar":     env.Name,
				}
				if _, err := tx.Run(ctx, query, params); err != nil {
					g.logger.Warn("Failed to create pod->secret env var relationship",
						zap.String("pod", pod.Name),
						zap.String("secret", env.ValueFrom.SecretKeyRef.Name),
						zap.Error(err))
				} else {
					relationshipsCreated++
				}
			}
		}
	}

	if relationshipsCreated > 0 {
		g.instrumentation.SecretReferences.Add(ctx, relationshipsCreated)
	}

	return nil
}

// updatePodPVCRelationships updates CLAIMS relationships between Pod and PVCs
func (g *K8sGrapher) updatePodPVCRelationships(ctx context.Context, tx neo4j.ManagedTransaction, pod *corev1.Pod) error {
	// First, remove existing relationships
	deleteQuery := `
		MATCH (p:Pod {namespace: $namespace, name: $name})-[r:CLAIMS]->(:PVC)
		DELETE r
	`
	if _, err := tx.Run(ctx, deleteQuery, map[string]interface{}{
		"namespace": pod.Namespace,
		"name":      pod.Name,
	}); err != nil {
		return fmt.Errorf("failed to delete old PVC relationships: %w", err)
	}

	relationshipsCreated := int64(0)

	// Check volumes for PVC
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			query := `
				MATCH (p:Pod {namespace: $namespace, name: $podName})
				MATCH (pvc:PVC {namespace: $namespace, name: $pvcName})
				MERGE (p)-[r:CLAIMS {volumeName: $volumeName}]->(pvc)
			`
			params := map[string]interface{}{
				"namespace":  pod.Namespace,
				"podName":    pod.Name,
				"pvcName":    volume.PersistentVolumeClaim.ClaimName,
				"volumeName": volume.Name,
			}
			if _, err := tx.Run(ctx, query, params); err != nil {
				g.logger.Warn("Failed to create pod->pvc relationship",
					zap.String("pod", pod.Name),
					zap.String("pvc", volume.PersistentVolumeClaim.ClaimName),
					zap.Error(err))
			} else {
				relationshipsCreated++
			}
		}
	}

	if relationshipsCreated > 0 {
		g.instrumentation.PVCBindings.Add(ctx, relationshipsCreated)
	}

	return nil
}

// updatePodOwnershipRelationships updates ownership chain from Pod to ReplicaSet/Deployment
func (g *K8sGrapher) updatePodOwnershipRelationships(ctx context.Context, tx neo4j.ManagedTransaction, pod *corev1.Pod) error {
	relationshipsCreated := int64(0)

	for _, owner := range pod.OwnerReferences {
		var query string
		params := map[string]interface{}{
			"namespace": pod.Namespace,
			"podName":   pod.Name,
			"ownerName": owner.Name,
		}

		switch owner.Kind {
		case "ReplicaSet":
			query = `
				MATCH (rs:ReplicaSet {namespace: $namespace, name: $ownerName})
				MATCH (p:Pod {namespace: $namespace, name: $podName})
				MERGE (rs)-[:OWNS]->(p)
			`
		case "Deployment":
			// Direct ownership (rare but possible)
			query = `
				MATCH (d:Deployment {namespace: $namespace, name: $ownerName})
				MATCH (p:Pod {namespace: $namespace, name: $podName})
				MERGE (d)-[:OWNS]->(p)
			`
		case "DaemonSet":
			query = `
				MATCH (ds:DaemonSet {namespace: $namespace, name: $ownerName})
				MATCH (p:Pod {namespace: $namespace, name: $podName})
				MERGE (ds)-[:OWNS]->(p)
			`
		case "StatefulSet":
			query = `
				MATCH (ss:StatefulSet {namespace: $namespace, name: $ownerName})
				MATCH (p:Pod {namespace: $namespace, name: $podName})
				MERGE (ss)-[:OWNS]->(p)
			`
		case "Job":
			query = `
				MATCH (j:Job {namespace: $namespace, name: $ownerName})
				MATCH (p:Pod {namespace: $namespace, name: $podName})
				MERGE (j)-[:OWNS]->(p)
			`
		default:
			continue
		}

		if _, err := tx.Run(ctx, query, params); err != nil {
			g.logger.Warn("Failed to create ownership relationship",
				zap.String("pod", pod.Name),
				zap.String("owner", owner.Name),
				zap.String("kind", owner.Kind),
				zap.Error(err))
		} else {
			relationshipsCreated++
		}
	}

	if relationshipsCreated > 0 {
		g.instrumentation.OwnershipChains.Add(ctx, relationshipsCreated)
	}

	return nil
}
