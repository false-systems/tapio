<script lang="ts" setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { GetStories, GetClusterStatus, ApplyFix } from '../../wailsjs/go/main/App'

interface Story {
  id: string
  title: string
  description: string
  severity: string
  category: string
  timestamp: string
  resources: Resource[]
  actions: Action[]
  root_cause?: string
  prediction?: string
}

interface Resource {
  type: string
  name: string
  namespace: string
}

interface Action {
  id: string
  title: string
  description: string
  commands: string[]
  risk: string
  auto_apply: boolean
}

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

const stories = ref<Story[]>([])
const clusterStatus = ref<ClusterStatus | null>(null)
const loading = ref(true)
const selectedStory = ref<Story | null>(null)
const refreshInterval = ref<number | null>(null)

const startAutoRefresh = () => {
  refreshInterval.value = window.setInterval(async () => {
    await loadStories()
  }, 30000) // 30 seconds
}

const stopAutoRefresh = () => {
  if (refreshInterval.value) {
    clearInterval(refreshInterval.value)
    refreshInterval.value = null
  }
}

const loadStories = async () => {
  try {
    const storiesData = await GetStories()
    stories.value = storiesData || []
  } catch (err) {
    console.error('Error loading stories:', err)
  }
}

const loadClusterStatus = async () => {
  try {
    const status = await GetClusterStatus()
    clusterStatus.value = status
  } catch (err) {
    console.error('Error loading cluster status:', err)
  }
}

const handleApplyFix = async (storyId: string, actionId: string) => {
  try {
    await ApplyFix(storyId, actionId)
    await loadStories()
  } catch (err) {
    console.error('Error applying fix:', err)
  }
}

const refresh = async () => {
  loading.value = true
  await Promise.all([loadStories(), loadClusterStatus()])
  loading.value = false
}

const formatTimestamp = (timestamp: string) => {
  try {
    const date = new Date(timestamp)
    const now = new Date()
    const diff = now.getTime() - date.getTime()
    const minutes = Math.floor(diff / 60000)
    const hours = Math.floor(diff / 3600000)
    const days = Math.floor(diff / 86400000)
    
    if (minutes < 1) return 'just now'
    if (minutes < 60) return `${minutes}m ago`
    if (hours < 24) return `${hours}h ago`
    return `${days}d ago`
  } catch {
    return 'unknown'
  }
}

onMounted(async () => {
  await refresh()
  startAutoRefresh()
})

onUnmounted(() => {
  stopAutoRefresh()
})
</script>

