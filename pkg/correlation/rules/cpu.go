package rules

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
)

// CPUThrottleDetection detects CPU throttling affecting application performance
func CPUThrottleDetection() *correlation.Rule {
	return &correlation.Rule{
		ID:          "cpu-throttle-detection",
		Name:        "CPU Throttling Detection",
		Description: "Detects when CPU throttling is affecting application performance",
		Category:    correlation.CategoryPerformance,
		Version:     "1.0.0",
		Author:      "Tapio Team",
		Tags:        []string{"cpu", "throttle", "performance"},

		MinConfidence: 0.6,
		Cooldown:      5 * time.Minute,
		TTL:           30 * time.Minute,

		RequiredSources: []correlation.EventSource{
			correlation.SourceEBPF,
		},
		OptionalSources: []correlation.EventSource{
			correlation.SourceMetrics,
			correlation.SourceKubernetes,
		},

		Evaluate: func(ctx *correlation.Context) *correlation.Result {
			// Get CPU throttling events from eBPF
			throttleEvents := ctx.GetEvents(correlation.Filter{
				Source:       correlation.SourceEBPF,
				Type:         "cpu_throttle",
				AttributeHas: "throttle_ratio",
			})

			if len(throttleEvents) == 0 {
				return nil // No throttling events
			}

			// Calculate average throttle ratio
			totalThrottleRatio := 0.0
			for _, event := range throttleEvents {
				if ratio, ok := event.Attributes["throttle_ratio"].(float64); ok {
					totalThrottleRatio += ratio
				}
			}
			avgThrottleRatio := totalThrottleRatio / float64(len(throttleEvents))

			// Only correlate if throttling is significant
			if avgThrottleRatio < 0.1 { // Less than 10% throttling
				return nil
			}

			// Get CPU usage metrics if available
			cpuUsage := ctx.GetMetricValue("container.cpu.usage_percent")

			// Get application performance events (timeouts, slow responses)
			perfEvents := ctx.GetEvents(correlation.Filter{
				Source: correlation.SourceKubernetes,
				Type:   "event",
				AttributeEq: map[string]string{
					"reason": "Timeout",
				},
			})

			// Also look for application timeout events
			timeoutEvents := ctx.GetEvents(correlation.Filter{
				Source: correlation.SourceEBPF,
				Type:   "syscall_timeout",
			})

			perfEvents = append(perfEvents, timeoutEvents...)

			// Check for correlated performance degradation
			correlatedPerfEvents := make([]correlation.Event, 0)
			for _, perfEvent := range perfEvents {
				// Check if performance event is temporally related to throttling
				for _, throttleEvent := range throttleEvents {
					timeDiff := perfEvent.Timestamp.Sub(throttleEvent.Timestamp).Abs()
					if timeDiff < 2*time.Minute && ctx.EntitiesRelated(perfEvent.Entity, throttleEvent.Entity) {
						correlatedPerfEvents = append(correlatedPerfEvents, perfEvent)
						break
					}
				}
			}

			// Calculate confidence
			confidence := 0.4 // Base confidence

			// Higher throttle ratio = higher confidence
			if avgThrottleRatio > 0.5 {
				confidence += 0.3
			} else if avgThrottleRatio > 0.3 {
				confidence += 0.2
			} else if avgThrottleRatio > 0.1 {
				confidence += 0.1
			}

			// More throttle events = higher confidence
			if len(throttleEvents) > 10 {
				confidence += 0.2
			} else if len(throttleEvents) > 5 {
				confidence += 0.1
			}

			// Correlated performance events increase confidence
			if len(correlatedPerfEvents) > 0 {
				confidence += 0.2
			}

			// High CPU usage with throttling is strong indicator
			if cpuUsage > 0.9 {
				confidence += 0.1
			}

			// Determine severity
			severity := correlation.SeverityLow
			if avgThrottleRatio > 0.7 || len(correlatedPerfEvents) > 5 {
				severity = correlation.SeverityCritical
			} else if avgThrottleRatio > 0.5 || len(correlatedPerfEvents) > 2 {
				severity = correlation.SeverityHigh
			} else if avgThrottleRatio > 0.3 || len(correlatedPerfEvents) > 0 {
				severity = correlation.SeverityMedium
			}

			// Build evidence
			evidence := correlation.Evidence{
				Events: append(throttleEvents, correlatedPerfEvents...),
				Metrics: map[string]float64{
					"avg_throttle_ratio":   avgThrottleRatio,
					"throttle_event_count": float64(len(throttleEvents)),
					"perf_impact_events":   float64(len(correlatedPerfEvents)),
					"cpu_usage_percent":    cpuUsage,
				},
				Patterns: []string{
					fmt.Sprintf("CPU throttling: %.1f%% average", avgThrottleRatio*100),
					fmt.Sprintf("Performance impact: %d events", len(correlatedPerfEvents)),
				},
			}

			// Calculate performance impact
			impactDuration := ctx.Window.Duration()
			if len(throttleEvents) > 1 {
				firstThrottle := throttleEvents[0].Timestamp
				lastThrottle := throttleEvents[len(throttleEvents)-1].Timestamp
				impactDuration = lastThrottle.Sub(firstThrottle)
			}

			// Generate recommendations
			recommendations := []string{
				"Increase CPU limits for the affected container",
				"Review CPU requests to ensure adequate allocation",
			}

			if avgThrottleRatio > 0.5 {
				recommendations = append(recommendations, "Consider immediate CPU limit increase - severe throttling detected")
			}

			if len(correlatedPerfEvents) > 0 {
				recommendations = append(recommendations, "Application performance is being impacted - urgent action needed")
			}

			if cpuUsage > 0.9 {
				recommendations = append(recommendations, "Consider horizontal scaling to distribute CPU load")
			}

			return &correlation.Result{
				Confidence: confidence,
				Severity:   severity,
				Title:      fmt.Sprintf("CPU throttling detected (%.1f%% average)", avgThrottleRatio*100),
				Description: fmt.Sprintf(
					"Container is experiencing significant CPU throttling with an average ratio of %.1f%%. "+
						"Detected %d throttling events over %v, with %d correlated performance impact events.",
					avgThrottleRatio*100,
					len(throttleEvents),
					impactDuration,
					len(correlatedPerfEvents),
				),
				Impact: fmt.Sprintf(
					"Application performance degraded for %v due to CPU resource constraints",
					impactDuration,
				),
				Evidence:        evidence,
				Recommendations: recommendations,
				Actions: []correlation.Action{
					{
						Type:     "alert",
						Target:   "platform-team",
						Priority: string(severity),
						Parameters: map[string]string{
							"throttle_ratio": fmt.Sprintf("%.2f", avgThrottleRatio),
							"event_count":    fmt.Sprintf("%d", len(throttleEvents)),
						},
					},
				},
			}
		},
	}
}

