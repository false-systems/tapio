package rules

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
)

// MemoryPressureCascade detects when memory pressure causes cascading failures
func MemoryPressureCascade() *correlation.Rule {
	return &correlation.Rule{
		ID:          "memory-pressure-cascade",
		Name:        "Memory Pressure Cascade Detection",
		Description: "Detects when OOM kills lead to pod restarts and service failures",
		Category:    correlation.CategoryResource,
		Version:     "1.0.0",
		Author:      "Tapio Team",
		Tags:        []string{"memory", "oom", "cascade", "reliability"},

		MinConfidence: 0.7,
		Cooldown:      5 * time.Minute,
		TTL:           1 * time.Hour,

		RequiredSources: []correlation.EventSource{
			correlation.SourceEBPF,
			correlation.SourceKubernetes,
		},
		OptionalSources: []correlation.EventSource{
			correlation.SourceSystemd,
		},

		Evaluate: func(ctx *correlation.Context) *correlation.Result {
			// Get OOM kill events from eBPF
			oomEvents := ctx.GetEvents(correlation.Filter{
				Source: correlation.SourceEBPF,
				Type:   "oom_kill",
			})

			if len(oomEvents) == 0 {
				return nil // No OOM events, no correlation
			}

			// Get pod restart events from Kubernetes
			podRestarts := ctx.GetEvents(correlation.Filter{
				Source: correlation.SourceKubernetes,
				Type:   "pod.restart",
				Since:  ctx.Window.Start,
			})

			if len(podRestarts) < 2 {
				return nil // Need at least 2 restarts for cascade
			}

			// Check temporal relationship - restarts should follow OOM kills
			firstOOM := oomEvents[0]
			correlatedRestarts := make([]correlation.Event, 0)

			for _, restart := range podRestarts {
				timeDiff := restart.Timestamp.Sub(firstOOM.Timestamp)
				if timeDiff > 0 && timeDiff < 2*time.Minute {
					// Check if restart is related to OOM (same node or pod)
					if ctx.SameNode(firstOOM, restart) || ctx.SamePod(firstOOM, restart) {
						correlatedRestarts = append(correlatedRestarts, restart)
					}
				}
			}

			if len(correlatedRestarts) < 2 {
				return nil // Not enough correlated restarts
			}

			// Get optional systemd service failures
			serviceFailures := ctx.GetEvents(correlation.Filter{
				Source: correlation.SourceSystemd,
				Type:   "service.failed",
				Since:  firstOOM.Timestamp.Add(-30 * time.Second),
				Until:  firstOOM.Timestamp.Add(5 * time.Minute),
			})

			// Calculate confidence based on evidence strength
			confidence := 0.5 // Base confidence

			// More OOM events = higher confidence
			if len(oomEvents) > 1 {
				confidence += 0.1
			}

			// More restarts = higher confidence
			confidence += float64(len(correlatedRestarts)) * 0.05

			// Same node correlation increases confidence
			sameNodeCount := 0
			for _, restart := range correlatedRestarts {
				if ctx.SameNode(firstOOM, restart) {
					sameNodeCount++
				}
			}
			if sameNodeCount > 0 {
				confidence += 0.1
			}

			// Service failures add confidence
			if len(serviceFailures) > 0 {
				confidence += 0.1
			}

			// Timeline analysis - rapid succession increases confidence
			if len(correlatedRestarts) >= 3 {
				if ctx.EventsInSequence(correlatedRestarts, 30*time.Second) {
					confidence += 0.15
				}
			}

			// Cap confidence at 1.0
			if confidence > 1.0 {
				confidence = 1.0
			}

			// Determine severity based on impact
			severity := correlation.SeverityMedium
			affectedNodes := ctx.GetUniqueNodes(append(oomEvents, correlatedRestarts...))
			affectedPods := ctx.GetUniquePods(correlatedRestarts)

			if len(affectedNodes) > 2 || len(affectedPods) > 5 {
				severity = correlation.SeverityCritical
			} else if len(affectedNodes) > 1 || len(affectedPods) > 3 {
				severity = correlation.SeverityHigh
			}

			// Build timeline for evidence
			timeline := make([]correlation.TimelineEntry, 0)
			timeline = append(timeline, correlation.TimelineEntry{
				Timestamp:   firstOOM.Timestamp,
				Description: fmt.Sprintf("OOM kill on %s", firstOOM.Entity.Node),
				EventID:     firstOOM.ID,
				Source:      string(firstOOM.Source),
			})

			for _, restart := range correlatedRestarts {
				timeline = append(timeline, correlation.TimelineEntry{
					Timestamp:   restart.Timestamp,
					Description: fmt.Sprintf("Pod restart: %s", restart.Entity.Pod),
					EventID:     restart.ID,
					Source:      string(restart.Source),
				})
			}

			// Collect evidence
			evidence := correlation.Evidence{
				Events:   append(oomEvents, correlatedRestarts...),
				Timeline: timeline,
				Entities: []correlation.Entity{firstOOM.Entity},
				Metrics: map[string]float64{
					"oom_count":        float64(len(oomEvents)),
					"restart_count":    float64(len(correlatedRestarts)),
					"affected_nodes":   float64(len(affectedNodes)),
					"affected_pods":    float64(len(affectedPods)),
					"cascade_duration": ctx.Window.Duration().Seconds(),
				},
			}

			if len(serviceFailures) > 0 {
				evidence.Events = append(evidence.Events, serviceFailures...)
				evidence.Metrics["service_failures"] = float64(len(serviceFailures))
			}

			// Generate recommendations
			recommendations := []string{
				"Increase memory limits for affected pods",
				"Review memory usage patterns of applications",
				"Consider adding more nodes to distribute memory load",
			}

			if len(affectedNodes) > 1 {
				recommendations = append(recommendations, "Investigate node-level memory pressure")
			}

			if len(correlatedRestarts) > 5 {
				recommendations = append(recommendations, "Implement pod disruption budgets to limit blast radius")
			}

			return &correlation.Result{
				Confidence: confidence,
				Severity:   severity,
				Title:      fmt.Sprintf("Memory pressure cascade detected on %s", firstOOM.Entity.Node),
				Description: fmt.Sprintf(
					"Memory pressure caused %d OOM kills followed by %d pod restarts within %v. "+
						"This indicates insufficient memory resources and potential cascade failure.",
					len(oomEvents),
					len(correlatedRestarts),
					ctx.Window.Duration(),
				),
				Impact: fmt.Sprintf(
					"Affected %d nodes and %d pods. Cascade duration: %v",
					len(affectedNodes),
					len(affectedPods),
					ctx.Window.Duration(),
				),
				Evidence:        evidence,
				Recommendations: recommendations,
				Actions: []correlation.Action{
					{
						Type:     "alert",
						Target:   "platform-team",
						Priority: "high",
						Parameters: map[string]string{
							"node":          firstOOM.Entity.Node,
							"restart_count": fmt.Sprintf("%d", len(correlatedRestarts)),
						},
					},
					{
						Type:   "annotate",
						Target: firstOOM.Entity.Node,
						Parameters: map[string]string{
							"memory-pressure-detected": "true",
							"correlation-id":           ctx.CorrelationID,
						},
					},
				},
			}
		},
	}
}

