<template>
  <div class="advanced-metrics">
    <!-- Page Header -->
    <div class="page-header">
      <div class="header-content">
        <div class="title-section">
          <h1 class="page-title">Infrastructure Metrics</h1>
          <p class="page-subtitle">Real-time observability across your entire stack</p>
        </div>
        
        <div class="header-actions">
          <div class="time-selector">
            <select v-model="selectedTimeRange" class="time-select">
              <option value="5m">Last 5 minutes</option>
              <option value="15m">Last 15 minutes</option>
              <option value="1h">Last hour</option>
              <option value="6h">Last 6 hours</option>
              <option value="24h">Last 24 hours</option>
              <option value="7d">Last 7 days</option>
            </select>
          </div>
          
          <button @click="refreshData" class="refresh-btn" :class="{ loading: isRefreshing }">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none" :class="{ 'animate-spin': isRefreshing }">
              <path d="M13.65 2.35A8 8 0 1 0 16 8h-1.5A6.5 6.5 0 1 1 8 1.5V0l4 4-4 4V5.5" 
                    stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
            </svg>
            {{ isRefreshing ? 'Refreshing...' : 'Refresh' }}
          </button>
        </div>
      </div>
    </div>

    <!-- KPI Overview Cards -->
    <div class="kpi-section">
      <div class="kpi-grid">
        <div v-for="kpi in kpiMetrics" :key="kpi.id" 
             class="kpi-card" 
             :class="`kpi-${kpi.status}`">
          <div class="kpi-header">
            <div class="kpi-icon-container">
              <div class="kpi-icon" v-html="kpi.icon"></div>
            </div>
            <div class="kpi-trend" :class="`trend-${kpi.trend}`">
              <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
                <path v-if="kpi.trend === 'up'" d="M5 15l5-5 5 5" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                <path v-else-if="kpi.trend === 'down'" d="M5 5l5 5 5-5" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                <path v-else d="M5 10h10" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
              </svg>
              <span class="trend-value">{{ kpi.change }}%</span>
            </div>
          </div>
          
          <div class="kpi-content">
            <div class="kpi-value">{{ kpi.value }}</div>
            <div class="kpi-label">{{ kpi.label }}</div>
            <div class="kpi-target" v-if="kpi.target">
              Target: <span class="target-value">{{ kpi.target }}</span>
            </div>
          </div>
          
          <div class="kpi-sparkline">
            <svg width="100%" height="48" viewBox="0 0 200 48" class="sparkline-svg">
              <defs>
                <linearGradient :id="`gradient-${kpi.id}`" x1="0%" y1="0%" x2="0%" y2="100%">
                  <stop offset="0%" :style="`stop-color:${getKpiColor(kpi.status)};stop-opacity:0.3`" />
                  <stop offset="100%" :style="`stop-color:${getKpiColor(kpi.status)};stop-opacity:0.05`" />
                </linearGradient>
              </defs>
              
              <path 
                :d="getSparklinePath(kpi.sparklineData)" 
                fill="none" 
                :stroke="getKpiColor(kpi.status)" 
                stroke-width="2"
                class="sparkline-line"
              />
              
              <path 
                :d="getSparklineAreaPath(kpi.sparklineData)" 
                :fill="`url(#gradient-${kpi.id})`"
                class="sparkline-area"
              />
              
              <circle 
                v-if="kpi.sparklineData.length > 0"
                :cx="190" 
                :cy="getLastPointY(kpi.sparklineData)" 
                r="4" 
                :fill="getKpiColor(kpi.status)"
                class="sparkline-dot"
              />
            </svg>
          </div>
        </div>
      </div>
    </div>

    <!-- Charts Section -->
    <div class="charts-section">
      <!-- Primary Chart -->
      <div class="chart-container primary-chart">
        <div class="chart-header">
          <div class="chart-title-section">
            <h3 class="chart-title">Resource Utilization Trends</h3>
            <p class="chart-subtitle">CPU, Memory, and Network usage over time</p>
          </div>
          
          <div class="chart-controls">
            <div class="legend-controls">
              <button v-for="metric in chartMetrics" :key="metric.id"
                      @click="toggleMetric(metric.id)"
                      :class="['legend-item', { active: metric.visible }]"
                      :style="{ '--color': metric.color }">
                <div class="legend-dot"></div>
                <span class="legend-label">{{ metric.label }}</span>
              </button>
            </div>
            
            <div class="chart-actions">
              <button class="chart-action-btn">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                  <path d="M3 9l4-4 4 4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
                </svg>
                Expand
              </button>
              
              <button class="chart-action-btn">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                  <path d="M8 1v6M5 4l3-3 3 3M3 10v3a1 1 0 001 1h8a1 1 0 001-1v-3" 
                        stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
                </svg>
                Export
              </button>
            </div>
          </div>
        </div>
        
        <div class="chart-content">
          <div class="chart-canvas">
            <svg class="main-chart" viewBox="0 0 800 300">
              <!-- Grid -->
              <defs>
                <pattern id="chart-grid" width="80" height="60" patternUnits="userSpaceOnUse">
                  <path d="M 80 0 L 0 0 0 60" fill="none" stroke="rgba(99, 102, 241, 0.1)" stroke-width="1"/>
                </pattern>
                
                <!-- Gradients for each metric -->
                <linearGradient id="cpu-gradient" x1="0%" y1="0%" x2="0%" y2="100%">
                  <stop offset="0%" style="stop-color:#3b82f6;stop-opacity:0.4" />
                  <stop offset="100%" style="stop-color:#3b82f6;stop-opacity:0.05" />
                </linearGradient>
                
                <linearGradient id="memory-gradient" x1="0%" y1="0%" x2="0%" y2="100%">
                  <stop offset="0%" style="stop-color:#10b981;stop-opacity:0.4" />
                  <stop offset="100%" style="stop-color:#10b981;stop-opacity:0.05" />
                </linearGradient>
                
                <linearGradient id="network-gradient" x1="0%" y1="0%" x2="0%" y2="100%">
                  <stop offset="0%" style="stop-color:#f59e0b;stop-opacity:0.4" />
                  <stop offset="100%" style="stop-color:#f59e0b;stop-opacity:0.05" />
                </linearGradient>
              </defs>
              
              <rect width="100%" height="100%" fill="url(#chart-grid)" opacity="0.6"/>
              
              <!-- Y-axis labels -->
              <g class="y-axis">
                <text x="40" y="25" class="axis-label">100%</text>
                <text x="40" y="85" class="axis-label">75%</text>
                <text x="40" y="145" class="axis-label">50%</text>
                <text x="40" y="205" class="axis-label">25%</text>
                <text x="40" y="265" class="axis-label">0%</text>
              </g>
              
              <!-- Chart areas -->
              <g class="chart-areas">
                <path v-if="chartMetrics[0].visible"
                      :d="getCpuAreaPath()" 
                      fill="url(#cpu-gradient)"
                      class="chart-area cpu-area" />
                      
                <path v-if="chartMetrics[1].visible"
                      :d="getMemoryAreaPath()" 
                      fill="url(#memory-gradient)"
                      class="chart-area memory-area" />
                      
                <path v-if="chartMetrics[2].visible"
                      :d="getNetworkAreaPath()" 
                      fill="url(#network-gradient)"
                      class="chart-area network-area" />
              </g>
              
              <!-- Chart lines -->
              <g class="chart-lines">
                <path v-if="chartMetrics[0].visible"
                      :d="getCpuPath()" 
                      fill="none" 
                      stroke="#3b82f6" 
                      stroke-width="2.5"
                      class="chart-line cpu-line" />
                      
                <path v-if="chartMetrics[1].visible"
                      :d="getMemoryPath()" 
                      fill="none" 
                      stroke="#10b981" 
                      stroke-width="2.5"
                      class="chart-line memory-line" />
                      
                <path v-if="chartMetrics[2].visible"
                      :d="getNetworkPath()" 
                      fill="none" 
                      stroke="#f59e0b" 
                      stroke-width="2.5"
                      class="chart-line network-line" />
              </g>
              
              <!-- Data points -->
              <g class="data-points">
                <circle v-if="chartMetrics[0].visible" v-for="(point, index) in cpuData" :key="`cpu-${index}`"
                        :cx="60 + index * 72" :cy="40 + (100 - point) * 2.2" r="3" 
                        fill="#3b82f6" class="data-point cpu-point" />
                        
                <circle v-if="chartMetrics[1].visible" v-for="(point, index) in memoryData" :key="`memory-${index}`"
                        :cx="60 + index * 72" :cy="40 + (100 - point) * 2.2" r="3" 
                        fill="#10b981" class="data-point memory-point" />
                        
                <circle v-if="chartMetrics[2].visible" v-for="(point, index) in networkData" :key="`network-${index}`"
                        :cx="60 + index * 72" :cy="40 + (100 - point) * 2.2" r="3" 
                        fill="#f59e0b" class="data-point network-point" />
              </g>
            </svg>
          </div>
        </div>
      </div>

      <!-- Secondary Charts Grid -->
      <div class="secondary-charts">
        <!-- Service Health Distribution -->
        <div class="chart-container">
          <div class="chart-header">
            <h4 class="chart-title">Service Health</h4>
            <div class="chart-value">
              <span class="value-number">{{ serviceHealth.healthy }}</span>
              <span class="value-total">/{{ serviceHealth.total }}</span>
            </div>
          </div>
          
          <div class="chart-content">
            <div class="donut-chart-container">
              <svg class="donut-chart" viewBox="0 0 120 120">
                <circle cx="60" cy="60" r="45" fill="none" stroke="rgba(156, 163, 175, 0.2)" stroke-width="12"/>
                
                <circle cx="60" cy="60" r="45" fill="none" stroke="#10b981" stroke-width="12"
                        stroke-dasharray="283" :stroke-dashoffset="283 - (serviceHealth.healthPercentage / 100) * 283"
                        class="donut-progress" transform="rotate(-90 60 60)" />
                        
                <text x="60" y="60" text-anchor="middle" dy="6" class="donut-percentage">
                  {{ serviceHealth.healthPercentage }}%
                </text>
              </svg>
              
              <div class="donut-legend">
                <div class="legend-item">
                  <div class="legend-dot healthy"></div>
                  <span class="legend-text">Healthy ({{ serviceHealth.healthy }})</span>
                </div>
                <div class="legend-item">
                  <div class="legend-dot warning"></div>
                  <span class="legend-text">Warning ({{ serviceHealth.warning }})</span>
                </div>
                <div class="legend-item">
                  <div class="legend-dot error"></div>
                  <span class="legend-text">Error ({{ serviceHealth.error }})</span>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- Response Time Trends -->
        <div class="chart-container">
          <div class="chart-header">
            <h4 class="chart-title">Response Times</h4>
            <div class="chart-value">
              <span class="value-number">{{ averageResponseTime }}</span>
              <span class="value-unit">ms</span>
            </div>
          </div>
          
          <div class="chart-content">
            <div class="response-time-chart">
              <svg viewBox="0 0 200 80" class="response-chart">
                <path :d="getResponseTimePath()" fill="none" stroke="#6366f1" stroke-width="2" class="response-line"/>
                
                <circle v-for="(time, index) in responseTimeData" :key="index"
                        :cx="10 + index * 18" :cy="70 - (time / 500) * 60" r="2.5" 
                        fill="#6366f1" class="response-point" />
              </svg>
            </div>
          </div>
        </div>

        <!-- Error Rate -->
        <div class="chart-container">
          <div class="chart-header">
            <h4 class="chart-title">Error Rate</h4>
            <div class="chart-value" :class="{ critical: errorRate > 5 }">
              <span class="value-number">{{ errorRate }}</span>
              <span class="value-unit">%</span>
            </div>
          </div>
          
          <div class="chart-content">
            <div class="error-rate-display">
              <div class="error-bar">
                <div class="error-fill" :style="{ width: `${Math.min(errorRate, 10) * 10}%` }"></div>
              </div>
              
              <div class="error-details">
                <div class="error-item">
                  <span class="error-type">4xx Errors</span>
                  <span class="error-count">{{ errorBreakdown.client }}</span>
                </div>
                <div class="error-item">
                  <span class="error-type">5xx Errors</span>
                  <span class="error-count">{{ errorBreakdown.server }}</span>
                </div>
                <div class="error-item">
                  <span class="error-type">Timeouts</span>
                  <span class="error-count">{{ errorBreakdown.timeout }}</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Service Table -->
    <div class="service-table-container">
      <div class="table-header">
        <h3 class="table-title">Service Performance</h3>
        <div class="table-actions">
          <button class="table-action-btn">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
              <path d="M2 4.5h12M4 4.5v-2a1 1 0 011-1h6a1 1 0 011 1v2M6 7v6M10 7v6" 
                    stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
            </svg>
            Filter
          </button>
          
          <button class="table-action-btn">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
              <path d="M8 1v6M5 4l3-3 3 3M3 10v3a1 1 0 001 1h8a1 1 0 001-1v-3" 
                    stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
            </svg>
            Export
          </button>
        </div>
      </div>
      
      <div class="table-content">
        <table class="service-table">
          <thead>
            <tr>
              <th>Service</th>
              <th>Status</th>
              <th>CPU</th>
              <th>Memory</th>
              <th>Requests/min</th>
              <th>Error Rate</th>
              <th>Latency (p95)</th>
              <th>Uptime</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="service in serviceData" :key="service.name" class="service-row">
              <td class="service-name">
                <div class="service-info">
                  <div class="service-icon" :class="`icon-${service.type}`"></div>
                  <div class="service-details">
                    <span class="name">{{ service.name }}</span>
                    <span class="namespace">{{ service.namespace }}</span>
                  </div>
                </div>
              </td>
              
              <td class="service-status">
                <span class="status-badge" :class="`status-${service.status}`">
                  {{ service.status }}
                </span>
              </td>
              
              <td class="metric-cpu">
                <div class="metric-display">
                  <div class="metric-bar">
                    <div class="metric-fill cpu-fill" :style="{ width: `${service.cpu}%` }"></div>
                  </div>
                  <span class="metric-value">{{ service.cpu }}%</span>
                </div>
              </td>
              
              <td class="metric-memory">
                <div class="metric-display">
                  <div class="metric-bar">
                    <div class="metric-fill memory-fill" :style="{ width: `${service.memory}%` }"></div>
                  </div>
                  <span class="metric-value">{{ service.memory }}%</span>
                </div>
              </td>
              
              <td class="metric-requests">
                <span class="metric-number">{{ service.requestsPerMin.toLocaleString() }}</span>
              </td>
              
              <td class="metric-errors" :class="{ critical: service.errorRate > 5 }">
                <span class="metric-number">{{ service.errorRate }}%</span>
              </td>
              
              <td class="metric-latency">
                <span class="metric-number">{{ service.latencyP95 }}ms</span>
              </td>
              
              <td class="metric-uptime">
                <span class="metric-number">{{ service.uptime }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'

// Component state
const selectedTimeRange = ref('1h')
const isRefreshing = ref(false)

// KPI Metrics
const kpiMetrics = ref([
  {
    id: 'health',
    label: 'System Health Score',
    value: '98.7%',
    target: '99.5%',
    change: 2.3,
    trend: 'up',
    status: 'healthy',
    icon: '<svg width="24" height="24" viewBox="0 0 24 24" fill="none"><path d="M12 21.35l-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z" fill="currentColor"/></svg>',
    sparklineData: [94, 95, 97, 96, 98, 99, 98.7]
  },
  {
    id: 'cpu',
    label: 'CPU Utilization',
    value: '67%',
    target: '75%',
    change: -1.8,
    trend: 'down',
    status: 'healthy',
    icon: '<svg width="24" height="24" viewBox="0 0 24 24" fill="none"><rect x="4" y="4" width="16" height="16" rx="2" stroke="currentColor" stroke-width="2" fill="none"/><rect x="7" y="7" width="10" height="10" rx="1" fill="currentColor" fill-opacity="0.2"/><path d="M8 2v2M16 2v2M8 20v2M16 20v2M2 8h2M2 16h2M20 8h2M20 16h2" stroke="currentColor" stroke-width="1.5"/></svg>',
    sparklineData: [72, 69, 71, 68, 67, 66, 67]
  },
  {
    id: 'memory',
    label: 'Memory Usage',
    value: '82%',
    target: '85%',
    change: 4.2,
    trend: 'up',
    status: 'warning',
    icon: '<svg width="24" height="24" viewBox="0 0 24 24" fill="none"><rect x="3" y="4" width="18" height="16" rx="2" stroke="currentColor" stroke-width="2" fill="none"/><path d="M7 8h10M7 12h10M7 16h6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
    sparklineData: [76, 78, 79, 81, 80, 82, 82]
  },
  {
    id: 'throughput',
    label: 'Request Throughput',
    value: '2.4K/min',
    change: 12.5,
    trend: 'up',
    status: 'healthy',
    icon: '<svg width="24" height="24" viewBox="0 0 24 24" fill="none"><path d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2H5a2 2 0 00-2 2z" stroke="currentColor" stroke-width="2" fill="none"/><path d="M8 21v-4a2 2 0 012-2h4a2 2 0 012 2v4" stroke="currentColor" stroke-width="2" fill="none"/></svg>',
    sparklineData: [2100, 2200, 2150, 2300, 2280, 2400, 2400]
  }
])

// Chart data
const cpuData = ref([68, 72, 69, 71, 68, 67, 66, 67, 69, 68])
const memoryData = ref([76, 78, 79, 81, 80, 82, 81, 82, 83, 82])
const networkData = ref([45, 48, 52, 49, 51, 54, 53, 55, 52, 54])

const chartMetrics = ref([
  { id: 'cpu', label: 'CPU', color: '#3b82f6', visible: true },
  { id: 'memory', label: 'Memory', color: '#10b981', visible: true },
  { id: 'network', label: 'Network', color: '#f59e0b', visible: true }
])

// Service health data
const serviceHealth = ref({
  healthy: 23,
  warning: 4,
  error: 1,
  total: 28,
  healthPercentage: 82
})

// Response time data
const responseTimeData = ref([120, 135, 118, 142, 128, 139, 125, 133, 127, 131, 124])
const averageResponseTime = computed(() => {
  const avg = responseTimeData.value.reduce((a, b) => a + b, 0) / responseTimeData.value.length
  return Math.round(avg)
})

// Error rate data
const errorRate = ref(2.3)
const errorBreakdown = ref({
  client: 45,
  server: 12,
  timeout: 8
})

// Service table data
const serviceData = ref([
  {
    name: 'api-gateway',
    namespace: 'production',
    type: 'gateway',
    status: 'healthy',
    cpu: 45,
    memory: 67,
    requestsPerMin: 1250,
    errorRate: 1.2,
    latencyP95: 89,
    uptime: '99.9%'
  },
  {
    name: 'user-service',
    namespace: 'production',
    type: 'service',
    status: 'warning',
    cpu: 78,
    memory: 82,
    requestsPerMin: 890,
    errorRate: 2.1,
    latencyP95: 156,
    uptime: '99.7%'
  },
  {
    name: 'auth-service',
    namespace: 'production',
    type: 'service',
    status: 'healthy',
    cpu: 34,
    memory: 45,
    requestsPerMin: 450,
    errorRate: 0.8,
    latencyP95: 67,
    uptime: '99.9%'
  },
  {
    name: 'payment-service',
    namespace: 'production',
    type: 'service',
    status: 'error',
    cpu: 89,
    memory: 91,
    requestsPerMin: 340,
    errorRate: 8.7,
    latencyP95: 234,
    uptime: '98.1%'
  }
])

// Methods
const refreshData = async () => {
  isRefreshing.value = true
  await new Promise(resolve => setTimeout(resolve, 1000))
  isRefreshing.value = false
}

const toggleMetric = (metricId: string) => {
  const metric = chartMetrics.value.find(m => m.id === metricId)
  if (metric) {
    metric.visible = !metric.visible
  }
}

const getKpiColor = (status: string) => {
  const colors = {
    healthy: '#10b981',
    warning: '#f59e0b',
    error: '#ef4444'
  }
  return colors[status] || '#6b7280'
}

const getSparklinePath = (data: number[]) => {
  if (data.length === 0) return ''
  
  const max = Math.max(...data)
  const min = Math.min(...data)
  const range = max - min || 1
  
  return data
    .map((value, index) => {
      const x = (index / (data.length - 1)) * 190 + 5
      const y = 40 - ((value - min) / range) * 35
      return `${index === 0 ? 'M' : 'L'} ${x} ${y}`
    })
    .join(' ')
}

const getSparklineAreaPath = (data: number[]) => {
  if (data.length === 0) return ''
  
  const linePath = getSparklinePath(data)
  const lastX = (data.length - 1) / (data.length - 1) * 190 + 5
  
  return `${linePath} L ${lastX} 45 L 5 45 Z`
}

const getLastPointY = (data: number[]) => {
  if (data.length === 0) return 24
  
  const max = Math.max(...data)
  const min = Math.min(...data)
  const range = max - min || 1
  const lastValue = data[data.length - 1]
  
  return 40 - ((lastValue - min) / range) * 35
}

const getCpuPath = () => {
  return cpuData.value
    .map((value, index) => {
      const x = 60 + index * 72
      const y = 40 + (100 - value) * 2.2
      return `${index === 0 ? 'M' : 'L'} ${x} ${y}`
    })
    .join(' ')
}

const getCpuAreaPath = () => {
  const linePath = getCpuPath()
  const lastX = 60 + (cpuData.value.length - 1) * 72
  
  return `${linePath} L ${lastX} 260 L 60 260 Z`
}

const getMemoryPath = () => {
  return memoryData.value
    .map((value, index) => {
      const x = 60 + index * 72
      const y = 40 + (100 - value) * 2.2
      return `${index === 0 ? 'M' : 'L'} ${x} ${y}`
    })
    .join(' ')
}

const getMemoryAreaPath = () => {
  const linePath = getMemoryPath()
  const lastX = 60 + (memoryData.value.length - 1) * 72
  
  return `${linePath} L ${lastX} 260 L 60 260 Z`
}

const getNetworkPath = () => {
  return networkData.value
    .map((value, index) => {
      const x = 60 + index * 72
      const y = 40 + (100 - value) * 2.2
      return `${index === 0 ? 'M' : 'L'} ${x} ${y}`
    })
    .join(' ')
}

const getNetworkAreaPath = () => {
  const linePath = getNetworkPath()
  const lastX = 60 + (networkData.value.length - 1) * 72
  
  return `${linePath} L ${lastX} 260 L 60 260 Z`
}

const getResponseTimePath = () => {
  return responseTimeData.value
    .map((time, index) => {
      const x = 10 + index * 18
      const y = 70 - (time / 500) * 60
      return `${index === 0 ? 'M' : 'L'} ${x} ${y}`
    })
    .join(' ')
}

onMounted(() => {
  // Initialize any real-time data connections
})
</script>

<style scoped>
.advanced-metrics {
  padding: 24px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  min-height: 100vh;
  color: #1f2937;
}

/* Page Header */
.page-header {
  margin-bottom: 32px;
}

.header-content {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  padding: 24px;
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(20px);
  border-radius: 16px;
  box-shadow: 
    0 4px 6px -1px rgba(0, 0, 0, 0.1),
    0 2px 4px -1px rgba(0, 0, 0, 0.06);
}

.title-section {
  flex: 1;
}

.page-title {
  font-size: 32px;
  font-weight: 800;
  color: #111827;
  margin: 0 0 8px 0;
  letter-spacing: -0.025em;
}

.page-subtitle {
  font-size: 16px;
  color: #6b7280;
  margin: 0;
  font-weight: 500;
}

.header-actions {
  display: flex;
  gap: 16px;
  align-items: center;
}

.time-select {
  padding: 12px 16px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  border-radius: 10px;
  background: white;
  font-size: 14px;
  font-weight: 500;
  color: #374151;
  min-width: 160px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

.refresh-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 20px;
  background: linear-gradient(135deg, #667eea, #764ba2);
  color: white;
  border: none;
  border-radius: 10px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s;
  box-shadow: 0 4px 12px rgba(102, 126, 234, 0.4);
}

.refresh-btn:hover {
  transform: translateY(-1px);
  box-shadow: 0 6px 16px rgba(102, 126, 234, 0.5);
}

.refresh-btn.loading {
  opacity: 0.8;
  cursor: not-allowed;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.animate-spin {
  animation: spin 1s linear infinite;
}

/* KPI Section */
.kpi-section {
  margin-bottom: 32px;
}

.kpi-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
  gap: 24px;
}

.kpi-card {
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(20px);
  border-radius: 16px;
  padding: 24px;
  box-shadow: 
    0 4px 6px -1px rgba(0, 0, 0, 0.1),
    0 2px 4px -1px rgba(0, 0, 0, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.2);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  position: relative;
  overflow: hidden;
}

.kpi-card::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 4px;
  background: linear-gradient(90deg, #10b981, #059669);
}

.kpi-card.kpi-warning::before {
  background: linear-gradient(90deg, #f59e0b, #d97706);
}

.kpi-card.kpi-error::before {
  background: linear-gradient(90deg, #ef4444, #dc2626);
}

.kpi-card:hover {
  transform: translateY(-4px);
  box-shadow: 
    0 10px 25px -3px rgba(0, 0, 0, 0.1),
    0 4px 6px -2px rgba(0, 0, 0, 0.05);
}

.kpi-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.kpi-icon-container {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 48px;
  height: 48px;
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.1), rgba(118, 75, 162, 0.1));
  border-radius: 12px;
  color: #667eea;
}

.kpi-trend {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 14px;
  font-weight: 600;
  padding: 6px 12px;
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.04);
}

.kpi-trend.trend-up {
  color: #10b981;
  background: rgba(16, 185, 129, 0.1);
}

.kpi-trend.trend-down {
  color: #ef4444;
  background: rgba(239, 68, 68, 0.1);
}

.kpi-trend.trend-stable {
  color: #6b7280;
  background: rgba(107, 114, 128, 0.1);
}

.kpi-content {
  margin-bottom: 20px;
}

.kpi-value {
  font-size: 36px;
  font-weight: 800;
  color: #111827;
  margin-bottom: 8px;
  letter-spacing: -0.025em;
}

.kpi-label {
  font-size: 16px;
  color: #6b7280;
  font-weight: 500;
  margin-bottom: 6px;
}

.kpi-target {
  font-size: 13px;
  color: #9ca3af;
}

.target-value {
  font-weight: 600;
  color: #374151;
}

.kpi-sparkline {
  height: 48px;
  margin-top: 16px;
}

.sparkline-svg {
  width: 100%;
  height: 100%;
}

.sparkline-line {
  filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.1));
}

