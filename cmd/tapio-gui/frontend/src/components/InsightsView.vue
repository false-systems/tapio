<template>
  <div class="insights-view">
    <!-- Clean Header -->
    <div class="insights-header">
      <div class="header-content">
        <h2 class="view-title">Insights</h2>
        <p class="view-subtitle">Deep technical context for every story</p>
      </div>
      
      <div class="header-actions">
        <div class="filter-group">
          <select v-model="selectedService" class="filter-select">
            <option value="">All services</option>
            <option value="tapio-relay">tapio-relay</option>
            <option value="tapio-engine">tapio-engine</option>
            <option value="tapio-collector">tapio-collector</option>
          </select>
          
          <select v-model="selectedTimeRange" class="filter-select">
            <option value="1h">Last hour</option>
            <option value="6h">Last 6 hours</option>
            <option value="24h">Last 24 hours</option>
            <option value="7d">Last 7 days</option>
          </select>
          
          <button @click="refreshInsights" class="icon-button" :disabled="loading">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
              <path d="M13.65 2.35A8 8 0 1 0 16 8h-1.5A6.5 6.5 0 1 1 8 1.5V0l4 4-4 4V5.5" 
                    stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
                    :class="{ 'animate-spin': loading }"/>
            </svg>
          </button>
        </div>
      </div>
    </div>

    <!-- Insights List -->
    <div class="insights-container">
      <div v-if="loading && insights.length === 0" class="empty-state">
        <div class="loading-spinner"></div>
        <p>Loading insights...</p>
      </div>
      
      <div v-else-if="insights.length === 0" class="empty-state">
        <svg width="48" height="48" viewBox="0 0 48 48" fill="none" class="empty-icon">
          <circle cx="24" cy="24" r="20" stroke="#DADCE0" stroke-width="2"/>
          <path d="M24 14v10M24 28v.01" stroke="#5F6368" stroke-width="2" stroke-linecap="round"/>
        </svg>
        <p>No insights available</p>
        <p class="empty-subtitle">Run tapio check to generate insights</p>
      </div>
      
      <div v-else class="insights-list">
        <div 
          v-for="insight in filteredInsights" 
          :key="insight.traceId"
          class="insight-card"
          :class="{ expanded: expandedInsights.has(insight.traceId) }"
        >
          <!-- Insight Summary -->
          <div class="insight-summary" @click="toggleInsight(insight.traceId)">
            <div class="insight-main">
              <div class="insight-header">
                <span class="insight-operation">{{ insight.operationName }}</span>
                <span class="insight-service">{{ insight.serviceName }}</span>
              </div>
              <div class="insight-context">
                {{ getInsightContext(insight) }}
              </div>
            </div>
            
            <div class="insight-meta">
              <span class="insight-severity" :class="getSeverityClass(insight)">
                {{ getSeverityLabel(insight) }}
              </span>
              <span class="insight-duration">{{ formatDuration(insight.duration) }}</span>
              <span class="insight-time">{{ formatTime(insight.startTime) }}</span>
            </div>
          </div>
          
          <!-- Expanded Details -->
          <div v-if="expandedInsights.has(insight.traceId)" class="insight-details">
            <!-- Flow Visualization -->
            <div class="flow-section">
              <h4 class="section-title">Event Flow</h4>
              <div class="flow-timeline">
                <InsightFlow :spans="insight.spans" :traceId="insight.traceId" />
              </div>
            </div>
            
            <!-- Key Findings -->
            <div class="findings-section">
              <h4 class="section-title">Key Findings</h4>
              <div class="findings-list">
                <div v-for="finding in getKeyFindings(insight)" :key="finding.id" class="finding">
                  <span class="finding-icon" :class="finding.type">{{ getFindingIcon(finding.type) }}</span>
                  <div class="finding-content">
                    <p class="finding-title">{{ finding.title }}</p>
                    <p class="finding-detail">{{ finding.detail }}</p>
                  </div>
                </div>
              </div>
            </div>
            
            <!-- Technical Details -->
            <div class="technical-section">
              <h4 class="section-title">Technical Details</h4>
              <div class="detail-grid">
                <div class="detail-item">
                  <span class="detail-label">Trace ID</span>
                  <span class="detail-value monospace">{{ insight.traceId }}</span>
                </div>
                <div class="detail-item">
                  <span class="detail-label">Total Spans</span>
                  <span class="detail-value">{{ insight.spanCount }}</span>
                </div>
                <div class="detail-item">
                  <span class="detail-label">Root Cause</span>
                  <span class="detail-value">{{ getRootCause(insight) }}</span>
                </div>
              </div>
            </div>
            
            <!-- Related Story Link -->
            <div v-if="insight.storyId" class="story-link">
              <button @click="viewStory(insight.storyId)" class="link-button">
                View Related Story →
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import InsightFlow from './InsightFlow.vue'
import { GetTraces, type OTELTrace } from '../mocks/otel'

