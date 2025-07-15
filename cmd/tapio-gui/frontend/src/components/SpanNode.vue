<template>
  <div class="span-node-container">
    <div 
      class="span-node-content"
      :style="{ paddingLeft: `${depth * 24}px` }"
      @click="$emit('span-click', span)"
    >
      <div class="span-bar-container">
        <div 
          class="span-bar"
          :style="{ 
            left: `${getSpanLeft()}%`,
            width: `${getSpanWidth()}%`
          }"
          :class="getSpanClass()"
        />
      </div>
      
      <div class="span-info">
        <div class="span-name">
          <span v-if="hasChildren" class="expand-icon" @click.stop="toggleExpanded">
            {{ expanded ? '▼' : '▶' }}
          </span>
          {{ span.operationName }}
        </div>
        <div class="span-meta">
          <span class="service">{{ span.serviceName }}</span>
          <span class="duration">{{ formatDuration(span.duration) }}</span>
          <span v-if="span.severity" class="severity" :class="span.severity">
            {{ span.severity }}
          </span>
        </div>
      </div>
    </div>
    
    <div v-if="expanded && hasChildren" class="children">
      <SpanNode
        v-for="child in childSpans"
        :key="child.spanId"
        :span="child"
        :spans="spans"
        :depth="depth + 1"
        :trace-start-time="traceStartTime"
        :trace-end-time="traceEndTime"
        @span-click="$emit('span-click', $event)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import type { OTELSpan, SpanRef } from '../mocks/otel'

const props = defineProps<{
  span: OTELSpan
  spans: OTELSpan[]
  depth: number
  traceStartTime?: number
  traceEndTime?: number
}>()

const emit = defineEmits<{
  'span-click': [span: OTELSpan]
}>()

const expanded = ref(true)

// Calculate trace time bounds
const traceTimeInfo = computed(() => {
  if (props.traceStartTime && props.traceEndTime) {
    return {
      startTime: props.traceStartTime,
      endTime: props.traceEndTime,
      duration: props.traceEndTime - props.traceStartTime
    }
  }
  
  // Calculate from all spans if not provided
  let minTime = props.span.startTime
  let maxTime = props.span.startTime + props.span.duration
  
  props.spans.forEach(s => {
    minTime = Math.min(minTime, s.startTime)
    maxTime = Math.max(maxTime, s.startTime + s.duration)
  })
  
  return {
    startTime: minTime,
    endTime: maxTime,
    duration: maxTime - minTime
  }
})

// Find child spans
const childSpans = computed(() => {
  return props.spans.filter(s => {
    if (!s.references) return false
    return s.references.some(ref => 
      ref.refType === 'CHILD_OF' && 
      ref.spanId === props.span.spanId
    )
  })
})

const hasChildren = computed(() => childSpans.value.length > 0)

const toggleExpanded = () => {
  expanded.value = !expanded.value
}

const getSpanLeft = (): number => {
  const { startTime, duration } = traceTimeInfo.value
  if (duration === 0) return 0
  return ((props.span.startTime - startTime) / duration) * 100
}

const getSpanWidth = (): number => {
  const { duration } = traceTimeInfo.value
  if (duration === 0) return 100
  return (props.span.duration / duration) * 100
}

const getSpanClass = (): string => {
  if (props.span.severity) {
    return `severity-${props.span.severity}`
  }
  if (props.span.tags?.error) {
    return 'error'
  }
  return 'normal'
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
</script>

<style scoped>
.span-node-container {
  position: relative;
}

.span-node-content {
  position: relative;
  padding: 8px 12px;
  cursor: pointer;
  transition: background 0.2s;
  min-height: 40px;
  display: flex;
  align-items: center;
}

.span-node-content:hover {
  background: rgba(59, 130, 246, 0.05);
}

.span-bar-container {
  position: absolute;
  top: 8px;
  left: 0;
  right: 0;
  height: 4px;
  background: #f0f2f5;
  border-radius: 2px;
}

.span-bar {
  position: absolute;
  top: 0;
  height: 100%;
  border-radius: 2px;
  transition: all 0.2s;
}

.span-bar.normal {
  background: #3b82f6;
}

.span-bar.error {
  background: #dc2626;
}

.span-bar.severity-critical {
  background: #dc2626;
}

.span-bar.severity-high {
  background: #ea580c;
}

.span-bar.severity-medium {
  background: #d97706;
}

.span-bar.severity-low {
  background: #2563eb;
}

.span-info {
  position: relative;
  z-index: 1;
  flex: 1;
  margin-top: 12px;
}

.span-name {
  font-size: 14px;
  font-weight: 500;
  color: #1a1a1a;
  display: flex;
  align-items: center;
  gap: 8px;
}

.expand-icon {
  font-size: 10px;
  color: #6c757d;
  user-select: none;
}

.span-meta {
  display: flex;
  gap: 12px;
  margin-top: 4px;
  font-size: 12px;
}

.service {
  color: #6c757d;
}

.duration {
  font-weight: 600;
  color: #495057;
}

.severity {
  padding: 1px 6px;
  border-radius: 3px;
  font-weight: 600;
  text-transform: uppercase;
  font-size: 10px;
}

.severity.critical {
  background: #fee2e2;
  color: #dc2626;
}

.severity.high {
  background: #fed7aa;
  color: #ea580c;
}

.severity.medium {
  background: #fef3c7;
  color: #d97706;
}

.severity.low {
  background: #dbeafe;
  color: #2563eb;
}

.children {
  position: relative;
  margin-left: 12px;
  border-left: 1px solid #e9ecef;
}
</style>