.sparkline-dot {
  filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.2));
}

/* Charts Section */
.charts-section {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 24px;
  margin-bottom: 32px;
}

.chart-container {
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(20px);
  border-radius: 16px;
  padding: 24px;
  box-shadow: 
    0 4px 6px -1px rgba(0, 0, 0, 0.1),
    0 2px 4px -1px rgba(0, 0, 0, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.2);
}

.primary-chart {
  grid-row: span 2;
}

.chart-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 24px;
  padding-bottom: 16px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.08);
}

.chart-title-section {
  flex: 1;
}

.chart-title {
  font-size: 20px;
  font-weight: 700;
  color: #111827;
  margin: 0 0 6px 0;
}

.chart-subtitle {
  font-size: 14px;
  color: #6b7280;
  margin: 0;
}

.chart-controls {
  display: flex;
  gap: 20px;
  align-items: center;
}

.legend-controls {
  display: flex;
  gap: 12px;
}

.legend-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  border-radius: 8px;
  background: transparent;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
}

.legend-item:hover {
  background: rgba(0, 0, 0, 0.04);
}

.legend-item.active {
  background: var(--color);
  color: white;
  border-color: var(--color);
}

.legend-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--color);
}

.legend-item.active .legend-dot {
  background: rgba(255, 255, 255, 0.8);
}

