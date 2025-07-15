<script lang="ts" setup>
interface ClusterStatus {
  status: string
  nodes_total: number
  nodes_ready: number
  pods_total: number
  pods_healthy: number
  stories_total: number
  stories_critical: number
  stories_resolved: number
  last_update: string
}

interface Props {
  status: ClusterStatus | null
}

defineProps<Props>()

const getStatusColor = (status: string) => {
  switch (status) {
    case 'healthy': return '#4ade80'
    case 'warning': return '#fbbf24'
    case 'critical': return '#ef4444'
    default: return '#94a3b8'
  }
}

const getStatusIcon = (status: string) => {
  switch (status) {
    case 'healthy': return '✅'
    case 'warning': return '⚠️'
    case 'critical': return '🚨'
    default: return '❓'
  }
}

const formatTimestamp = (timestamp: string) => {
  try {
    return new Date(timestamp).toLocaleTimeString()
  } catch {
    return timestamp
  }
}
</script>

<template>
  <div class="cluster-status">
    <div class="status-header">
      <h3>🏗️ Cluster Status</h3>
    </div>
    
    <div v-if="!status" class="loading-status">
      <div class="loading-spinner">⟳</div>
      <p>Loading cluster status...</p>
    </div>
    
    <div v-else class="status-content">
      <!-- Overall Status -->
      <div class="status-overview">
        <div class="status-indicator" :style="{ color: getStatusColor(status.status) }">
          {{ getStatusIcon(status.status) }}
        </div>
        <div class="status-text">
          <div class="status-label">Overall Status</div>
          <div class="status-value" :style="{ color: getStatusColor(status.status) }">
            {{ status.status.toUpperCase() }}
          </div>
        </div>
      </div>

      <!-- Resources -->
      <div class="metrics-grid">
        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-icon">🖥️</span>
            <span class="metric-label">Nodes</span>
          </div>
          <div class="metric-value">
            {{ status.nodes_ready }}/{{ status.nodes_total }}
          </div>
          <div class="metric-description">ready</div>
        </div>

        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-icon">📦</span>
            <span class="metric-label">Pods</span>
          </div>
          <div class="metric-value">
            {{ status.pods_healthy }}/{{ status.pods_total }}
          </div>
          <div class="metric-description">healthy</div>
        </div>
      </div>

      <!-- Stories Summary -->
      <div class="stories-summary">
        <div class="summary-header">
          <span class="summary-icon">📊</span>
          <span class="summary-label">Stories Overview</span>
        </div>
        
        <div class="stories-grid">
          <div class="story-metric">
            <div class="story-count">{{ status.stories_total }}</div>
            <div class="story-label">Total</div>
          </div>
          
          <div class="story-metric critical">
            <div class="story-count">{{ status.stories_critical }}</div>
            <div class="story-label">Critical</div>
          </div>
          
          <div class="story-metric resolved">
            <div class="story-count">{{ status.stories_resolved }}</div>
            <div class="story-label">Resolved</div>
          </div>
        </div>
      </div>

      <!-- Last Update -->
      <div class="last-update">
        <span class="update-icon">🕐</span>
        <span class="update-text">Last updated: {{ formatTimestamp(status.last_update) }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.cluster-status {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.status-header {
  padding: var(--space-md) var(--space-lg);
  border-bottom: 1px solid var(--color-border-primary);
  flex-shrink: 0;
  background: var(--color-bg-tertiary);
  min-height: 48px;
  display: flex;
  align-items: center;
}

.status-header h3 {
  margin: 0;
  font-size: 1rem;
  font-weight: 600;
  color: var(--color-text-primary);
  line-height: 1.4;
}

.loading-status {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: #94a3b8;
  gap: 1rem;
}

.loading-spinner {
  font-size: 1.5rem;
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.status-content {
  flex: 1;
  padding: var(--space-lg);
  display: flex;
  flex-direction: column;
  gap: var(--space-lg);
  overflow-y: auto;
}

.status-overview {
  display: flex;
  align-items: center;
  gap: var(--space-md);
  padding: var(--space-lg);
  background: var(--color-surface-primary);
  border-radius: var(--radius-md);
  border: 1px solid var(--color-border-primary);
  transition: all 0.2s ease;
}

.status-overview:hover {
  background: var(--color-surface-secondary);
  border-color: var(--color-border-secondary);
}

.status-indicator {
  font-size: 1.5rem;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
}

.status-text {
  flex: 1;
}

.status-label {
  font-size: 0.75rem;
  color: var(--color-text-tertiary);
  margin-bottom: var(--space-xs);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  font-weight: 500;
}

.status-value {
  font-size: 1.125rem;
  font-weight: 600;
  color: var(--color-text-primary);
}

.metrics-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 1rem;
}

.metric-card {
  background: var(--color-surface-primary);
  border: 1px solid var(--color-border-primary);
  border-radius: var(--radius-md);
  padding: var(--space-lg);
  transition: all 0.2s ease;
}

.metric-card:hover {
  background: var(--color-surface-secondary);
  border-color: var(--color-border-secondary);
}

.metric-header {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.75rem;
}

.metric-icon {
  font-size: 1.1rem;
}

.metric-label {
  font-size: 0.85rem;
  color: #94a3b8;
}

.metric-value {
  font-size: 1.5rem;
  font-weight: 700;
  color: var(--color-text-primary);
  margin-bottom: var(--space-xs);
  line-height: 1;
}

.metric-description {
  font-size: 0.8rem;
  color: #94a3b8;
}

.stories-summary {
  background: linear-gradient(135deg, rgba(15, 52, 96, 0.5) 0%, rgba(30, 64, 175, 0.5) 100%);
  backdrop-filter: blur(10px);
  border: 1px solid rgba(74, 222, 128, 0.2);
  border-radius: 12px;
  padding: 1.25rem;
  box-shadow: 
    0 4px 15px rgba(0, 0, 0, 0.2),
    inset 0 1px 0 rgba(255, 255, 255, 0.05);
  transition: all 0.3s ease;
}

.stories-summary:hover {
  border-color: rgba(74, 222, 128, 0.4);
  box-shadow: 
    0 6px 20px rgba(0, 0, 0, 0.25),
    0 0 15px rgba(74, 222, 128, 0.15),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
}

.summary-header {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 1rem;
}

.summary-icon {
  font-size: 1.1rem;
}

.summary-label {
  font-size: 0.9rem;
  color: #94a3b8;
}

.stories-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 0.75rem;
}

.story-metric {
  text-align: center;
  padding: 1rem;
  background: linear-gradient(135deg, rgba(26, 26, 46, 0.8) 0%, rgba(55, 65, 81, 0.8) 100%);
  backdrop-filter: blur(5px);
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  transition: all 0.3s ease;
  cursor: pointer;
}

.story-metric:hover {
  transform: translateY(-2px);
  background: linear-gradient(135deg, rgba(26, 26, 46, 0.9) 0%, rgba(55, 65, 81, 0.9) 100%);
  box-shadow: 0 4px 15px rgba(0, 0, 0, 0.3);
}

.story-metric.critical {
  border-color: #ef4444;
}

.story-metric.resolved {
  border-color: #4ade80;
}

.story-count {
  font-size: 1.6rem;
  font-weight: 700;
  color: #ffffff;
  margin-bottom: 0.25rem;
  text-shadow: 0 0 10px currentColor;
}

.story-metric.critical .story-count {
  color: #ef4444;
}

.story-metric.resolved .story-count {
  color: #4ade80;
}

.story-label {
  font-size: 0.8rem;
  color: #94a3b8;
}

.last-update {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 1rem;
  background: linear-gradient(135deg, rgba(15, 52, 96, 0.4) 0%, rgba(30, 64, 175, 0.4) 100%);
  backdrop-filter: blur(5px);
  border-radius: 10px;
  border: 1px solid rgba(74, 222, 128, 0.2);
  margin-top: auto;
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.15);
}

.update-icon {
  font-size: 0.9rem;
}

.update-text {
  font-size: 0.8rem;
  color: #94a3b8;
}
</style>