// CPUContentionDetection detects CPU contention between containers
func CPUContentionDetection() *correlation.Rule {
	return &correlation.Rule{
		ID:          "cpu-contention-detection",
		Name:        "CPU Contention Detection",
		Description: "Detects CPU contention between multiple containers on the same node",
		Category:    correlation.CategoryResource,
		Version:     "1.0.0",
		Author:      "Tapio Team",
		Tags:        []string{"cpu", "contention", "resource", "node"},

		MinConfidence: 0.7,
		Cooldown:      10 * time.Minute,
		TTL:           1 * time.Hour,

		RequiredSources: []correlation.EventSource{
			correlation.SourceEBPF,
			correlation.SourceMetrics,
		},

		Evaluate: func(ctx *correlation.Context) *correlation.Result {
			// Get CPU throttling events
			throttleEvents := ctx.GetEvents(correlation.Filter{
				Source: correlation.SourceEBPF,
				Type:   "cpu_throttle",
			})

			if len(throttleEvents) < 2 {
				return nil // Need multiple throttling events for contention
			}

			// Group throttling events by node
			nodeThrottleMap := make(map[string][]correlation.Event)
			for _, event := range throttleEvents {
				node := event.Entity.Node
				if node != "" {
					nodeThrottleMap[node] = append(nodeThrottleMap[node], event)
				}
			}

			// Look for nodes with multiple throttled containers
			contentionNode := ""
			maxThrottledContainers := 0

			for node, events := range nodeThrottleMap {
				// Count unique containers experiencing throttling
				containerSet := make(map[string]bool)
				for _, event := range events {
					if event.Entity.Container != "" {
						containerSet[event.Entity.Container] = true
					}
				}

				if len(containerSet) > maxThrottledContainers {
					maxThrottledContainers = len(containerSet)
					contentionNode = node
				}
			}

			// Need at least 3 containers throttled on same node for contention
			if maxThrottledContainers < 3 {
				return nil
			}

			contentionEvents := nodeThrottleMap[contentionNode]

			// Get node CPU usage metrics
			nodeCPUUsage := ctx.GetMetricValue("node.cpu.usage_percent")

			// Check if node CPU is near capacity
			if nodeCPUUsage < 0.8 { // Less than 80% - probably not contention
				return nil
			}

			// Calculate contention severity
			confidence := 0.5

			// More throttled containers = higher confidence
			if maxThrottledContainers >= 5 {
				confidence += 0.3
			} else if maxThrottledContainers >= 4 {
				confidence += 0.2
			} else {
				confidence += 0.1
			}

			// High node CPU usage increases confidence
			if nodeCPUUsage > 0.95 {
				confidence += 0.2
			} else if nodeCPUUsage > 0.9 {
				confidence += 0.1
			}

			// Many throttling events increases confidence
			if len(contentionEvents) > 20 {
				confidence += 0.1
			}

			// Check for temporal clustering of throttling events
			if ctx.EventsInSequence(contentionEvents, 30*time.Second) {
				confidence += 0.1
			}

			// Determine severity based on impact
			severity := correlation.SeverityMedium
			if maxThrottledContainers >= 6 || nodeCPUUsage > 0.98 {
				severity = correlation.SeverityCritical
			} else if maxThrottledContainers >= 4 || nodeCPUUsage > 0.95 {
				severity = correlation.SeverityHigh
			}

			// Get unique containers and pods affected
			affectedContainers := make(map[string]bool)
			affectedPods := make(map[string]bool)

			for _, event := range contentionEvents {
				if event.Entity.Container != "" {
					affectedContainers[event.Entity.Container] = true
				}
				if event.Entity.Pod != "" {
					key := event.Entity.Namespace + "/" + event.Entity.Pod
					affectedPods[key] = true
				}
			}

			// Build evidence
			evidence := correlation.Evidence{
				Events: contentionEvents,
				Metrics: map[string]float64{
					"node_cpu_usage":       nodeCPUUsage,
					"throttled_containers": float64(maxThrottledContainers),
					"throttle_events":      float64(len(contentionEvents)),
					"affected_pods":        float64(len(affectedPods)),
				},
				Patterns: []string{
					fmt.Sprintf("CPU contention on node: %s", contentionNode),
					fmt.Sprintf("Throttled containers: %d", maxThrottledContainers),
					fmt.Sprintf("Node CPU usage: %.1f%%", nodeCPUUsage*100),
				},
				Entities: []correlation.Entity{
					{
						Type: "node",
						Name: contentionNode,
					},
				},
			}

			// Generate recommendations
			recommendations := []string{
				fmt.Sprintf("Redistribute workloads from node %s", contentionNode),
				"Review CPU requests and limits for affected containers",
				"Consider adding more nodes to the cluster",
			}

			if nodeCPUUsage > 0.95 {
				recommendations = append(recommendations, "Immediate action required - node CPU critically overloaded")
			}

			if maxThrottledContainers >= 5 {
				recommendations = append(recommendations, "Multiple workloads affected - consider emergency scaling")
			}

			return &correlation.Result{
				Confidence: confidence,
				Severity:   severity,
				Title:      fmt.Sprintf("CPU contention detected on node %s", contentionNode),
				Description: fmt.Sprintf(
					"Node %s is experiencing CPU contention with %d containers being throttled. "+
						"Node CPU usage is at %.1f%%, affecting %d pods.",
					contentionNode,
					maxThrottledContainers,
					nodeCPUUsage*100,
					len(affectedPods),
				),
				Impact: fmt.Sprintf(
					"Performance degradation for %d containers on node %s",
					maxThrottledContainers,
					contentionNode,
				),
				Evidence:        evidence,
				Recommendations: recommendations,
				Actions: []correlation.Action{
					{
						Type:     "alert",
						Target:   "ops-team",
						Priority: string(severity),
						Parameters: map[string]string{
							"node":                 contentionNode,
							"throttled_containers": fmt.Sprintf("%d", maxThrottledContainers),
							"cpu_usage":            fmt.Sprintf("%.1f", nodeCPUUsage*100),
						},
					},
					{
						Type:   "webhook",
						Target: "node-balancer",
						Parameters: map[string]string{
							"node":   contentionNode,
							"action": "rebalance",
							"reason": "cpu_contention",
						},
						Condition: "auto_rebalancing_enabled",
					},
				},
			}
		},
	}
}