.chart-actions {
  display: flex;
  gap: 8px;
}

.chart-action-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  border-radius: 6px;
  background: transparent;
  font-size: 13px;
  color: #6b7280;
  cursor: pointer;
  transition: all 0.2s;
}

.chart-action-btn:hover {
  background: rgba(0, 0, 0, 0.04);
  color: #374151;
}

.chart-content {
  flex: 1;
}

.chart-canvas {
  height: 300px;
}

.main-chart {
  width: 100%;
  height: 100%;
}

.axis-label {
  font-size: 12px;
  fill: #9ca3af;
  font-weight: 500;
}

.chart-area {
  opacity: 0.7;
}

.chart-line {
  filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.1));
}

.data-point {
  filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.2));
  transition: r 0.2s;
}

.data-point:hover {
  r: 5;
}

/* Secondary Charts */
.secondary-charts {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.chart-value {
  text-align: right;
}

.value-number {
  font-size: 24px;
  font-weight: 700;
  color: #111827;
}

.value-total {
  font-size: 16px;
  color: #6b7280;
}

.value-unit {
  font-size: 14px;
  color: #6b7280;
  margin-left: 4px;
}

.chart-value.critical .value-number {
  color: #ef4444;
}

/* Donut Chart */
.donut-chart-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
}