// MemoryLeakDetection detects potential memory leaks in applications
func MemoryLeakDetection() *correlation.Rule {
	return &correlation.Rule{
		ID:          "memory-leak-detection",
		Name:        "Memory Leak Detection",
		Description: "Detects applications with steadily increasing memory usage",
		Category:    correlation.CategoryResource,
		Version:     "1.0.0",
		Author:      "Tapio Team",
		Tags:        []string{"memory", "leak", "performance"},

		MinConfidence: 0.6,
		Cooldown:      15 * time.Minute,
		TTL:           4 * time.Hour,

		RequiredSources: []correlation.EventSource{
			correlation.SourceMetrics,
		},
		OptionalSources: []correlation.EventSource{
			correlation.SourceKubernetes,
		},

		Evaluate: func(ctx *correlation.Context) *correlation.Result {
			// Get memory usage metrics
			memoryUsage := ctx.GetMetric("container.memory.usage")
			if len(memoryUsage.Points) < 10 {
				return nil // Need enough data points for trend analysis
			}

			// Analyze trend
			trend := ctx.AnalyzeTrend(memoryUsage)

			// Must be a strong increasing trend
			if !trend.IsIncreasing() || !trend.IsStrong() {
				return nil
			}

			// Get recent events to check for restarts
			recentRestarts := ctx.GetEvents(correlation.Filter{
				Source: correlation.SourceKubernetes,
				Type:   "pod.restart",
				Since:  ctx.Window.Start.Add(-1 * time.Hour), // Look back further
			})

			// Check if memory is approaching limits
			memoryLimit := ctx.GetMetricValue("container.memory.limit")
			currentUsage := ctx.GetMetricValue("container.memory.usage")

			usageRatio := 0.0
			if memoryLimit > 0 {
				usageRatio = currentUsage / memoryLimit
			}

			// Calculate confidence
			confidence := 0.4 // Base confidence

			// Strong trend increases confidence
			if trend.Correlation > 0.9 {
				confidence += 0.2
			} else if trend.Correlation > 0.8 {
				confidence += 0.1
			}

			// High usage ratio increases confidence
			if usageRatio > 0.9 {
				confidence += 0.2
			} else if usageRatio > 0.8 {
				confidence += 0.1
			}

			// Recent restarts may indicate OOM cycles
			if len(recentRestarts) > 0 {
				confidence += 0.1
			}

			// Time series length affects confidence
			if len(memoryUsage.Points) > 50 {
				confidence += 0.1
			}

			// Determine severity
			severity := correlation.SeverityLow
			if usageRatio > 0.95 {
				severity = correlation.SeverityCritical
			} else if usageRatio > 0.9 {
				severity = correlation.SeverityHigh
			} else if usageRatio > 0.8 {
				severity = correlation.SeverityMedium
			}

			// Estimate time to OOM if trend continues
			timeToOOM := "unknown"
			if memoryLimit > 0 && trend.Slope > 0 {
				remainingMemory := memoryLimit - currentUsage
				secondsToOOM := remainingMemory / trend.Slope
				if secondsToOOM > 0 && secondsToOOM < 24*3600 { // Less than 24 hours
					timeToOOM = time.Duration(secondsToOOM * float64(time.Second)).String()
				}
			}

			// Generate recommendations
			recommendations := []string{
				"Investigate application memory usage patterns",
				"Check for memory leaks in application code",
				"Review garbage collection configuration",
				"Consider increasing memory limits if justified",
			}

			if len(recentRestarts) > 0 {
				recommendations = append(recommendations, "Analyze restart patterns for OOM correlation")
			}

			if usageRatio > 0.9 {
				recommendations = append(recommendations, "Immediate action required: memory usage critical")
			}

			return &correlation.Result{
				Confidence: confidence,
				Severity:   severity,
				Title:      "Potential memory leak detected",
				Description: fmt.Sprintf(
					"Container memory usage shows a strong increasing trend (correlation: %.2f, slope: %.2f). "+
						"Current usage: %.1f%% of limit. Time to potential OOM: %s",
					trend.Correlation,
					trend.Slope,
					usageRatio*100,
					timeToOOM,
				),
				Evidence: correlation.Evidence{
					Metrics: map[string]float64{
						"trend_correlation": trend.Correlation,
						"trend_slope":       trend.Slope,
						"usage_ratio":       usageRatio,
						"current_usage":     currentUsage,
						"memory_limit":      memoryLimit,
						"recent_restarts":   float64(len(recentRestarts)),
					},
					Patterns: []string{
						fmt.Sprintf("Memory trend: %s (%s)", trend.Direction, trend.Strength),
						fmt.Sprintf("Usage ratio: %.1f%%", usageRatio*100),
					},
				},
				Recommendations: recommendations,
				Actions: []correlation.Action{
					{
						Type:     "alert",
						Target:   "dev-team",
						Priority: string(severity),
						Parameters: map[string]string{
							"usage_ratio": fmt.Sprintf("%.2f", usageRatio),
							"time_to_oom": timeToOOM,
						},
					},
				},
			}
		},
	}
}

