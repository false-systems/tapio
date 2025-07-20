<template>
  <div class="professional-dashboard">
    <!-- Modern Header -->
    <ModernHeader 
      :activeTab="activeTab" 
      :isConnected="isConnected"
      @tab-change="handleTabChange" 
    />

    <!-- Main Content Area -->
    <main class="main-content">
      <!-- Overview Tab -->
      <div v-if="activeTab === 'overview'" class="tab-content overview-content">
        <div class="overview-grid">
          <!-- Quick Stats Cards -->
          <div class="stats-section">
            <div class="section-header">
              <h2 class="section-title">
                <svg width="20" height="20" viewBox="0 0 20 20" fill="none" class="section-icon">
                  <path d="M3 4a1 1 0 011-1h12a1 1 0 011 1v2a1 1 0 01-1 1H4a1 1 0 01-1-1V4zM3 10a1 1 0 011-1h6a1 1 0 011 1v6a1 1 0 01-1 1H4a1 1 0 01-1-1v-6zM13 9a1 1 0 00-1 1v6a1 1 0 001 1h3a1 1 0 001-1v-6a1 1 0 00-1-1h-3z" fill="currentColor"/>
                </svg>
                System Overview
              </h2>
              <div class="live-indicator" :class="{ active: isConnected }">
                <div class="live-dot"></div>
                <span>{{ isConnected ? 'Live Data' : 'Offline' }}</span>
              </div>
            </div>
            
            <div class="stats-grid">
              <div class="stat-card healthy">
                <div class="stat-icon">
                  <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
                    <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z" stroke="currentColor" stroke-width="2" fill="none"/>
                  </svg>
                </div>
                <div class="stat-content">
                  <div class="stat-value">{{ systemHealth }}%</div>
                  <div class="stat-label">System Health</div>
                  <div class="stat-change positive">+2.1% from last hour</div>
                </div>
              </div>

              <div class="stat-card warning">
                <div class="stat-icon">
                  <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
                    <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" stroke="currentColor" stroke-width="2" fill="none"/>
                  </svg>
                </div>
                <div class="stat-content">
                  <div class="stat-value">{{ activeAlerts }}</div>
                  <div class="stat-label">Active Alerts</div>
                  <div class="stat-change negative">{{ alertTrend > 0 ? '+' : '' }}{{ alertTrend }} from yesterday</div>
                </div>
              </div>

              <div class="stat-card info">
                <div class="stat-icon">
                  <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
                    <rect x="2" y="3" width="20" height="14" rx="2" ry="2" stroke="currentColor" stroke-width="2" fill="none"/>
                    <path d="M8 21l4-4 4 4" stroke="currentColor" stroke-width="2"/>
                  </svg>
                </div>
                <div class="stat-content">
                  <div class="stat-value">{{ totalServices }}</div>
                  <div class="stat-label">Total Services</div>
                  <div class="stat-change positive">{{ runningServices }} running</div>
                </div>
              </div>

              <div class="stat-card performance">
                <div class="stat-icon">
                  <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
                    <path d="M12 20V10M18 20V4M6 20v-6" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                  </svg>
                </div>
                <div class="stat-content">
                  <div class="stat-value">{{ formatThroughput(totalThroughput) }}</div>
                  <div class="stat-label">Total Throughput</div>
                  <div class="stat-change positive">Peak: {{ formatThroughput(peakThroughput) }}</div>
                </div>
              </div>
            </div>
          </div>

          <!-- Recent Activity -->
          <div class="activity-section">
            <div class="section-header">
              <h2 class="section-title">
                <svg width="20" height="20" viewBox="0 0 20 20" fill="none" class="section-icon">
                  <path d="M10 2v6l3 3M18 10a8 8 0 11-16 0 8 8 0 0116 0z" stroke="currentColor" stroke-width="1.5" fill="none"/>
                </svg>
                Recent Activity
              </h2>
              <button class="action-btn" @click="refreshActivity">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                  <path d="M13.65 2.35a8 8 0 11-11.31 11.31" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
                  <path d="M16 4l-4-1-1 4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
                </svg>
                Refresh
              </button>
            </div>
            
            <div class="activity-feed">
              <div v-for="activity in recentActivities" :key="activity.id" 
                   class="activity-item" 
                   :class="`activity-${activity.type}`">
                <div class="activity-icon">
                  <component :is="'div'" v-html="getActivityIcon(activity.type)"></component>
                </div>
                <div class="activity-content">
                  <div class="activity-title">{{ activity.title }}</div>
                  <div class="activity-description">{{ activity.description }}</div>
                  <div class="activity-time">{{ formatTimeAgo(activity.timestamp) }}</div>
                </div>
                <div class="activity-status" :class="`status-${activity.severity}`">
                  {{ activity.severity }}
                </div>
              </div>
            </div>
          </div>

          <!-- Quick Actions -->
          <div class="actions-section">
            <div class="section-header">
              <h2 class="section-title">
                <svg width="20" height="20" viewBox="0 0 20 20" fill="none" class="section-icon">
                  <path d="M13 6a3 3 0 11-6 0 3 3 0 016 0zM18 8a2 2 0 11-4 0 2 2 0 014 0zM14 15a4 4 0 00-8 0v3h8v-3z" stroke="currentColor" stroke-width="1.5" fill="none"/>
                </svg>
                Quick Actions
              </h2>
            </div>
            
            <div class="action-grid">
              <button @click="handleAction('topology')" class="quick-action-btn">
                <div class="action-icon">
                  <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
                    <circle cx="12" cy="12" r="3" stroke="currentColor" stroke-width="2"/>
                    <path d="M12 1v6m0 6v6m11-7h-6m-6 0H1" stroke="currentColor" stroke-width="2"/>
                  </svg>
                </div>
                <div class="action-content">
                  <div class="action-title">View Topology</div>
                  <div class="action-subtitle">Network service map</div>
                </div>
              </button>

              <button @click="handleAction('metrics')" class="quick-action-btn">
                <div class="action-icon">
                  <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
                    <path d="M3 3v18h18M7 12l4-4 4 4 4-4" stroke="currentColor" stroke-width="2" fill="none"/>
                  </svg>
                </div>
                <div class="action-content">
                  <div class="action-title">Advanced Metrics</div>
                  <div class="action-subtitle">Performance dashboard</div>
                </div>
              </button>

              <button @click="handleAction('incidents')" class="quick-action-btn">
                <div class="action-icon">
                  <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
                    <path d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" stroke="currentColor" stroke-width="2" fill="none"/>
                  </svg>
                </div>
                <div class="action-content">
                  <div class="action-title">Incident Response</div>
                  <div class="action-subtitle">Manage alerts & issues</div>
                </div>
              </button>

              <button @click="handleAction('traces')" class="quick-action-btn">
                <div class="action-icon">
                  <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
                    <path d="M9 12l2 2 4-4M21 12a9 9 0 11-18 0 9 9 0 0118 0z" stroke="currentColor" stroke-width="2" fill="none"/>
                  </svg>
                </div>
                <div class="action-content">
                  <div class="action-title">Distributed Traces</div>
                  <div class="action-subtitle">Request flow analysis</div>
                </div>
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- Topology Tab -->
      <div v-if="activeTab === 'topology'" class="tab-content topology-content">
        <ModernTopology />
      </div>

      <!-- Metrics Tab -->
      <div v-if="activeTab === 'metrics'" class="tab-content metrics-content">
        <AdvancedMetrics :isConnected="isConnected" />
      </div>

      <!-- Incidents Tab -->
      <div v-if="activeTab === 'incidents'" class="tab-content incidents-content">
        <div class="incidents-dashboard">
          <div class="incidents-header">
            <h1 class="page-title">Incident Management</h1>
            <div class="incidents-summary">
              <div class="summary-card critical">
                <div class="summary-number">2</div>
                <div class="summary-label">Critical</div>
              </div>
              <div class="summary-card warning">
                <div class="summary-number">5</div>
                <div class="summary-label">Warning</div>
              </div>
              <div class="summary-card resolved">
                <div class="summary-number">12</div>
                <div class="summary-label">Resolved Today</div>
              </div>
            </div>
          </div>
          
          <div class="incidents-placeholder">
            <div class="placeholder-icon">
              <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
                <path d="M24 4L44 40H4L24 4z" stroke="currentColor" stroke-width="2" fill="none"/>
                <path d="M24 18v8M24 34h.01" stroke="currentColor" stroke-width="2"/>
              </svg>
            </div>
            <h3>Advanced Incident Management</h3>
            <p>Comprehensive incident tracking, root cause analysis, and automated response workflows coming soon.</p>
          </div>
        </div>
      </div>

      <!-- Traces Tab -->
      <div v-if="activeTab === 'traces'" class="tab-content traces-content">
        <div class="traces-dashboard">
          <div class="traces-header">
            <h1 class="page-title">Distributed Tracing</h1>
            <div class="traces-controls">
              <select class="trace-filter">
                <option>Last 15 minutes</option>
                <option>Last hour</option>
                <option>Last 4 hours</option>
              </select>
              <input type="text" placeholder="Search traces..." class="trace-search" />
            </div>
          </div>
          
          <div class="traces-placeholder">
            <div class="placeholder-icon">
              <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
                <path d="M8 24h8m0 0h8m-8 0v8m0-8v-8" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                <path d="M32 16h8m-4-4v8" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                <path d="M32 32h8m-4-4v8" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
              </svg>
            </div>
            <h3>Distributed Trace Analysis</h3>
            <p>End-to-end request tracing across your entire service mesh with performance insights and error tracking.</p>
          </div>
        </div>
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import ModernHeader from './ModernHeader.vue'
import ModernTopology from './ModernTopology.vue'
import AdvancedMetrics from './AdvancedMetrics.vue'