.donut-chart {
  width: 120px;
  height: 120px;
}

.donut-progress {
  transition: stroke-dashoffset 0.5s ease;
}

.donut-percentage {
  font-size: 18px;
  font-weight: 700;
  fill: #111827;
  text-anchor: middle;
  dominant-baseline: middle;
}

.donut-legend {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
}

.donut-legend .legend-item {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  border: none;
  padding: 0;
  background: none;
  cursor: default;
}

.donut-legend .legend-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.donut-legend .legend-dot.healthy {
  background: #10b981;
}

.donut-legend .legend-dot.warning {
  background: #f59e0b;
}

.donut-legend .legend-dot.error {
  background: #ef4444;
}

.legend-text {
  color: #6b7280;
  font-weight: 500;
}

/* Response Time Chart */
.response-time-chart {
  height: 80px;
  margin-top: 12px;
}

.response-chart {
  width: 100%;
  height: 100%;
}

.response-line {
  filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.1));
}

.response-point {
  filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.2));
}

/* Error Rate */
.error-rate-display {
  margin-top: 16px;
}

.error-bar {
  width: 100%;
  height: 8px;
  background: rgba(239, 68, 68, 0.1);
  border-radius: 4px;
  overflow: hidden;
  margin-bottom: 16px;
}