// HighCPUUtilizationPattern detects sustained high CPU utilization patterns
func HighCPUUtilizationPattern() *correlation.Rule {
	return &correlation.Rule{
		ID:          "high-cpu-utilization-pattern",
		Name:        "High CPU Utilization Pattern",
		Description: "Detects sustained high CPU utilization that may lead to performance issues",
		Category:    correlation.CategoryPerformance,
		Version:     "1.0.0",
		Author:      "Tapio Team",
		Tags:        []string{"cpu", "utilization", "performance", "pattern"},

		MinConfidence: 0.6,
		Cooldown:      15 * time.Minute,
		TTL:           2 * time.Hour,

		RequiredSources: []correlation.EventSource{
			correlation.SourceMetrics,
		},
		OptionalSources: []correlation.EventSource{
			correlation.SourceEBPF,
		},

		Evaluate: func(ctx *correlation.Context) *correlation.Result {
			// Get CPU usage metrics
			cpuUsage := ctx.GetMetric("container.cpu.usage_percent")
			if len(cpuUsage.Points) < 10 {
				return nil // Need enough data points
			}

			// Calculate statistics
			mean, _ := cpuUsage.Statistics()

			// Check if average CPU usage is high
			if mean < 0.8 { // Less than 80% average
				return nil
			}

			// Count points above threshold
			highUsageCount := 0
			sustainedHighUsage := 0
			maxUsage := 0.0

			for i, point := range cpuUsage.Points {
				if point.Value > maxUsage {
					maxUsage = point.Value
				}

				if point.Value > 0.9 { // 90% threshold
					highUsageCount++

					// Check for sustained high usage (consecutive points)
					if i > 0 && cpuUsage.Points[i-1].Value > 0.9 {
						sustainedHighUsage++
					}
				}
			}

			highUsageRatio := float64(highUsageCount) / float64(len(cpuUsage.Points))

			// Need significant high usage for correlation
			if highUsageRatio < 0.3 { // Less than 30% of time
				return nil
			}

			// Check for CPU throttling events during high usage periods
			throttleEvents := ctx.GetEvents(correlation.Filter{
				Source: correlation.SourceEBPF,
				Type:   "cpu_throttle",
			})

			// Analyze trend to see if usage is increasing
			trend := ctx.AnalyzeTrend(cpuUsage)

			// Calculate confidence
			confidence := 0.4

			// High average usage increases confidence
			if mean > 0.95 {
				confidence += 0.2
			} else if mean > 0.9 {
				confidence += 0.15
			} else if mean > 0.85 {
				confidence += 0.1
			}

			// High usage ratio increases confidence
			if highUsageRatio > 0.8 {
				confidence += 0.2
			} else if highUsageRatio > 0.6 {
				confidence += 0.15
			} else if highUsageRatio > 0.4 {
				confidence += 0.1
			}

			// Sustained high usage increases confidence
			if sustainedHighUsage > 10 {
				confidence += 0.15
			} else if sustainedHighUsage > 5 {
				confidence += 0.1
			}

			// Throttling events during high usage confirm the issue
			if len(throttleEvents) > 0 {
				confidence += 0.1
			}

			// Increasing trend is concerning
			if trend.IsIncreasing() && trend.IsStrong() {
				confidence += 0.1
			}

			// Determine severity
			severity := correlation.SeverityMedium
			if mean > 0.98 || maxUsage > 0.99 {
				severity = correlation.SeverityCritical
			} else if mean > 0.95 || maxUsage > 0.98 {
				severity = correlation.SeverityHigh
			}

			// Calculate duration of high usage
			highUsageDuration := time.Duration(highUsageCount) *
				time.Duration(ctx.Window.Duration().Nanoseconds()/int64(len(cpuUsage.Points)))

			// Generate recommendations
			recommendations := []string{
				"Review application CPU usage patterns",
				"Consider increasing CPU limits",
				"Analyze workload efficiency",
			}

			if trend.IsIncreasing() {
				recommendations = append(recommendations, "CPU usage is trending upward - investigate cause")
			}

			if len(throttleEvents) > 0 {
				recommendations = append(recommendations, "CPU throttling detected - immediate limit increase recommended")
			}

			if mean > 0.95 {
				recommendations = append(recommendations, "Critical CPU usage - consider horizontal scaling")
			}

			return &correlation.Result{
				Confidence: confidence,
				Severity:   severity,
				Title:      fmt.Sprintf("High CPU utilization pattern detected (%.1f%% average)", mean*100),
				Description: fmt.Sprintf(
					"Container has sustained high CPU usage with %.1f%% average utilization. "+
						"CPU usage exceeded 90%% for %.1f%% of the monitoring period (%v total). "+
						"Maximum usage: %.1f%%",
					mean*100,
					highUsageRatio*100,
					highUsageDuration.Round(time.Second),
					maxUsage*100,
				),
				Impact: fmt.Sprintf(
					"Potential performance degradation due to high CPU utilization for %v",
					highUsageDuration.Round(time.Second),
				),
				Evidence: correlation.Evidence{
					Events: throttleEvents,
					Metrics: map[string]float64{
						"mean_cpu_usage":       mean,
						"max_cpu_usage":        maxUsage,
						"high_usage_ratio":     highUsageRatio,
						"sustained_high_count": float64(sustainedHighUsage),
						"throttle_events":      float64(len(throttleEvents)),
						"trend_slope":          trend.Slope,
					},
					Patterns: []string{
						fmt.Sprintf("CPU usage pattern: %.1f%% average, %.1f%% peak", mean*100, maxUsage*100),
						fmt.Sprintf("High usage duration: %v", highUsageDuration.Round(time.Second)),
						fmt.Sprintf("Trend: %s (%s)", trend.Direction, trend.Strength),
					},
				},
				Recommendations: recommendations,
				Actions: []correlation.Action{
					{
						Type:     "alert",
						Target:   "dev-team",
						Priority: string(severity),
						Parameters: map[string]string{
							"mean_usage": fmt.Sprintf("%.1f", mean*100),
							"max_usage":  fmt.Sprintf("%.1f", maxUsage*100),
							"duration":   highUsageDuration.String(),
						},
					},
				},
			}
		},
	}
}