interface Activity {
  id: string
  type: 'deployment' | 'alert' | 'performance' | 'security' | 'scaling'
  title: string
  description: string
  timestamp: Date
  severity: 'info' | 'warning' | 'error' | 'success'
}

// Component state
const activeTab = ref('overview')
const isConnected = ref(false)
const connectionCheckInterval = ref<number>()

// Dashboard metrics
const systemHealth = ref(98.2)
const activeAlerts = ref(3)
const alertTrend = ref(-1)
const totalServices = ref(12)
const runningServices = ref(11)
const totalThroughput = ref(2847)
const peakThroughput = ref(3291)

// Recent activities
const recentActivities = ref<Activity[]>([
  {
    id: 'act-1',
    type: 'performance',
    title: 'Memory leak detected in payment service',
    description: 'Automatic memory optimization triggered, issue resolved',
    timestamp: new Date(Date.now() - 2 * 60 * 1000), // 2 minutes ago
    severity: 'success'
  },
  {
    id: 'act-2',
    type: 'scaling',
    title: 'Auto-scaling triggered for user service',
    description: 'Scaled from 3 to 5 replicas due to increased load',
    timestamp: new Date(Date.now() - 5 * 60 * 1000), // 5 minutes ago
    severity: 'info'
  },
  {
    id: 'act-3',
    type: 'alert',
    title: 'High CPU usage detected',
    description: 'Auth service CPU usage above 85% threshold',
    timestamp: new Date(Date.now() - 8 * 60 * 1000), // 8 minutes ago
    severity: 'warning'
  },
  {
    id: 'act-4',
    type: 'deployment',
    title: 'New deployment completed',
    description: 'Frontend v2.1.3 deployed successfully',
    timestamp: new Date(Date.now() - 15 * 60 * 1000), // 15 minutes ago
    severity: 'success'
  },
  {
    id: 'act-5',
    type: 'security',
    title: 'Security scan completed',
    description: 'No vulnerabilities found in latest scan',
    timestamp: new Date(Date.now() - 22 * 60 * 1000), // 22 minutes ago
    severity: 'success'
  }
])