.error-fill {
  height: 100%;
  background: linear-gradient(90deg, #f59e0b, #ef4444);
  border-radius: 4px;
  transition: width 0.3s ease;
}

.error-details {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.error-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 13px;
}

.error-type {
  color: #6b7280;
  font-weight: 500;
}

.error-count {
  color: #111827;
  font-weight: 600;
}

/* Service Table */
.service-table-container {
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(20px);
  border-radius: 16px;
  box-shadow: 
    0 4px 6px -1px rgba(0, 0, 0, 0.1),
    0 2px 4px -1px rgba(0, 0, 0, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.2);
  overflow: hidden;
}

.table-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 24px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.08);
}

.table-title {
  font-size: 20px;
  font-weight: 700;
  color: #111827;
  margin: 0;
}

.table-actions {
  display: flex;
  gap: 12px;
}

.table-action-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  border-radius: 8px;
  background: transparent;
  font-size: 14px;
  font-weight: 500;
  color: #6b7280;
  cursor: pointer;
  transition: all 0.2s;
}

.table-action-btn:hover {
  background: rgba(0, 0, 0, 0.04);
  color: #374151;
}

.table-content {
  overflow-x: auto;
}

.service-table {
  width: 100%;
  border-collapse: collapse;
}

.service-table th {
  background: rgba(0, 0, 0, 0.02);
  padding: 16px;
  text-align: left;
  font-size: 13px;
  font-weight: 600;
  color: #6b7280;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  border-bottom: 1px solid rgba(0, 0, 0, 0.08);
}

