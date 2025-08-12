package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence"
	"go.uber.org/zap"
)

func main() {
	// Create logger
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	fmt.Println("🧠 Tapio Intelligence Engine Test Drive")
	fmt.Println("=====================================")

	// Configure intelligence service
	config := intelligence.Config{
		Neo4jURI:      "bolt://localhost:7687",
		Neo4jUsername: "neo4j",
		Neo4jPassword: "password",
		Neo4jDatabase: "neo4j",
	}

	// Create service
	fmt.Println("\n1️⃣ Connecting to Neo4j...")
	service, err := intelligence.NewService(config, logger)
	if err != nil {
		log.Fatalf("Failed to create intelligence service: %v", err)
	}
	defer service.Close(context.Background())

	ctx := context.Background()
	fmt.Println("✅ Connected and indexes created!")

	// Scenario 1: OOM Kill cascade
	fmt.Println("\n2️⃣ Scenario: Memory pressure causing OOM kills")
	fmt.Println("------------------------------------------------")

	// Create a deployment
	deploymentEvent := createEvent("deployment_created", "deployment", "web-app", map[string]interface{}{
		"replicas": 3,
	})
	service.ProcessEvent(ctx, deploymentEvent)
	fmt.Println("📦 Created deployment: web-app")

	// Create pods
	for i := 1; i <= 3; i++ {
		podEvent := createPodEvent(fmt.Sprintf("web-app-pod-%d", i), "web-app-rs", map[string]string{
			"app": "web-app",
		})
		service.ProcessEvent(ctx, podEvent)
		fmt.Println(fmt.Sprintf("🟢 Created pod: web-app-pod-%d", i))
	}

	// Create service
	serviceEvent := createServiceEvent("web-app-svc", map[string]string{"app": "web-app"})
	service.ProcessEvent(ctx, serviceEvent)
	fmt.Println("🔌 Created service: web-app-svc")

	// Simulate OOM Kill
	time.Sleep(100 * time.Millisecond)
	oomEvent := &domain.UnifiedEvent{
		ID:        domain.GenerateEventID(),
		Timestamp: time.Now(),
		Type:      "pod_oom_killed",
		Source:    "kubelet",
		Message:   "OOMKilled",
		Severity:  domain.EventSeverityCritical,
		Entity: &domain.EntityContext{
			Type:      "pod",
			Name:      "web-app-pod-1",
			Namespace: "default",
			UID:       "pod-uid-1",
		},
		K8sContext: &domain.K8sContext{
			Name:      "web-app-pod-1",
			Namespace: "default",
			Kind:      "Pod",
			OwnerReferences: []domain.OwnerReference{{
				Kind: "ReplicaSet",
				Name: "web-app-rs",
				UID:  "rs-uid",
			}},
		},
	}

	fmt.Println("\n💥 OOM Kill event for web-app-pod-1")
	err = service.ProcessEvent(ctx, oomEvent)
	if err != nil {
		log.Printf("Error processing OOM event: %v", err)
	}

	// Query for root cause
	time.Sleep(100 * time.Millisecond)
	fmt.Println("\n🔍 Querying: Why did web-app-pod-1 fail?")
	analysis, err := service.WhyDidThisFail(ctx, "pod", "default", "web-app-pod-1")
	if err != nil {
		fmt.Printf("❌ Query failed: %v\n", err)
	} else if analysis != nil {
		fmt.Println("📊 Root Cause Analysis:")
		fmt.Printf("   Failed Entity: %s/%s\n", analysis.FailedEntity.Namespace, analysis.FailedEntity.Name)
		if len(analysis.RootCauses) > 0 {
			fmt.Println("   Root Causes:")
			for _, cause := range analysis.RootCauses {
				fmt.Printf("   - %s: %s\n", cause.Type, cause.Message)
			}
		}
	}

	// Scenario 2: ConfigMap change cascade
	fmt.Println("\n3️⃣ Scenario: ConfigMap change triggering restarts")
	fmt.Println("------------------------------------------------")

	// Create ConfigMap
	cmEvent := createConfigMapEvent("app-config")
	service.ProcessEvent(ctx, cmEvent)
	fmt.Println("📄 Created ConfigMap: app-config")

	// Update ConfigMap
	time.Sleep(100 * time.Millisecond)
	cmUpdateEvent := &domain.UnifiedEvent{
		ID:        domain.GenerateEventID(),
		Timestamp: time.Now(),
		Type:      "modified",
		Source:    "kubeapi",
		Entity: &domain.EntityContext{
			Type:      "configmap",
			Name:      "app-config",
			Namespace: "default",
			UID:       "cm-uid",
		},
	}
	service.ProcessEvent(ctx, cmUpdateEvent)
	fmt.Println("🔄 Updated ConfigMap: app-config")

	// Simulate pod restarts
	for i := 1; i <= 3; i++ {
		restartEvent := createPodRestartEvent(fmt.Sprintf("web-app-pod-%d", i))
		service.ProcessEvent(ctx, restartEvent)
		fmt.Println(fmt.Sprintf("🔄 Pod restarted: web-app-pod-%d", i))
		time.Sleep(50 * time.Millisecond)
	}

	// Scenario 3: Service disruption
	fmt.Println("\n4️⃣ Scenario: Service disruption detection")
	fmt.Println("------------------------------------------------")

	// Check service impact
	fmt.Println("\n🔍 Querying: What does web-app-svc impact?")
	impact, err := service.WhatDoesThisImpact(ctx, "service", "default", "web-app-svc")
	if err != nil {
		fmt.Printf("❌ Query failed: %v\n", err)
	} else if impact != nil {
		fmt.Println("📊 Impact Analysis:")
		fmt.Printf("   Service: %s/%s\n", impact.Service.Namespace, impact.Service.Name)
		fmt.Printf("   Affected Pods: %d\n", len(impact.AffectedPods))
		for _, pod := range impact.AffectedPods {
			fmt.Printf("   - %s\n", pod.Name)
		}
	}

	// Scenario 4: Crash loop detection
	fmt.Println("\n5️⃣ Scenario: Crash loop backoff detection")
	fmt.Println("------------------------------------------------")

	// Simulate multiple restarts for crash loop
	crashPod := "crash-loop-pod"
	for i := 1; i <= 5; i++ {
		crashEvent := createPodRestartEvent(crashPod)
		service.ProcessEvent(ctx, crashEvent)
		fmt.Printf("💥 Restart #%d for %s\n", i, crashPod)
		time.Sleep(100 * time.Millisecond)
	}

	// Check for cascading failures
	fmt.Println("\n6️⃣ Checking for cascading failure patterns...")
	fmt.Println("------------------------------------------------")
	cascades, err := service.GetCascadingFailures(ctx, 5*time.Minute)
	if err != nil {
		fmt.Printf("❌ Query failed: %v\n", err)
	} else {
		fmt.Printf("📊 Found %d cascading failure patterns\n", len(cascades))
		for _, cascade := range cascades {
			fmt.Printf("   - Pattern: %s\n", cascade.Pattern)
			fmt.Printf("     Severity: %s\n", cascade.Severity)
		}
	}

	fmt.Println("\n✅ Test drive complete!")
	fmt.Println("\n💡 The intelligence engine can:")
	fmt.Println("   - Track K8s resource relationships")
	fmt.Println("   - Detect failure patterns in real-time")
	fmt.Println("   - Answer 'why' questions about failures")
	fmt.Println("   - Identify service impacts")
	fmt.Println("   - Find cascading failures")
}

