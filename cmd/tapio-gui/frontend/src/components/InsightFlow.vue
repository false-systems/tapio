<template>
  <div class="insight-flow">
    <div class="flow-container">
      <div 
        v-for="(span, index) in sortedSpans" 
        :key="span.spanId"
        class="flow-node"
        :style="getNodeStyle(span)"
      >
        <div class="node-bar" :style="getBarStyle(span)"></div>
        <div class="node-content">
          <div class="node-name">{{ span.operationName }}</div>
          <div class="node-info">
            <span class="node-service">{{ span.serviceName }}</span>
            <span class="node-duration">{{ formatDuration(span.duration) }}</span>
          </div>
        </div>
      </div>
    </div>
    
    <!-- Time scale -->
    <div class="time-scale">
      <div class="scale-start">0ms</div>
      <div class="scale-end">{{ formatDuration(totalDuration) }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { OTELSpan } from '../mocks/otel'

const props = defineProps<{
  spans: OTELSpan[]
  traceId: string
}>()

// Sort spans by start time
const sortedSpans = computed(() => {
  return [...props.spans].sort((a, b) => a.startTime - b.startTime)
})

// Calculate total duration
const totalDuration = computed(() => {
  if (props.spans.length === 0) return 0
  
  const minStart = Math.min(...props.spans.map(s => s.startTime))
  const maxEnd = Math.max(...props.spans.map(s => s.startTime + s.duration))
  
  return maxEnd - minStart
})

const getNodeStyle = (span: OTELSpan) => {
  const minStart = Math.min(...props.spans.map(s => s.startTime))
  const relativeStart = span.startTime - minStart
  const leftPercent = (relativeStart / totalDuration.value) * 100
  
  return {
    marginLeft: `${leftPercent}%`
  }
}

const getBarStyle = (span: OTELSpan) => {
  const widthPercent = (span.duration / totalDuration.value) * 100
  const color = getSpanColor(span)
  
  return {
    width: `${Math.max(widthPercent, 1)}%`,
    backgroundColor: color
  }
}

const getSpanColor = (span: OTELSpan): string => {
  if (span.severity === 'critical') return '#D93025'
  if (span.severity === 'high') return '#F9AB00'
  if (span.severity === 'medium') return '#1A73E8'
  if (span.tags?.error) return '#D93025'
  return '#1E8E3E'
}

const formatDuration = (microseconds: number): string => {
  if (microseconds < 1000) {
    return `${Math.round(microseconds)}μs`
  } else if (microseconds < 1000000) {
    return `${(microseconds / 1000).toFixed(1)}ms`
  } else {
    return `${(microseconds / 1000000).toFixed(1)}s`
  }
}
</script>

<style scoped>
.insight-flow {
  position: relative;
  padding-bottom: 32px;
}

.flow-container {
  position: relative;
  min-height: 120px;
}

.flow-node {
  margin-bottom: 8px;
  position: relative;
}

.node-bar {
  height: 4px;
  border-radius: 2px;
  margin-bottom: 4px;
  transition: all 0.2s;
}

.node-content {
  padding: 8px 0;
}

.node-name {
  font-size: 13px;
  font-weight: 500;
  color: #202124;
  margin-bottom: 2px;
}

.node-info {
  display: flex;
  gap: 12px;
  font-size: 12px;
}

.node-service {
  color: #5F6368;
}

.node-duration {
  font-weight: 500;
  color: #202124;
}

.time-scale {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  display: flex;
  justify-content: space-between;
  padding-top: 8px;
  border-top: 1px solid #DADCE0;
  font-size: 11px;
  color: #5F6368;
}
</style>