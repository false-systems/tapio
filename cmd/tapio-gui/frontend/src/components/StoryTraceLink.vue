<template>
  <div class="story-trace-link" v-if="story">
    <div class="link-header">
      <h4>Related OTEL Traces</h4>
      <button @click="fetchTraces" class="refresh-btn" :disabled="loading">
        <span class="icon">🔗</span> Find Traces
      </button>
    </div>
    
    <div v-if="loading" class="loading-state">
      Searching for related traces...
    </div>
    
    <div v-else-if="traces.length === 0" class="empty-state">
      No traces found for this story. Make sure OTEL is enabled.
    </div>
    
    <div v-else class="trace-links">
      <div 
        v-for="trace in traces" 
        :key="trace.traceId"
        class="trace-link-item"
        @click="viewTrace(trace)"
      >
        <div class="trace-info">
          <span class="operation">{{ trace.operationName }}</span>
          <span class="service">{{ trace.serviceName }}</span>
        </div>
        <div class="trace-meta">
          <span class="spans">{{ trace.spanCount }} spans</span>
          <span class="duration">{{ formatDuration(trace.duration) }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
// Use mock until Wails regenerates bindings
import { GetTracesForStory, type OTELTrace } from '../mocks/otel'

interface Story {
  id: string
  title: string
  correlationId?: string
}

const props = defineProps<{
  story: Story | null
}>()

const emit = defineEmits<{
  'view-trace': [trace: OTELTrace]
}>()

const traces = ref<OTELTrace[]>([])
const loading = ref(false)

const fetchTraces = async () => {
  if (!props.story) return
  
  loading.value = true
  try {
    // This would call the backend to find traces related to this story
    // For now, using mock implementation
    traces.value = await GetTracesForStory(props.story.id)
  } catch (error) {
    console.error('Failed to fetch traces for story:', error)
    traces.value = []
  } finally {
    loading.value = false
  }
}

const viewTrace = (trace: OTELTrace) => {
  emit('view-trace', trace)
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

// Auto-fetch traces when story changes
watch(() => props.story, (newStory) => {
  if (newStory) {
    fetchTraces()
  } else {
    traces.value = []
  }
}, { immediate: true })
</script>

<style scoped>
.story-trace-link {
  margin-top: 16px;
  padding: 16px;
  background: #f8f9fa;
  border-radius: 8px;
}

.link-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.link-header h4 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: #495057;
}

.refresh-btn {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 12px;
  background: white;
  border: 1px solid #dee2e6;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  color: #495057;
  cursor: pointer;
  transition: all 0.2s;
}

.refresh-btn:hover:not(:disabled) {
  background: #e9ecef;
  border-color: #adb5bd;
}

.refresh-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.loading-state, .empty-state {
  text-align: center;
  padding: 24px;
  color: #6c757d;
  font-size: 14px;
}

.trace-links {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.trace-link-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px;
  background: white;
  border: 1px solid #e9ecef;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.2s;
}

.trace-link-item:hover {
  border-color: #3b82f6;
  box-shadow: 0 2px 4px rgba(59, 130, 246, 0.1);
}

.trace-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.operation {
  font-size: 14px;
  font-weight: 500;
  color: #1a1a1a;
}

.service {
  font-size: 12px;
  color: #6c757d;
}

.trace-meta {
  display: flex;
  gap: 12px;
  align-items: center;
  font-size: 12px;
  color: #6c757d;
}

.spans {
  background: #e3f2fd;
  color: #1976d2;
  padding: 2px 6px;
  border-radius: 3px;
  font-weight: 500;
}

.duration {
  font-weight: 600;
  color: #495057;
}
</style>