// WebSocket connection management
let websocket: WebSocket | null = null

const connectWebSocket = () => {
  try {
    websocket = new WebSocket('ws://localhost:3001/ws')
    
    websocket.onopen = () => {
      console.log('WebSocket connected')
      isConnected.value = true
    }
    
    websocket.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        handleWebSocketMessage(data)
      } catch (error) {
        console.error('Error parsing WebSocket message:', error)
      }
    }
    
    websocket.onclose = () => {
      console.log('WebSocket disconnected')
      isConnected.value = false
      // Attempt to reconnect after 3 seconds
      setTimeout(connectWebSocket, 3000)
    }
    
    websocket.onerror = (error) => {
      console.error('WebSocket error:', error)
      isConnected.value = false
    }
  } catch (error) {
    console.error('Failed to connect WebSocket:', error)
    isConnected.value = false
  }
}

const handleWebSocketMessage = (data: any) => {
  switch (data.type) {
    case 'health_update':
      systemHealth.value = Math.round(parseFloat(data.data.cpu || '98.2'))
      break
    case 'alert_update':
      if (data.data.alert_count !== undefined) {
        const newCount = parseInt(data.data.alert_count)
        alertTrend.value = newCount - activeAlerts.value
        activeAlerts.value = newCount
      }
      break
    case 'throughput_update':
      if (data.data.throughput !== undefined) {
        totalThroughput.value = parseInt(data.data.throughput)
        if (totalThroughput.value > peakThroughput.value) {
          peakThroughput.value = totalThroughput.value
        }
      }
      break
    case 'activity_update':
      // Add new activity to the feed
      if (data.data.activity) {
        const newActivity: Activity = {
          id: `act-${Date.now()}`,
          type: data.data.activity.type || 'info',
          title: data.data.activity.title || 'System Update',
          description: data.data.activity.description || 'System activity detected',
          timestamp: new Date(),
          severity: data.data.activity.severity || 'info'
        }
        recentActivities.value.unshift(newActivity)
        // Keep only the latest 10 activities
        if (recentActivities.value.length > 10) {
          recentActivities.value = recentActivities.value.slice(0, 10)
        }
      }
      break
  }
}

