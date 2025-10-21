package k8scontext

import (
	"encoding/json"
	"fmt"
)

// makeSchedulingInfoKey generates NATS KV key for pod scheduling metadata
func makeSchedulingInfoKey(podUID string) string {
	return fmt.Sprintf("pod.scheduling.%s", podUID)
}

// makePluginMetricsKey generates NATS KV key for plugin metrics
func makePluginMetricsKey(pluginName, extensionPoint string) string {
	return fmt.Sprintf("scheduler.plugin.%s.%s", pluginName, extensionPoint)
}

// makeSchedulerMetricsKey generates NATS KV key for global scheduler metrics
func makeSchedulerMetricsKey() string {
	return "scheduler.metrics"
}

// makePreemptionInfoKey generates NATS KV key for preemption events
func makePreemptionInfoKey(victimPodUID string) string {
	return fmt.Sprintf("scheduler.preemption.%s", victimPodUID)
}

// serializeSchedulingInfo marshals SchedulingInfo to JSON
func serializeSchedulingInfo(info SchedulingInfo) ([]byte, error) {
	data, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal SchedulingInfo: %w", err)
	}
	return data, nil
}

// serializePluginMetrics marshals PluginMetrics to JSON
func serializePluginMetrics(metrics PluginMetrics) ([]byte, error) {
	data, err := json.Marshal(metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PluginMetrics: %w", err)
	}
	return data, nil
}

// serializeSchedulerMetrics marshals SchedulerMetrics to JSON
func serializeSchedulerMetrics(metrics SchedulerMetrics) ([]byte, error) {
	data, err := json.Marshal(metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal SchedulerMetrics: %w", err)
	}
	return data, nil
}

// serializePreemptionInfo marshals PreemptionInfo to JSON
func serializePreemptionInfo(info PreemptionInfo) ([]byte, error) {
	data, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PreemptionInfo: %w", err)
	}
	return data, nil
}

// storeSchedulingInfo writes scheduling metadata to NATS KV
func (s *Service) storeSchedulingInfo(info SchedulingInfo) error {
	data, err := serializeSchedulingInfo(info)
	if err != nil {
		return err
	}

	key := makeSchedulingInfoKey(info.PodUID)
	if _, err := s.kv.Put(key, data); err != nil {
		return fmt.Errorf("failed to store scheduling info for %s: %w", key, err)
	}

	return nil
}

// deleteSchedulingInfo removes scheduling metadata from NATS KV
func (s *Service) deleteSchedulingInfo(podUID string) error {
	key := makeSchedulingInfoKey(podUID)
	if err := s.kv.Delete(key); err != nil {
		return fmt.Errorf("failed to delete scheduling info for %s: %w", key, err)
	}

	return nil
}

// storePluginMetrics writes plugin metrics to NATS KV
func (s *Service) storePluginMetrics(metrics PluginMetrics) error {
	data, err := serializePluginMetrics(metrics)
	if err != nil {
		return err
	}

	key := makePluginMetricsKey(metrics.PluginName, metrics.ExtensionPoint)
	if _, err := s.kv.Put(key, data); err != nil {
		return fmt.Errorf("failed to store plugin metrics for %s: %w", key, err)
	}

	return nil
}

// storeSchedulerMetrics writes global scheduler metrics to NATS KV
func (s *Service) storeSchedulerMetrics(metrics SchedulerMetrics) error {
	data, err := serializeSchedulerMetrics(metrics)
	if err != nil {
		return err
	}

	key := makeSchedulerMetricsKey()
	if _, err := s.kv.Put(key, data); err != nil {
		return fmt.Errorf("failed to store scheduler metrics: %w", err)
	}

	return nil
}

// storePreemptionInfo writes preemption event to NATS KV
func (s *Service) storePreemptionInfo(info PreemptionInfo) error {
	data, err := serializePreemptionInfo(info)
	if err != nil {
		return err
	}

	key := makePreemptionInfoKey(info.VictimPodUID)
	if _, err := s.kv.Put(key, data); err != nil {
		return fmt.Errorf("failed to store preemption info for %s: %w", key, err)
	}

	return nil
}

// getSchedulingInfo retrieves scheduling metadata from NATS KV
func (s *Service) getSchedulingInfo(podUID string) (*SchedulingInfo, error) {
	key := makeSchedulingInfoKey(podUID)
	entry, err := s.kv.Get(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get scheduling info for %s: %w", key, err)
	}

	var info SchedulingInfo
	if err := json.Unmarshal(entry.Value(), &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal SchedulingInfo: %w", err)
	}

	return &info, nil
}