// Rename OTELTrace to Insight for clarity
type Insight = OTELTrace

const insights = ref<Insight[]>([])
const loading = ref(false)
const selectedService = ref('')
const selectedTimeRange = ref('1h')
const expandedInsights = ref(new Set<string>())

const filteredInsights = computed(() => {
  if (!selectedService.value) return insights.value
  return insights.value.filter(i => i.serviceName === selectedService.value)
})

const refreshInsights = async () => {
  loading.value = true
  try {
    insights.value = await GetTraces(selectedService.value, 20)
  } catch (error) {
    console.error('Failed to fetch insights:', error)
  } finally {
    loading.value = false
  }
}

const toggleInsight = (traceId: string) => {
  if (expandedInsights.value.has(traceId)) {
    expandedInsights.value.delete(traceId)
  } else {
    expandedInsights.value.add(traceId)
  }
}

const getInsightContext = (insight: Insight): string => {
  // Extract human-readable context from the insight
  const tags = insight.tags || {}
  if (tags['correlation.id']) {
    return `Part of ${tags['correlation.id']} correlation group`
  }
  if (insight.operationName.includes('memory')) {
    return 'Memory pressure detected across multiple pods'
  }
  if (insight.operationName.includes('network')) {
    return 'Network connectivity issues identified'
  }
  return 'System behavior analysis'
}

const getSeverityClass = (insight: Insight): string => {
  const severity = insight.tags?.severity || 'normal'
  return `severity-${severity}`
}

const getSeverityLabel = (insight: Insight): string => {
  return insight.tags?.severity || 'normal'
}

const getKeyFindings = (insight: Insight): any[] => {
  // Extract key findings from spans
  return [
    {
      id: '1',
      type: 'error',
      title: 'Memory limit exceeded',
      detail: 'Pod api-service-abc123 hit 256Mi limit'
    },
    {
      id: '2',
      type: 'warning',
      title: 'Predicted failure',
      detail: 'Service outage likely in 45 minutes'
    },
    {
      id: '3',
      type: 'info',
      title: 'Pattern detected',
      detail: 'Consistent OOM kills every 5 minutes'
    }
  ]
}

const getFindingIcon = (type: string): string => {
  switch (type) {
    case 'error': return '⚠️'
    case 'warning': return '⚡'
    case 'info': return 'ℹ️'
    default: return '•'
  }
}

const getRootCause = (insight: Insight): string => {
  return insight.tags?.['root.cause'] || 'Analyzing...'
}

const formatDuration = (microseconds: number): string => {
  if (microseconds < 1000) {
    return `${microseconds}μs`
  } else if (microseconds < 1000000) {
    return `${(microseconds / 1000).toFixed(1)}ms`
  } else {
    return `${(microseconds / 1000000).toFixed(1)}s`
  }
}

const formatTime = (timestamp: string): string => {
  const date = new Date(timestamp)
  const now = new Date()
  const diff = now.getTime() - date.getTime()
  
  if (diff < 60000) return 'just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  return date.toLocaleDateString()
}

const viewStory = (storyId: string) => {
  // Emit event to switch to story view
  console.log('View story:', storyId)
}

onMounted(() => {
  refreshInsights()
})
</script>

<style scoped>
/* GCP-inspired clean design */
.insights-view {
  height: 100%;
  display: flex;
  flex-direction: column;
}