// Event handlers
const handleTabChange = (tabId: string) => {
  activeTab.value = tabId
}

const handleAction = (action: string) => {
  switch (action) {
    case 'topology':
      activeTab.value = 'topology'
      break
    case 'metrics':
      activeTab.value = 'metrics'
      break
    case 'incidents':
      activeTab.value = 'incidents'
      break
    case 'traces':
      activeTab.value = 'traces'
      break
  }
}

const refreshActivity = () => {
  // Simulate refresh by adding a new activity
  const refreshActivity: Activity = {
    id: `refresh-${Date.now()}`,
    type: 'performance',
    title: 'Activity feed refreshed',
    description: 'Latest system activities updated',
    timestamp: new Date(),
    severity: 'info'
  }
  recentActivities.value.unshift(refreshActivity)
  if (recentActivities.value.length > 10) {
    recentActivities.value = recentActivities.value.slice(0, 10)
  }
}

// Utility functions
const formatThroughput = (value: number): string => {
  if (value >= 1000000) {
    return `${(value / 1000000).toFixed(1)}M/s`
  } else if (value >= 1000) {
    return `${(value / 1000).toFixed(1)}k/s`
  }
  return `${value}/s`
}

const formatTimeAgo = (timestamp: Date): string => {
  const now = new Date()
  const diffMs = now.getTime() - timestamp.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  const diffHours = Math.floor(diffMs / 3600000)
  const diffDays = Math.floor(diffMs / 86400000)

  if (diffMins < 1) return 'Just now'
  if (diffMins < 60) return `${diffMins} min ago`
  if (diffHours < 24) return `${diffHours} hour${diffHours > 1 ? 's' : ''} ago`
  return `${diffDays} day${diffDays > 1 ? 's' : ''} ago`
}

const getActivityIcon = (type: string): string => {
  const icons = {
    deployment: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M8 2v12M2 8h12" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
    alert: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M8 1l7 14H1L8 1z" stroke="currentColor" stroke-width="1.5" fill="none"/><path d="M8 5v4M8 11h.01" stroke="currentColor" stroke-width="1.5"/></svg>',
    performance: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M3 13l4-4 2 2 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
    security: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M8 1l4 2v5c0 3-4 7-4 7s-4-4-4-7V3l4-2z" stroke="currentColor" stroke-width="1.5" fill="none"/></svg>',
    scaling: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M12 4v8M8 6v6M4 8v4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>'
  }
  return icons[type] || icons.deployment
}

// Lifecycle hooks
onMounted(() => {
  // Connect to WebSocket
  connectWebSocket()
  
  // Update timestamps every minute
  setInterval(() => {
    // Force reactivity update for time formatting
    recentActivities.value = [...recentActivities.value]
  }, 60000)
})

onUnmounted(() => {
  if (websocket) {
    websocket.close()
  }
  if (connectionCheckInterval.value) {
    clearInterval(connectionCheckInterval.value)
  }
})
</script>

