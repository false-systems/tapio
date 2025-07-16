<template>
  <div class="span-tree">
    <div 
      v-for="span in rootSpans" 
      :key="span.spanId"
      class="span-node"
    >
      <SpanNode 
        :span="span" 
        :spans="spans" 
        :depth="0"
        @span-click="onSpanClick"
      />
    </div>
    
    <!-- Span Details Modal -->
    <div v-if="selectedSpan" class="span-details-modal" @click="selectedSpan = null">
      <div class="span-details" @click.stop>
        <div class="span-details-header">
          <h4>{{ selectedSpan.operationName }}</h4>
          <button @click="selectedSpan = null" class="close-btn">×</button>
        </div>
        
        <div class="span-details-content">
          <div class="detail-section">
            <h5>Basic Info</h5>
            <div class="detail-row">
              <span class="label">Service:</span>
              <span class="value">{{ selectedSpan.serviceName }}</span>
            </div>
            <div class="detail-row">
              <span class="label">Duration:</span>
              <span class="value">{{ formatDuration(selectedSpan.duration) }}</span>
            </div>
            <div class="detail-row">
              <span class="label">Start Time:</span>
              <span class="value">{{ formatTimestamp(selectedSpan.startTime) }}</span>
            </div>
          </div>
          
          <div v-if="selectedSpan.storyId" class="detail-section tapio-section">
            <h5>Tapio Intelligence</h5>
            <div class="detail-row">
              <span class="label">Story ID:</span>
              <span class="value link" @click="openStory(selectedSpan.storyId)">
                {{ selectedSpan.storyId }}
              </span>
            </div>
            <div v-if="selectedSpan.correlationId" class="detail-row">
              <span class="label">Correlation:</span>
              <span class="value">{{ selectedSpan.correlationId }}</span>
            </div>
            <div v-if="selectedSpan.severity" class="detail-row">
              <span class="label">Severity:</span>
              <span class="value severity" :class="selectedSpan.severity">
                {{ selectedSpan.severity }}
              </span>
            </div>
            <div v-if="selectedSpan.pattern" class="detail-row">
              <span class="label">Pattern:</span>
              <span class="value">{{ selectedSpan.pattern }}</span>
            </div>
          </div>
          
          <div v-if="selectedSpan.tags && Object.keys(selectedSpan.tags).length > 0" class="detail-section">
            <h5>Tags</h5>
            <div class="tags-grid">
              <div v-for="(value, key) in selectedSpan.tags" :key="key" class="tag-entry">
                <span class="tag-key">{{ key }}:</span>
                <span class="tag-value">{{ formatTagValue(value) }}</span>
              </div>
            </div>
          </div>
          
          <div v-if="selectedSpan.logs && selectedSpan.logs.length > 0" class="detail-section">
            <h5>Logs</h5>
            <div class="logs-list">
              <div v-for="(log, index) in selectedSpan.logs" :key="index" class="log-entry">
                <span class="log-time">{{ formatTimestamp(log.timestamp) }}</span>
                <div class="log-fields">
                  <div v-for="field in log.fields" :key="field.key" class="log-field">
                    <span class="field-key">{{ field.key }}:</span>
                    <span class="field-value">{{ field.value }}</span>
                  </div>
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
import { ref, computed } from 'vue'
import SpanNode from './SpanNode.vue'
import type { OTELSpan, SpanLog, LogField, SpanRef } from '../mocks/otel'

const props = defineProps<{
  spans: OTELSpan[]
  traceId: string
}>()

const selectedSpan = ref<OTELSpan | null>(null)

// Compute root spans (spans without parent references)
const rootSpans = computed(() => {
  const spanMap = new Map(props.spans.map(s => [s.spanId, s]))
  const childSpanIds = new Set<string>()
  
  props.spans.forEach(span => {
    if (span.references) {
      span.references.forEach(ref => {
        if (ref.refType === 'CHILD_OF' && ref.traceId === props.traceId) {
          childSpanIds.add(span.spanId)
        }
      })
    }
  })
  
  return props.spans.filter(span => !childSpanIds.has(span.spanId))
})

const onSpanClick = (span: OTELSpan) => {
  selectedSpan.value = span
}

const openStory = (storyId: string) => {
  // Emit event to parent to switch to story view
  console.log('Opening story:', storyId)
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

const formatTimestamp = (microseconds: number): string => {
  const date = new Date(microseconds / 1000)
  return date.toLocaleTimeString([], { 
    hour: '2-digit', 
    minute: '2-digit', 
    second: '2-digit',
    fractionalSecondDigits: 3 
  })
}

const formatTagValue = (value: any): string => {
  if (typeof value === 'object') {
    return JSON.stringify(value, null, 2)
  }
  return String(value)
}
</script>

<style scoped>
.span-tree {
  position: relative;
}

.span-node {
  margin-bottom: 2px;
}

/* Span Details Modal */
.span-details-modal {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.span-details {
  background: white;
  border-radius: 12px;
  width: 90%;
  max-width: 600px;
  max-height: 80vh;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.2);
}

.span-details-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 20px 24px;
  border-bottom: 1px solid #e9ecef;
}

.span-details-header h4 {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: #1a1a1a;
}

.close-btn {
  background: none;
  border: none;
  font-size: 24px;
  color: #6c757d;
  cursor: pointer;
  padding: 0;
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  transition: all 0.2s;
}

.close-btn:hover {
  background: #f0f2f5;
  color: #1a1a1a;
}

.span-details-content {
  flex: 1;
  overflow-y: auto;
  padding: 24px;
}

.detail-section {
  margin-bottom: 24px;
}

.detail-section:last-child {
  margin-bottom: 0;
}

.detail-section h5 {
  margin: 0 0 12px 0;
  font-size: 14px;
  font-weight: 600;
  color: #6c757d;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.detail-row {
  display: flex;
  gap: 12px;
  margin-bottom: 8px;
  font-size: 14px;
}

.detail-row .label {
  flex-shrink: 0;
  width: 120px;
  color: #6c757d;
}

.detail-row .value {
  flex: 1;
  color: #1a1a1a;
  font-weight: 500;
  word-break: break-word;
}

.detail-row .value.link {
  color: #3b82f6;
  cursor: pointer;
  text-decoration: underline;
}

.detail-row .value.link:hover {
  color: #2563eb;
}

.severity {
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
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

.tapio-section {
  background: #f0f9ff;
  padding: 16px;
  border-radius: 8px;
  margin-bottom: 24px;
}

.tags-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 8px;
}

.tag-entry {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 8px;
  background: #f8f9fa;
  border-radius: 6px;
}

.tag-key {
  font-size: 12px;
  color: #6c757d;
}

.tag-value {
  font-size: 13px;
  font-weight: 500;
  color: #1a1a1a;
  word-break: break-word;
}

.logs-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.log-entry {
  padding: 12px;
  background: #f8f9fa;
  border-radius: 6px;
}

.log-time {
  font-size: 12px;
  color: #6c757d;
  font-family: monospace;
}

.log-fields {
  margin-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.log-field {
  display: flex;
  gap: 8px;
  font-size: 13px;
}

.field-key {
  color: #6c757d;
}

.field-value {
  color: #1a1a1a;
  font-weight: 500;
}
</style>