<template>
  <div class="dashboard">
    <!-- Clean Header -->
    <header class="header">
      <div class="header-brand">
        <h1 class="brand-name">Tapio</h1>
      </div>
      
      <nav class="header-nav">
        <button class="nav-item active">Stories</button>
        <button class="nav-item">Insights</button>
        <button class="nav-item">Settings</button>
      </nav>
      
      <div class="header-actions">
        <button @click="refresh" class="btn-icon" :disabled="loading">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
            <path d="M13.65 2.35A8 8 0 1 0 16 8h-1.5A6.5 6.5 0 1 1 8 1.5V0l4 4-4 4V5.5" 
                  stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
                  :class="{ 'animate-spin': loading }"/>
          </svg>
        </button>
      </div>
    </header>

    <!-- Main Content -->
    <main class="main">
      <div class="container">
        <!-- Clean Stats Bar -->
        <div class="stats-bar">
          <div class="stat">
            <span class="stat-value">{{ stories.length }}</span>
            <span class="stat-label">Active Stories</span>
          </div>
          <div class="stat">
            <span class="stat-value critical">{{ stories.filter(s => s.severity === 'critical').length }}</span>
            <span class="stat-label">Critical</span>
          </div>
          <div class="stat">
            <span class="stat-value warning">{{ stories.filter(s => s.severity === 'high').length }}</span>
            <span class="stat-label">High Priority</span>
          </div>
          <div class="stat">
            <span class="stat-value">{{ clusterStatus?.pods_healthy || 0 }}/{{ clusterStatus?.pods_total || 0 }}</span>
            <span class="stat-label">Healthy Pods</span>
          </div>
        </div>

        <!-- Stories List -->
        <div class="stories-container">
          <div class="stories-header">
            <h2 class="section-title">Active Stories</h2>
            <span class="text-sm text-gray-500">Real-time cluster insights</span>
          </div>

          <div class="stories-list">
            <div v-if="loading && stories.length === 0" class="empty-state">
              <div class="spinner"></div>
              <p class="text-gray-500">Loading stories...</p>
            </div>

            <div v-else-if="stories.length === 0" class="empty-state">
              <svg width="48" height="48" viewBox="0 0 48 48" fill="none" class="empty-icon">
                <circle cx="24" cy="24" r="20" stroke="var(--gray-300)" stroke-width="2"/>
                <path d="M24 14v10M24 28v.01" stroke="var(--gray-400)" stroke-width="2" stroke-linecap="round"/>
              </svg>
              <p class="text-gray-500">No active stories</p>
              <p class="text-sm text-gray-400">Your cluster is healthy</p>
            </div>

            <div v-else class="story-cards">
              <article 
                v-for="story in stories" 
                :key="story.id"
                class="story-card"
                :class="{ selected: selectedStory?.id === story.id }"
                @click="selectedStory = selectedStory?.id === story.id ? null : story"
              >
                <div class="story-header">
                  <div class="story-meta">
                    <span class="severity-indicator" :class="story.severity"></span>
                    <span class="severity-label">{{ story.severity }}</span>
                    <span class="divider">·</span>
                    <span class="category">{{ story.category }}</span>
                    <span class="divider">·</span>
                    <span class="timestamp">{{ formatTimestamp(story.timestamp) }}</span>
                  </div>
                </div>

                <h3 class="story-title">{{ story.title }}</h3>
                <p class="story-description">{{ story.description }}</p>

                <!-- Expanded Details -->
                <div v-if="selectedStory?.id === story.id" class="story-details">
                  <div v-if="story.root_cause" class="detail-section">
                    <h4 class="detail-title">Root Cause</h4>
                    <p class="detail-content">{{ story.root_cause }}</p>
                  </div>

                  <div v-if="story.prediction" class="detail-section">
                    <h4 class="detail-title">Prediction</h4>
                    <p class="detail-content">{{ story.prediction }}</p>
                  </div>

                  <div v-if="story.resources.length > 0" class="detail-section">
                    <h4 class="detail-title">Affected Resources</h4>
                    <div class="resources">
                      <div v-for="resource in story.resources" :key="`${resource.type}-${resource.name}`" class="resource">
                        <span class="resource-type">{{ resource.type }}</span>
                        <span class="resource-name">{{ resource.name }}</span>
                        <span v-if="resource.namespace" class="resource-namespace">{{ resource.namespace }}</span>
                      </div>
                    </div>
                  </div>

                  <div v-if="story.actions.length > 0" class="detail-section">
                    <h4 class="detail-title">Recommended Actions</h4>
                    <div class="actions">
                      <div v-for="action in story.actions" :key="action.id" class="action">
                        <div class="action-header">
                          <span class="action-title">{{ action.title }}</span>
                          <span class="action-risk" :class="action.risk">{{ action.risk }} risk</span>
                        </div>
                        <p class="action-description">{{ action.description }}</p>
                        <button 
                          @click.stop="handleApplyFix(story.id, action.id)"
                          class="btn-primary"
                        >
                          Apply Fix
                        </button>
                      </div>
                    </div>
                  </div>
                </div>
              </article>
            </div>
          </div>
        </div>
      </div>
    </main>
  </div>
</template>

<style scoped>
/* Clean, Professional Dashboard */
.dashboard {
  height: 100vh;
  display: flex;
  flex-direction: column;
  background: white;
}

/* Header */
.header {
  height: 56px;
  border-bottom: 1px solid var(--gray-200);
  background: white;
  display: flex;
  align-items: center;
  padding: 0 24px;
  flex-shrink: 0;
}

.header-brand {
  margin-right: 48px;
}

.brand-name {
  font-size: 18px;
  font-weight: 600;
  color: var(--gray-900);
  margin: 0;
}

.header-nav {
  display: flex;
  gap: 4px;
  flex: 1;
}

.nav-item {
  padding: 6px 12px;
  background: none;
  border: none;
  color: var(--gray-600);
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  border-radius: var(--radius-md);
  transition-property: color, background-color;
  transition-duration: 150ms;
}

.nav-item:hover {
  color: var(--gray-900);
  background: var(--gray-100);
}

.nav-item.active {
  color: var(--gray-900);
  background: var(--gray-100);
}

.header-actions {
  display: flex;
  gap: 8px;
}

.btn-icon {
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: 1px solid var(--gray-200);
  border-radius: var(--radius-md);
  color: var(--gray-600);
  cursor: pointer;
  transition-property: color, background-color, border-color;
  transition-duration: 150ms;
}

.btn-icon:hover {
  background: var(--gray-50);
  border-color: var(--gray-300);
  color: var(--gray-900);
}

.btn-icon:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.animate-spin {
  animation: spin 1s linear infinite;
}

/* Main Content */
.main {
  flex: 1;
  overflow-y: auto;
  background: var(--gray-50);
}

