<template>
  <div class="otel-trace-view">
    <div class="trace-header">
      <h3>OTEL Traces</h3>
      <div class="trace-controls">
        <button @click="refreshTraces" class="refresh-btn">
          <span class="icon">↻</span> Refresh
        </button>
        <select v-model="selectedService" @change="onServiceChange" class="service-selector">
          <option value="all">All Services</option>
          <option value="tapio-relay">Tapio Relay</option>
          <option value="tapio-engine">Tapio Engine</option>
          <option value="tapio-collector">Tapio Collector</option>
        </select>
      </div>
    </div>

    <div class="traces-container">
      <div v-if="loading" class="loading">Loading traces...</div>
      
      <div v-else-if="traces.length === 0" class="no-traces">
        No traces found. Make sure OTEL backend is running.
      </div>
      
      <div v-else class="trace-list">
        <div 
          v-for="trace in traces" 
          :key="trace.traceId"
          class="trace-item"
          :class="{ expanded: expandedTraces.has(trace.traceId) }"
          @click="toggleTrace(trace.traceId)"
        >
          <div class="trace-summary">
            <div class="trace-main">
              <span class="operation-name">{{ trace.operationName }}</span>
              <span class="service-name">{{ trace.serviceName }}</span>
            </div>
            <div class="trace-meta">
              <span class="span-count">{{ trace.spanCount }} spans</span>
              <span class="duration">{{ formatDuration(trace.duration) }}</span>
              <span class="timestamp">{{ formatTime(trace.startTime) }}</span>
            </div>
          </div>
          
          <div v-if="expandedTraces.has(trace.traceId)" class="trace-details">
            <OTELSpanTree :spans="trace.spans" :traceId="trace.traceId" />
            
            <div v-if="trace.tags" class="trace-tags">
              <h4>Trace Tags</h4>
              <div class="tag-list">
                <div v-for="(value, key) in trace.tags" :key="key" class="tag-item">
                  <span class="tag-key">{{ key }}:</span>
                  <span class="tag-value">{{ value }}</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import OTELSpanTree from './OTELSpanTree.vue'
// Use mock until Wails regenerates bindings
import { GetTraces, type OTELTrace, type OTELSpan, type SpanLog, type LogField, type SpanRef } from '../mocks/otel'

const traces = ref<OTELTrace[]>([])
const loading = ref(false)
const selectedService = ref('all')
const expandedTraces = ref(new Set<string>())

const refreshTraces = async () => {
  loading.value = true
  try {
    const serviceName = selectedService.value === 'all' ? '' : selectedService.value
    traces.value = await GetTraces(serviceName, 20)
  } catch (error) {
    console.error('Failed to fetch traces:', error)
  } finally {
    loading.value = false
  }
}

const onServiceChange = () => {
  refreshTraces()
}

const toggleTrace = (traceId: string) => {
  if (expandedTraces.value.has(traceId)) {
    expandedTraces.value.delete(traceId)
  } else {
    expandedTraces.value.add(traceId)
  }
}

const formatDuration = (microseconds: number): string => {
  if (microseconds < 1000) {
    return `${microseconds}μs`
  } else if (microseconds < 1000000) {
    return `${(microseconds / 1000).toFixed(2)}ms`
  } else {
    return `${(microseconds / 1000000).toFixed(2)}s`
  }
}

const formatTime = (timestamp: string): string => {
  const date = new Date(timestamp)
  return date.toLocaleTimeString()
}

onMounted(() => {
  refreshTraces()
})
</script>

<style scoped>
.otel-trace-view {
  background: #ffffff;
  border-radius: 12px;
  padding: 24px;
  box-shadow: 0 4px 24px rgba(0, 0, 0, 0.06);
}

.trace-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.trace-header h3 {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  color: #1a1a1a;
}

.trace-controls {
  display: flex;
  gap: 12px;
  align-items: center;
}

.refresh-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  background: #f0f2f5;
  border: none;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 500;
  color: #495057;
  cursor: pointer;
  transition: all 0.2s;
}

.refresh-btn:hover {
  background: #e1e5eb;
  transform: translateY(-1px);
}

.refresh-btn .icon {
  font-size: 16px;
}

.service-selector {
  padding: 8px 12px;
  border: 1px solid #e1e5eb;
  border-radius: 8px;
  font-size: 14px;
  background: white;
  color: #495057;
  cursor: pointer;
}

.traces-container {
  min-height: 400px;
}

.loading, .no-traces {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 400px;
  color: #6c757d;
  font-size: 16px;
}

.trace-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.trace-item {
  background: #f8f9fa;
  border: 1px solid #e9ecef;
  border-radius: 8px;
  padding: 16px;
  cursor: pointer;
  transition: all 0.2s;
}

.trace-item:hover {
  border-color: #3b82f6;
  box-shadow: 0 2px 8px rgba(59, 130, 246, 0.1);
}

.trace-item.expanded {
  background: white;
  border-color: #3b82f6;
}

.trace-summary {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.trace-main {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.operation-name {
  font-weight: 600;
  color: #1a1a1a;
  font-size: 15px;
}

.service-name {
  font-size: 13px;
  color: #6c757d;
}

.trace-meta {
  display: flex;
  gap: 16px;
  align-items: center;
  font-size: 13px;
  color: #6c757d;
}

.span-count {
  background: #e3f2fd;
  color: #1976d2;
  padding: 2px 8px;
  border-radius: 4px;
  font-weight: 500;
}

.duration {
  font-weight: 600;
  color: #495057;
}

.trace-details {
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid #e9ecef;
}

.trace-tags {
  margin-top: 16px;
  padding: 12px;
  background: #f8f9fa;
  border-radius: 6px;
}

.trace-tags h4 {
  margin: 0 0 8px 0;
  font-size: 14px;
  font-weight: 600;
  color: #495057;
}

.tag-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tag-item {
  display: flex;
  gap: 4px;
  padding: 4px 8px;
  background: white;
  border-radius: 4px;
  font-size: 12px;
}

.tag-key {
  color: #6c757d;
}

.tag-value {
  font-weight: 500;
  color: #1a1a1a;
}
</style>