/* Header */
.insights-header {
  background: #FFFFFF;
  border-bottom: 1px solid #DADCE0;
  padding: 24px 32px;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-content {
  flex: 1;
}

.view-title {
  font-size: 24px;
  font-weight: 400;
  color: #202124;
  margin: 0;
  letter-spacing: -0.02em;
}

.view-subtitle {
  font-size: 14px;
  color: #5F6368;
  margin: 4px 0 0 0;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.filter-group {
  display: flex;
  gap: 8px;
  align-items: center;
}

.filter-select {
  height: 36px;
  padding: 0 12px;
  border: 1px solid #DADCE0;
  border-radius: 4px;
  background: #FFFFFF;
  color: #202124;
  font-size: 14px;
  cursor: pointer;
  transition: all 0.2s;
}

.filter-select:hover {
  border-color: #5F6368;
}

.filter-select:focus {
  outline: none;
  border-color: #1A73E8;
}

.icon-button {
  width: 36px;
  height: 36px;
  border: 1px solid #DADCE0;
  border-radius: 4px;
  background: #FFFFFF;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s;
}

.icon-button:hover {
  background: #F8F9FA;
  border-color: #5F6368;
}

.icon-button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* Container */
.insights-container {
  flex: 1;
  overflow-y: auto;
  background: #F8F9FA;
  padding: 24px 32px;
}

/* Empty State */
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 400px;
  text-align: center;
}

.empty-icon {
  margin-bottom: 16px;
}

.empty-state p {
  margin: 0;
  color: #5F6368;
  font-size: 16px;
}

.empty-subtitle {
  font-size: 14px !important;
  margin-top: 8px !important;
  color: #80868B !important;
}

.loading-spinner {
  width: 32px;
  height: 32px;
  border: 3px solid #F8F9FA;
  border-top-color: #1A73E8;
  border-radius: 50%;
  animation: spin 1s linear infinite;
  margin-bottom: 16px;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

/* Insights List */
.insights-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
  max-width: 1200px;
  margin: 0 auto;
}

/* Insight Card */
.insight-card {
  background: #FFFFFF;
  border: 1px solid #DADCE0;
  border-radius: 8px;
  transition: all 0.2s;
}

.insight-card:hover {
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

.insight-card.expanded {
  box-shadow: 0 4px 8px rgba(0, 0, 0, 0.1);
}

.insight-summary {
  padding: 20px 24px;
  cursor: pointer;
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
}

.insight-main {
  flex: 1;
  margin-right: 24px;
}

.insight-header {
  display: flex;
  align-items: baseline;
  gap: 12px;
  margin-bottom: 8px;
}

.insight-operation {
  font-size: 16px;
  font-weight: 500;
  color: #202124;
}

.insight-service {
  font-size: 14px;
  color: #5F6368;
}

.insight-context {
  font-size: 14px;
  color: #5F6368;
  line-height: 1.5;
}

.insight-meta {
  display: flex;
  gap: 16px;
  align-items: center;
  flex-shrink: 0;
}

.insight-severity {
  padding: 4px 8px;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 500;
  text-transform: uppercase;
}

.severity-critical {
  background: #FCE8E6;
  color: #D93025;
}

.severity-high {
  background: #FEF7E0;
  color: #F9AB00;
}

.severity-medium {
  background: #E4F7FB;
  color: #1A73E8;
}

.severity-normal {
  background: #E6F4EA;
  color: #1E8E3E;
}

.insight-duration {
  font-size: 14px;
  font-weight: 500;
  color: #202124;
}

.insight-time {
  font-size: 14px;
  color: #5F6368;
}

/* Insight Details */
.insight-details {
  border-top: 1px solid #DADCE0;
  padding: 24px;
  background: #FAFAFA;
}

.section-title {
  font-size: 14px;
  font-weight: 500;
  color: #202124;
  margin: 0 0 16px 0;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

/* Flow Section */
.flow-section {
  margin-bottom: 32px;
}

.flow-timeline {
  background: #FFFFFF;
  border: 1px solid #DADCE0;
  border-radius: 4px;
  padding: 16px;
}

/* Findings Section */
.findings-section {
  margin-bottom: 32px;
}

.findings-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.finding {
  display: flex;
  gap: 12px;
  padding: 12px;
  background: #FFFFFF;
  border: 1px solid #DADCE0;
  border-radius: 4px;
}

.finding-icon {
  font-size: 18px;
  flex-shrink: 0;
}

.finding-content {
  flex: 1;
}

.finding-title {
  font-size: 14px;
  font-weight: 500;
  color: #202124;
  margin: 0 0 4px 0;
}

.finding-detail {
  font-size: 13px;
  color: #5F6368;
  margin: 0;
}

/* Technical Section */
.technical-section {
  margin-bottom: 24px;
}

.detail-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
  gap: 16px;
}

.detail-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.detail-label {
  font-size: 12px;
  color: #5F6368;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.detail-value {
  font-size: 14px;
  color: #202124;
}

.monospace {
  font-family: 'Monaco', 'Consolas', monospace;
  font-size: 13px;
}

/* Story Link */
.story-link {
  padding-top: 16px;
  border-top: 1px solid #DADCE0;
}

.link-button {
  padding: 8px 16px;
  background: #1A73E8;
  color: #FFFFFF;
  border: none;
  border-radius: 4px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}

.link-button:hover {
  background: #1765CC;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.2);
}

/* Animations */
.animate-spin {
  animation: spin 1s linear infinite;
}
</style>