// ContainerOOMPrediction predicts OOM kills before they happen
func ContainerOOMPrediction() *correlation.Rule {
	return &correlation.Rule{
		ID:          "container-oom-prediction",
		Name:        "Container OOM Prediction",
		Description: "Predicts container OOM kills before they occur",
		Category:    correlation.CategoryResource,
		Version:     "1.0.0",
		Author:      "Tapio Team",
		Tags:        []string{"memory", "oom", "prediction", "proactive"},

		MinConfidence: 0.8, // High confidence required for predictions
		Cooldown:      10 * time.Minute,
		TTL:           30 * time.Minute, // Shorter TTL for predictions

		RequiredSources: []correlation.EventSource{
			correlation.SourceMetrics,
		},

		Evaluate: func(ctx *correlation.Context) *correlation.Result {
			// Get memory metrics
			memoryUsage := ctx.GetMetric("container.memory.usage")
			memoryLimit := ctx.GetMetricValue("container.memory.limit")

			if len(memoryUsage.Points) < 5 || memoryLimit == 0 {
				return nil
			}

			// Get current usage ratio
			currentUsage := memoryUsage.Points[len(memoryUsage.Points)-1].Value
			usageRatio := currentUsage / memoryLimit

			// Only predict if usage is already high
			if usageRatio < 0.7 {
				return nil
			}

			// Analyze recent trend (last 5 minutes of data)
			recentPoints := memoryUsage.Last(5 * time.Minute)
			if len(recentPoints) < 3 {
				return nil
			}

			recentSeries := correlation.MetricSeries{
				Name:   "recent_memory",
				Points: recentPoints,
			}

			trend := ctx.AnalyzeTrend(recentSeries)

			// Must be increasing trend for prediction
			if !trend.IsIncreasing() {
				return nil
			}

			// Calculate time to OOM based on trend
			if trend.Slope <= 0 {
				return nil
			}

			remainingMemory := memoryLimit - currentUsage
			secondsToOOM := remainingMemory / trend.Slope

			// Only predict if OOM is likely within next 30 minutes
			if secondsToOOM <= 0 || secondsToOOM > 1800 { // 30 minutes
				return nil
			}

			timeToOOM := time.Duration(secondsToOOM * float64(time.Second))

			// Calculate confidence based on trend strength and usage level
			confidence := 0.5

			// Strong trend increases confidence
			if trend.IsStrong() {
				confidence += 0.2
			}

			// High usage ratio increases confidence
			if usageRatio > 0.95 {
				confidence += 0.2
			} else if usageRatio > 0.9 {
				confidence += 0.15
			} else if usageRatio > 0.8 {
				confidence += 0.1
			}

			// Shorter prediction time increases confidence
			if secondsToOOM < 300 { // 5 minutes
				confidence += 0.1
			}

			// Multiple data points increase confidence
			if len(recentPoints) > 10 {
				confidence += 0.05
			}

			return &correlation.Result{
				Confidence: confidence,
				Severity:   correlation.SeverityCritical,
				Title:      fmt.Sprintf("OOM kill predicted in %v", timeToOOM.Round(time.Second)),
				Description: fmt.Sprintf(
					"Container memory usage is at %.1f%% and increasing rapidly. "+
						"Based on current trend (slope: %.2f), OOM kill predicted in %v. "+
						"Immediate action recommended.",
					usageRatio*100,
					trend.Slope,
					timeToOOM.Round(time.Second),
				),
				Impact: "Container will likely be killed due to out-of-memory condition",
				Evidence: correlation.Evidence{
					Metrics: map[string]float64{
						"current_usage_ratio": usageRatio,
						"trend_slope":         trend.Slope,
						"trend_correlation":   trend.Correlation,
						"seconds_to_oom":      secondsToOOM,
						"memory_limit":        memoryLimit,
						"current_usage":       currentUsage,
					},
					Patterns: []string{
						fmt.Sprintf("Memory trend: %s (%s)", trend.Direction, trend.Strength),
						fmt.Sprintf("Predicted OOM in: %v", timeToOOM.Round(time.Second)),
					},
				},
				Recommendations: []string{
					"Immediately restart the container to prevent OOM",
					"Increase memory limits before restarting",
					"Investigate memory usage spike cause",
					"Consider horizontal scaling if possible",
				},
				Actions: []correlation.Action{
					{
						Type:     "alert",
						Target:   "ops-team",
						Priority: "critical",
						Parameters: map[string]string{
							"time_to_oom": timeToOOM.String(),
							"usage_ratio": fmt.Sprintf("%.2f", usageRatio),
							"immediate":   "true",
						},
					},
					{
						Type:   "webhook",
						Target: "auto-scaler",
						Parameters: map[string]string{
							"action":     "scale_up",
							"urgency":    "high",
							"prediction": "oom",
						},
						Condition: "auto_scaling_enabled",
					},
				},
			}
		},
	}
}