.container {
  max-width: 1200px;
  margin: 0 auto;
  padding: 32px;
}

/* Stats Bar */
.stats-bar {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 24px;
  margin-bottom: 32px;
}

.stat {
  background: white;
  padding: 20px 24px;
  border-radius: var(--radius-lg);
  border: 1px solid var(--gray-200);
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.stat-value {
  font-size: 24px;
  font-weight: 600;
  color: var(--gray-900);
  font-variant-numeric: tabular-nums;
}

.stat-value.critical {
  color: var(--red-600);
}

.stat-value.warning {
  color: var(--amber-600);
}

.stat-label {
  font-size: 14px;
  color: var(--gray-500);
}

/* Stories */
.stories-container {
  background: white;
  border-radius: var(--radius-lg);
  border: 1px solid var(--gray-200);
  overflow: hidden;
}

.stories-header {
  padding: 24px;
  border-bottom: 1px solid var(--gray-200);
}

.section-title {
  font-size: 18px;
  font-weight: 600;
  color: var(--gray-900);
  margin: 0 0 4px 0;
}

.stories-list {
  padding: 24px;
}

.empty-state {
  text-align: center;
  padding: 48px 24px;
}

.empty-icon {
  margin-bottom: 16px;
}

.spinner {
  width: 32px;
  height: 32px;
  border: 2px solid var(--gray-200);
  border-top-color: var(--gray-600);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
  margin: 0 auto 16px;
}

/* Story Cards */
.story-cards {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.story-card {
  padding: 20px;
  border: 1px solid var(--gray-200);
  border-radius: var(--radius-md);
  background: white;
  cursor: pointer;
  transition-property: border-color, box-shadow;
  transition-duration: 150ms;
}

.story-card:hover {
  border-color: var(--gray-300);
  box-shadow: var(--shadow-sm);
}

.story-card.selected {
  border-color: var(--blue-500);
  box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.1);
}

.story-header {
  margin-bottom: 8px;
}

.story-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--gray-500);
}

.severity-indicator {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--gray-400);
}

.severity-indicator.critical {
  background: var(--red-600);
}

.severity-indicator.high {
  background: var(--amber-600);
}

.severity-indicator.medium {
  background: var(--amber-500);
}

.severity-indicator.low {
  background: var(--blue-500);
}

.severity-label {
  font-weight: 500;
  text-transform: capitalize;
}

.divider {
  color: var(--gray-300);
}

.story-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--gray-900);
  margin: 0 0 4px 0;
}

.story-description {
  font-size: 14px;
  color: var(--gray-600);
  margin: 0;
  line-height: 1.5;
}

/* Story Details */
.story-details {
  margin-top: 20px;
  padding-top: 20px;
  border-top: 1px solid var(--gray-200);
  animation: fadeIn 200ms ease-out;
}

@keyframes fadeIn {
  from {
    opacity: 0;
    transform: translateY(-4px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.detail-section {
  margin-bottom: 20px;
}

.detail-section:last-child {
  margin-bottom: 0;
}

.detail-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--gray-700);
  margin: 0 0 8px 0;
  text-transform: uppercase;
  letter-spacing: 0.025em;
}

.detail-content {
  font-size: 14px;
  color: var(--gray-600);
  margin: 0;
  line-height: 1.5;
}

/* Resources */
.resources {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.resource {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}

.resource-type {
  color: var(--gray-500);
  font-weight: 500;
  background: var(--gray-100);
  padding: 2px 8px;
  border-radius: var(--radius-sm);
}

.resource-name {
  color: var(--gray-900);
  font-family: var(--font-mono);
}

.resource-namespace {
  color: var(--gray-500);
}

/* Actions */
.actions {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.action {
  padding: 16px;
  background: var(--gray-50);
  border-radius: var(--radius-md);
  border: 1px solid var(--gray-200);
}

.action-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.action-title {
  font-weight: 600;
  color: var(--gray-900);
}

.action-risk {
  font-size: 12px;
  font-weight: 500;
  padding: 2px 8px;
  border-radius: var(--radius-sm);
}

.action-risk.low {
  background: var(--green-500);
  color: white;
}

.action-risk.medium {
  background: var(--amber-500);
  color: white;
}

.action-risk.high {
  background: var(--red-500);
  color: white;
}

.action-description {
  font-size: 13px;
  color: var(--gray-600);
  margin: 0 0 12px 0;
}

.btn-primary {
  background: var(--gray-900);
  color: white;
  border: none;
  padding: 8px 16px;
  border-radius: var(--radius-md);
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition-property: background-color;
  transition-duration: 150ms;
}

.btn-primary:hover {
  background: var(--gray-800);
}
</style>