// Helper functions to create events

func createEvent(eventType, entityType, name string, metadata map[string]interface{}) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        domain.GenerateEventID(),
		Timestamp: time.Now(),
		Type:      domain.EventType(eventType),
		Source:    "test-driver",
		Entity: &domain.EntityContext{
			Type:      entityType,
			Name:      name,
			Namespace: "default",
			UID:       fmt.Sprintf("%s-uid", name),
		},
		Attributes: metadata,
	}
}

func createPodEvent(name, owner string, labels map[string]string) *domain.UnifiedEvent {
	event := createEvent("pod_created", "pod", name, nil)
	event.Entity.Labels = labels
	event.K8sContext = &domain.K8sContext{
		Name:      name,
		Namespace: "default",
		Kind:      "Pod",
		Labels:    labels,
		OwnerReferences: []domain.OwnerReference{{
			Kind: "ReplicaSet",
			Name: owner,
			UID:  fmt.Sprintf("%s-uid", owner),
		}},
	}
	return event
}

func createServiceEvent(name string, selector map[string]string) *domain.UnifiedEvent {
	event := createEvent("service_created", "service", name, nil)
	event.K8sContext = &domain.K8sContext{
		Name:      name,
		Namespace: "default",
		Kind:      "Service",
		Selectors: selector,
	}
	return event
}

func createConfigMapEvent(name string) *domain.UnifiedEvent {
	return createEvent("configmap_created", "configmap", name, nil)
}

func createPodRestartEvent(podName string) *domain.UnifiedEvent {
	event := createEvent("pod_restarted", "pod", podName, nil)
	event.Entity.Type = "pod"
	event.Type = "pod_restarted"
	return event
}