.service-table td {
  padding: 16px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.05);
  font-size: 14px;
}

.service-row:hover {
  background: rgba(0, 0, 0, 0.02);
}

.service-info {
  display: flex;
  align-items: center;
  gap: 12px;
}

.service-icon {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.service-icon.icon-gateway {
  background: #3b82f6;
}

.service-icon.icon-service {
  background: #8b5cf6;
}

.service-icon.icon-database {
  background: #06b6d4;
}

.service-details {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.service-details .name {
  font-weight: 600;
  color: #111827;
}

.service-details .namespace {
  font-size: 12px;
  color: #6b7280;
}

.status-badge {
  padding: 4px 8px;
  border-radius: 12px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.status-badge.status-healthy {
  background: rgba(16, 185, 129, 0.1);
  color: #065f46;
}

.status-badge.status-warning {
  background: rgba(245, 158, 11, 0.1);
  color: #92400e;
}

.status-badge.status-error {
  background: rgba(239, 68, 68, 0.1);
  color: #991b1b;
}

.metric-display {
  display: flex;
  align-items: center;
  gap: 8px;
}

.metric-bar {
  width: 60px;
  height: 6px;
  background: rgba(0, 0, 0, 0.1);
  border-radius: 3px;
  overflow: hidden;
}

.metric-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.3s ease;
}

.metric-fill.cpu-fill {
  background: linear-gradient(90deg, #3b82f6, #1d4ed8);
}

.metric-fill.memory-fill {
  background: linear-gradient(90deg, #10b981, #047857);
}

.metric-value {
  font-size: 13px;
  font-weight: 600;
  color: #374151;
  min-width: 40px;
}

.metric-number {
  font-weight: 600;
  color: #111827;
  font-variant-numeric: tabular-nums;
}

.metric-errors.critical .metric-number {
  color: #ef4444;
}

/* Responsive Design */
@media (max-width: 1200px) {
  .charts-section {
    grid-template-columns: 1fr;
  }
  
  .primary-chart {
    grid-row: span 1;
  }
  
  .secondary-charts {
    flex-direction: row;
    flex-wrap: wrap;
  }
  
  .secondary-charts .chart-container {
    flex: 1;
    min-width: 250px;
  }
}

@media (max-width: 768px) {
  .advanced-metrics {
    padding: 16px;
  }
  
  .header-content {
    flex-direction: column;
    gap: 16px;
    align-items: stretch;
  }
  
  .kpi-grid {
    grid-template-columns: 1fr;
  }
  
  .secondary-charts {
    flex-direction: column;
  }
  
  .table-content {
    font-size: 12px;
  }
  
  .service-table th,
  .service-table td {
    padding: 12px 8px;
  }
}
</style>