<style scoped>
.professional-dashboard {
  min-height: 100vh;
  background: linear-gradient(135deg, #f8fafc 0%, #e2e8f0 100%);
  color: #1a1d29;
}

.main-content {
  padding-top: 64px; /* Account for fixed header */
  min-height: calc(100vh - 64px);
}

.tab-content {
  width: 100%;
  min-height: calc(100vh - 64px);
}

/* Overview Content */
.overview-content {
  padding: 32px 24px;
  max-width: 1400px;
  margin: 0 auto;
}

.overview-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 32px;
}

/* Section Headers */
.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.section-title {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 20px;
  font-weight: 700;
  color: #1a1d29;
  margin: 0;
}

.section-icon {
  color: #667eea;
}

.live-indicator {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border-radius: 20px;
  background: rgba(0, 0, 0, 0.04);
  border: 1px solid rgba(0, 0, 0, 0.08);
  font-size: 12px;
  font-weight: 600;
  color: #6b7280;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.live-indicator.active {
  background: rgba(16, 185, 129, 0.1);
  border-color: rgba(16, 185, 129, 0.3);
  color: #065f46;
}

.live-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #ef4444;
}

.live-indicator.active .live-dot {
  background: #10b981;
  animation: pulse 2s infinite;
}

@keyframes pulse {
  0%, 100% { 
    box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.7); 
  }
  50% { 
    box-shadow: 0 0 0 4px rgba(16, 185, 129, 0); 
  }
}

.action-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  border-radius: 8px;
  background: white;
  color: #6b7280;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}

.action-btn:hover {
  background: rgba(0, 0, 0, 0.04);
  color: #374151;
}

/* Stats Section */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 20px;
}

.stat-card {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 24px;
  background: white;
  border-radius: 16px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.2);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.stat-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 25px rgba(0, 0, 0, 0.15);
}

.stat-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 48px;
  height: 48px;
  border-radius: 12px;
  color: white;
}

.stat-card.healthy .stat-icon {
  background: linear-gradient(135deg, #10b981, #047857);
}

.stat-card.warning .stat-icon {
  background: linear-gradient(135deg, #f59e0b, #d97706);
}

.stat-card.info .stat-icon {
  background: linear-gradient(135deg, #3b82f6, #1d4ed8);
}

.stat-card.performance .stat-icon {
  background: linear-gradient(135deg, #8b5cf6, #7c3aed);
}

.stat-content {
  flex: 1;
}

.stat-value {
  font-size: 28px;
  font-weight: 700;
  color: #111827;
  line-height: 1.2;
}

.stat-label {
  font-size: 14px;
  font-weight: 500;
  color: #6b7280;
  margin: 4px 0;
}

.stat-change {
  font-size: 12px;
  font-weight: 600;
}

.stat-change.positive {
  color: #10b981;
}

.stat-change.negative {
  color: #ef4444;
}

/* Activity Section */
.activity-feed {
  background: white;
  border-radius: 16px;
  overflow: hidden;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

.activity-item {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 20px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.05);
  transition: background 0.2s;
}

.activity-item:last-child {
  border-bottom: none;
}

.activity-item:hover {
  background: rgba(0, 0, 0, 0.02);
}

.activity-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.04);
  color: #6b7280;
}

.activity-deployment .activity-icon {
  background: rgba(16, 185, 129, 0.1);
  color: #10b981;
}

.activity-alert .activity-icon {
  background: rgba(245, 158, 11, 0.1);
  color: #f59e0b;
}

.activity-performance .activity-icon {
  background: rgba(59, 130, 246, 0.1);
  color: #3b82f6;
}

.activity-security .activity-icon {
  background: rgba(139, 92, 246, 0.1);
  color: #8b5cf6;
}

.activity-scaling .activity-icon {
  background: rgba(34, 197, 94, 0.1);
  color: #22c55e;
}

.activity-content {
  flex: 1;
}

.activity-title {
  font-size: 14px;
  font-weight: 600;
  color: #111827;
  margin-bottom: 2px;
}

.activity-description {
  font-size: 13px;
  color: #6b7280;
  margin-bottom: 4px;
}

.activity-time {
  font-size: 11px;
  color: #9ca3af;
}

.activity-status {
  padding: 4px 8px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.activity-status.status-success {
  background: rgba(16, 185, 129, 0.1);
  color: #065f46;
}

.activity-status.status-warning {
  background: rgba(245, 158, 11, 0.1);
  color: #92400e;
}

.activity-status.status-error {
  background: rgba(239, 68, 68, 0.1);
  color: #991b1b;
}

.activity-status.status-info {
  background: rgba(59, 130, 246, 0.1);
  color: #1e40af;
}

/* Quick Actions */
.action-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 16px;
}

.quick-action-btn {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 20px;
  background: white;
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 12px;
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  text-align: left;
}

.quick-action-btn:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 25px rgba(0, 0, 0, 0.15);
  border-color: rgba(102, 126, 234, 0.2);
}

.action-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 48px;
  height: 48px;
  border-radius: 12px;
  background: linear-gradient(135deg, #667eea, #764ba2);
  color: white;
}

.action-content {
  flex: 1;
}

.action-title {
  font-size: 16px;
  font-weight: 600;
  color: #111827;
  margin-bottom: 4px;
}

.action-subtitle {
  font-size: 14px;
  color: #6b7280;
}

/* Topology Content */
.topology-content {
  height: calc(100vh - 64px);
  overflow: hidden;
}

/* Metrics Content */
.metrics-content {
  height: calc(100vh - 64px);
  overflow: hidden;
}

/* Incidents Content */
.incidents-content,
.traces-content {
  padding: 32px 24px;
  max-width: 1400px;
  margin: 0 auto;
}

.incidents-dashboard,
.traces-dashboard {
  background: white;
  border-radius: 16px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
  overflow: hidden;
}

.incidents-header,
.traces-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 24px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.08);
}

.page-title {
  font-size: 24px;
  font-weight: 700;
  color: #111827;
  margin: 0;
}

.incidents-summary {
  display: flex;
  gap: 16px;
}

.summary-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 12px 16px;
  border-radius: 8px;
  min-width: 80px;
}

.summary-card.critical {
  background: rgba(239, 68, 68, 0.1);
  color: #991b1b;
}

.summary-card.warning {
  background: rgba(245, 158, 11, 0.1);
  color: #92400e;
}

.summary-card.resolved {
  background: rgba(16, 185, 129, 0.1);
  color: #065f46;
}

.summary-number {
  font-size: 20px;
  font-weight: 700;
}

.summary-label {
  font-size: 12px;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.traces-controls {
  display: flex;
  gap: 12px;
  align-items: center;
}

.trace-filter,
.trace-search {
  padding: 8px 12px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  border-radius: 8px;
  font-size: 14px;
  background: white;
}

.trace-search {
  width: 240px;
}

.incidents-placeholder,
.traces-placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 80px 40px;
  text-align: center;
}

.placeholder-icon {
  margin-bottom: 24px;
  color: #9ca3af;
}

.incidents-placeholder h3,
.traces-placeholder h3 {
  font-size: 20px;
  font-weight: 600;
  color: #374151;
  margin: 0 0 12px 0;
}

.incidents-placeholder p,
.traces-placeholder p {
  font-size: 16px;
  color: #6b7280;
  max-width: 400px;
  line-height: 1.6;
  margin: 0;
}

/* Responsive Design */
@media (max-width: 1200px) {
  .overview-content {
    padding: 24px 16px;
  }
  
  .stats-grid {
    grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  }
}

@media (max-width: 768px) {
  .overview-content {
    padding: 16px;
  }
  
  .overview-grid {
    gap: 24px;
  }
  
  .section-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }
  
  .stats-grid {
    grid-template-columns: 1fr;
  }
  
  .action-grid {
    grid-template-columns: 1fr;
  }
  
  .stat-card {
    padding: 20px;
  }
  
  .quick-action-btn {
    padding: 16px;
  }
  
  .incidents-header,
  .traces-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 16px;
  }
  
  .incidents-summary {
    width: 100%;
    justify-content: space-between;
  }
  
  .traces-controls {
    width: 100%;
    flex-direction: column;
    align-items: stretch;
  }
  
  .trace-search {
    width: 100%;
  }
}

@media (max-width: 480px) {
  .activity-item {
    padding: 12px 16px;
  }
  
  .quick-action-btn {
    flex-direction: column;
    text-align: center;
    gap: 12px;
  }
  
  .incidents-placeholder,
  .traces-placeholder {
    padding: 40px 20px;
  }
}
</style>