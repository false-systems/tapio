<template>
  <div class="ultra-modern-dashboard">
    <!-- Sleek Header -->
    <header class="sleek-header">
      <div class="header-inner">
        <!-- Logo -->
        <div class="logo-section">
          <div class="logo">
            <div class="logo-icon">T</div>
            <div class="logo-text">Tapio</div>
          </div>
        </div>

        <!-- Nav -->
        <nav class="main-nav">
          <button 
            v-for="item in navItems" 
            :key="item.id"
            @click="activeView = item.id"
            :class="['nav-item', { active: activeView === item.id }]"
          >
            {{ item.name }}
          </button>
        </nav>

        <!-- Actions -->
        <div class="header-actions">
          <div class="connection-status" :class="{ connected: isConnected }">
            <div class="status-dot"></div>
          </div>
          <button class="notification-btn">
            <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
              <path d="M10 2a6 6 0 0 0-6 6v3.586l-.707.707A1 1 0 0 0 4 14h12a1 1 0 0 0 .707-1.707L16 11.586V8a6 6 0 0 0-6-6zM10 18a3 3 0 0 1-3-3h6a3 3 0 0 1-3 3z" fill="currentColor"/>
            </svg>
            <span class="notification-badge">3</span>
          </button>
          <div class="user-avatar">A</div>
        </div>
      </div>
    </header>

    <!-- Main Content -->
    <main class="main-container">
      <!-- Dashboard View -->
      <div v-if="activeView === 'dashboard'" class="dashboard-view">
        <!-- Hero Stats -->
        <div class="hero-section">
          <div class="hero-stat">
            <div class="hero-value">
              <span class="hero-number">98.7</span>
              <span class="hero-unit">%</span>
            </div>
            <div class="hero-label">System Health</div>
            <div class="hero-trend up">↑ 2.3%</div>
          </div>

          <div class="hero-stat">
            <div class="hero-value">
              <span class="hero-number">2.4</span>
              <span class="hero-unit">ms</span>
            </div>
            <div class="hero-label">Avg Response Time</div>
            <div class="hero-trend down">↓ 0.3ms</div>
          </div>

          <div class="hero-stat">
            <div class="hero-value">
              <span class="hero-number">142</span>
              <span class="hero-unit">k/sec</span>
            </div>
            <div class="hero-label">Throughput</div>
            <div class="hero-trend up">↑ 12%</div>
          </div>

          <div class="hero-stat alert">
            <div class="hero-value">
              <span class="hero-number">3</span>
            </div>
            <div class="hero-label">Active Alerts</div>
            <div class="hero-trend">2 critical</div>
          </div>
        </div>

        <!-- Chart Section -->
        <div class="charts-section">
          <div class="chart-card large">
            <div class="chart-header">
              <h3>Performance Overview</h3>
              <div class="time-selector">
                <button class="time-btn active">1H</button>
                <button class="time-btn">24H</button>
                <button class="time-btn">7D</button>
              </div>
            </div>
            <div class="chart-body">
              <svg class="performance-chart" viewBox="0 0 800 300">
                <!-- Grid -->
                <g class="chart-grid">
                  <line v-for="i in 5" :key="`h-${i}`" 
                        :x1="0" :y1="i * 60" 
                        :x2="800" :y2="i * 60" 
                        stroke="#f0f0f0" stroke-width="1"/>
                </g>
                
                <!-- Area Chart -->
                <defs>
                  <linearGradient id="perf-gradient" x1="0%" y1="0%" x2="0%" y2="100%">
                    <stop offset="0%" style="stop-color:#4F46E5;stop-opacity:0.3" />
                    <stop offset="100%" style="stop-color:#4F46E5;stop-opacity:0" />
                  </linearGradient>
                </defs>
                
                <path d="M0 200 L100 180 L200 160 L300 170 L400 140 L500 130 L600 120 L700 140 L800 130 L800 300 L0 300 Z" 
                      fill="url(#perf-gradient)"/>
                <path d="M0 200 L100 180 L200 160 L300 170 L400 140 L500 130 L600 120 L700 140 L800 130" 
                      stroke="#4F46E5" stroke-width="3" fill="none"/>
                
                <!-- Data Points -->
                <g class="data-points">
                  <circle v-for="(point, i) in [200, 180, 160, 170, 140, 130, 120, 140, 130]" 
                          :key="`point-${i}`"
                          :cx="i * 100" 
                          :cy="point" 
                          r="4" 
                          fill="#4F46E5"
                          class="data-point"/>
                </g>
              </svg>
            </div>
          </div>

          <div class="chart-card small">
            <div class="chart-header">
              <h3>Service Health</h3>
            </div>
            <div class="service-health-grid">
              <div class="service-health-item healthy">
                <div class="service-icon">API</div>
                <div class="service-info">
                  <div class="service-name">API Gateway</div>
                  <div class="service-status">Healthy</div>
                </div>
                <div class="service-metric">99.9%</div>
              </div>
              <div class="service-health-item healthy">
                <div class="service-icon">DB</div>
                <div class="service-info">
                  <div class="service-name">Database</div>
                  <div class="service-status">Healthy</div>
                </div>
                <div class="service-metric">98.2%</div>
              </div>
              <div class="service-health-item warning">
                <div class="service-icon">AU</div>
                <div class="service-info">
                  <div class="service-name">Auth Service</div>
                  <div class="service-status">High Load</div>
                </div>
                <div class="service-metric">89.1%</div>
              </div>
              <div class="service-health-item error">
                <div class="service-icon">CA</div>
                <div class="service-info">
                  <div class="service-name">Cache</div>
                  <div class="service-status">Error</div>
                </div>
                <div class="service-metric">72.3%</div>
              </div>
            </div>
          </div>
        </div>

        <!-- Activity Feed -->
        <div class="activity-section">
          <div class="section-header">
            <h3>Recent Activity</h3>
            <button class="see-all-btn">See all →</button>
          </div>
          <div class="activity-list">
            <div class="activity-item">
              <div class="activity-icon deployment">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                  <path d="M8 1v7m-3-3l3 3 3-3m-7 7h8" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                </svg>
              </div>
              <div class="activity-details">
                <div class="activity-title">New deployment completed</div>
                <div class="activity-description">Frontend v2.3.1 deployed to production</div>
                <div class="activity-time">2 minutes ago</div>
              </div>
              <div class="activity-status success">Success</div>
            </div>

            <div class="activity-item">
              <div class="activity-icon alert">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                  <path d="M8 1l7 13H1L8 1z" stroke="currentColor" stroke-width="2" fill="none"/>
                  <path d="M8 6v3m0 2h.01" stroke="currentColor" stroke-width="2"/>
                </svg>
              </div>
              <div class="activity-details">
                <div class="activity-title">High memory usage detected</div>
                <div class="activity-description">Cache service using 89% of allocated memory</div>
                <div class="activity-time">5 minutes ago</div>
              </div>
              <div class="activity-status warning">Warning</div>
            </div>

            <div class="activity-item">
              <div class="activity-icon scaling">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                  <path d="M3 13V7m0 0l3 3m-3-3l3-3m4 0v6m0 0l3-3m-3 3l3 3" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
                </svg>
              </div>
              <div class="activity-details">
                <div class="activity-title">Auto-scaling triggered</div>
                <div class="activity-description">API Gateway scaled from 3 to 5 instances</div>
                <div class="activity-time">12 minutes ago</div>
              </div>
              <div class="activity-status info">Info</div>
            </div>
          </div>
        </div>
      </div>

      <!-- Services View -->
      <div v-if="activeView === 'services'" class="services-view">
        <div class="view-header">
          <h1>Service Topology</h1>
          <div class="view-controls">
            <button class="control-btn">
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                <path d="M3 6h10M3 10h10" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
              </svg>
              Filter
            </button>
            <button class="control-btn primary">
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                <path d="M8 4v8m4-4H4" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
              </svg>
              Add Service
            </button>
          </div>
        </div>

        <div class="topology-container">
          <svg class="topology-svg" viewBox="0 0 1000 600">
            <!-- Connections -->
            <g class="connections">
              <path d="M 200 150 Q 350 150 350 300" stroke="#e0e0e0" stroke-width="2" fill="none"/>
              <path d="M 200 150 Q 350 150 500 150" stroke="#e0e0e0" stroke-width="2" fill="none"/>
              <path d="M 500 150 Q 500 225 350 300" stroke="#e0e0e0" stroke-width="2" fill="none"/>
              <path d="M 350 300 Q 350 400 350 450" stroke="#e0e0e0" stroke-width="2" fill="none"/>
              <path d="M 500 150 Q 650 150 650 300" stroke="#e0e0e0" stroke-width="2" fill="none"/>
            </g>

            <!-- Service Nodes -->
            <g class="nodes">
              <!-- Frontend -->
              <g transform="translate(200, 150)">
                <circle r="40" fill="#4F46E5" opacity="0.1"/>
                <circle r="40" fill="none" stroke="#4F46E5" stroke-width="2"/>
                <text y="5" text-anchor="middle" fill="#4F46E5" font-weight="bold">FE</text>
                <text y="60" text-anchor="middle" fill="#666" font-size="12">Frontend</text>
              </g>

              <!-- API Gateway -->
              <g transform="translate(500, 150)">
                <circle r="40" fill="#10B981" opacity="0.1"/>
                <circle r="40" fill="none" stroke="#10B981" stroke-width="2"/>
                <text y="5" text-anchor="middle" fill="#10B981" font-weight="bold">API</text>
                <text y="60" text-anchor="middle" fill="#666" font-size="12">API Gateway</text>
              </g>

              <!-- Auth Service -->
              <g transform="translate(350, 300)">
                <circle r="40" fill="#F59E0B" opacity="0.1"/>
                <circle r="40" fill="none" stroke="#F59E0B" stroke-width="2"/>
                <text y="5" text-anchor="middle" fill="#F59E0B" font-weight="bold">AU</text>
                <text y="60" text-anchor="middle" fill="#666" font-size="12">Auth Service</text>
              </g>

              <!-- Database -->
              <g transform="translate(350, 450)">
                <rect x="-40" y="-40" width="80" height="80" rx="8" fill="#6366F1" opacity="0.1"/>
                <rect x="-40" y="-40" width="80" height="80" rx="8" fill="none" stroke="#6366F1" stroke-width="2"/>
                <text y="5" text-anchor="middle" fill="#6366F1" font-weight="bold">DB</text>
                <text y="60" text-anchor="middle" fill="#666" font-size="12">PostgreSQL</text>
              </g>

              <!-- Cache -->
              <g transform="translate(650, 300)">
                <circle r="40" fill="#EF4444" opacity="0.1"/>
                <circle r="40" fill="none" stroke="#EF4444" stroke-width="2"/>
                <text y="5" text-anchor="middle" fill="#EF4444" font-weight="bold">CA</text>
                <text y="60" text-anchor="middle" fill="#666" font-size="12">Redis Cache</text>
              </g>
            </g>

            <!-- Traffic Flow Animation -->
            <circle r="4" fill="#4F46E5">
              <animateMotion dur="3s" repeatCount="indefinite">
                <mpath href="#flow-path-1"/>
              </animateMotion>
            </circle>
            <path id="flow-path-1" d="M 200 150 Q 350 150 350 300" fill="none"/>
          </svg>
        </div>
      </div>

      <!-- Metrics View -->
      <div v-if="activeView === 'metrics'" class="metrics-view">
        <div class="metrics-grid">
          <div class="metric-card">
            <div class="metric-header">
              <h3>CPU Usage</h3>
              <span class="metric-value">67%</span>
            </div>
            <div class="metric-chart">
              <div class="bar-chart">
                <div v-for="i in 12" :key="`cpu-${i}`" 
                     class="bar" 
                     :style="{ height: `${Math.random() * 60 + 40}%` }"></div>
              </div>
            </div>
          </div>

          <div class="metric-card">
            <div class="metric-header">
              <h3>Memory Usage</h3>
              <span class="metric-value">4.2 GB</span>
            </div>
            <div class="metric-chart">
              <div class="donut-chart">
                <svg viewBox="0 0 100 100">
                  <circle cx="50" cy="50" r="40" fill="none" stroke="#f0f0f0" stroke-width="8"/>
                  <circle cx="50" cy="50" r="40" fill="none" stroke="#4F46E5" stroke-width="8"
                          stroke-dasharray="201 251" transform="rotate(-90 50 50)"/>
                  <text x="50" y="55" text-anchor="middle" font-size="20" font-weight="bold" fill="#333">80%</text>
                </svg>
              </div>
            </div>
          </div>

          <div class="metric-card">
            <div class="metric-header">
              <h3>Request Rate</h3>
              <span class="metric-value">1.2k/s</span>
            </div>
            <div class="metric-chart">
              <div class="line-chart">
                <svg viewBox="0 0 200 80">
                  <polyline points="0,60 20,50 40,55 60,45 80,40 100,35 120,30 140,35 160,25 180,30 200,20"
                            fill="none" stroke="#10B981" stroke-width="2"/>
                  <polyline points="0,60 20,50 40,55 60,45 80,40 100,35 120,30 140,35 160,25 180,30 200,20 200,80 0,80"
                            fill="#10B981" opacity="0.1"/>
                </svg>
              </div>
            </div>
          </div>

          <div class="metric-card">
            <div class="metric-header">
              <h3>Error Rate</h3>
              <span class="metric-value error">2.1%</span>
            </div>
            <div class="metric-chart">
              <div class="bar-chart error-chart">
                <div v-for="i in 12" :key="`error-${i}`" 
                     class="bar error" 
                     :style="{ height: `${Math.random() * 20 + 5}%` }"></div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Alerts View -->
      <div v-if="activeView === 'alerts'" class="alerts-view">
        <div class="alerts-header">
          <h1>Active Alerts</h1>
          <div class="alert-summary">
            <span class="summary-item critical">2 Critical</span>
            <span class="summary-item warning">5 Warning</span>
            <span class="summary-item info">12 Info</span>
          </div>
        </div>

        <div class="alerts-list">
          <div class="alert-card critical">
            <div class="alert-icon">
              <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
                <path d="M12 8v4m0 4h.01M12 2L2 20h20L12 2z" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
              </svg>
            </div>
            <div class="alert-content">
              <h3>Database Connection Failed</h3>
              <p>Primary database is unreachable. Multiple services affected.</p>
              <div class="alert-meta">
                <span>Started 5 minutes ago</span>
                <span>•</span>
                <span>3 services affected</span>
              </div>
            </div>
            <button class="alert-action">Investigate →</button>
          </div>

          <div class="alert-card critical">
            <div class="alert-icon">
              <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
                <path d="M12 8v4m0 4h.01M12 2L2 20h20L12 2z" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
              </svg>
            </div>
            <div class="alert-content">
              <h3>High Error Rate</h3>
              <p>Payment service error rate exceeded 5% threshold.</p>
              <div class="alert-meta">
                <span>Started 2 minutes ago</span>
                <span>•</span>
                <span>Payment flow impacted</span>
              </div>
            </div>
            <button class="alert-action">Investigate →</button>
          </div>

          <div class="alert-card warning">
            <div class="alert-icon">
              <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
                <path d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
              </svg>
            </div>
            <div class="alert-content">
              <h3>High Memory Usage</h3>
              <p>Cache service memory usage at 89% of capacity.</p>
              <div class="alert-meta">
                <span>Started 15 minutes ago</span>
                <span>•</span>
                <span>Auto-scaling available</span>
              </div>
            </div>
            <button class="alert-action">View Details →</button>
          </div>
        </div>
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'

const activeView = ref('dashboard')
const isConnected = ref(false)

const navItems = [
  { id: 'dashboard', name: 'Dashboard' },
  { id: 'services', name: 'Services' },
  { id: 'metrics', name: 'Metrics' },
  { id: 'alerts', name: 'Alerts' }
]

let websocket: WebSocket | null = null

const connectWebSocket = () => {
  try {
    websocket = new WebSocket('ws://localhost:3001/ws')
    
    websocket.onopen = () => {
      isConnected.value = true
    }
    
    websocket.onclose = () => {
      isConnected.value = false
      setTimeout(connectWebSocket, 3000)
    }
    
    websocket.onerror = () => {
      isConnected.value = false
    }
  } catch (error) {
    console.error('WebSocket connection failed:', error)
    isConnected.value = false
  }
}

onMounted(() => {
  connectWebSocket()
})

onUnmounted(() => {
  if (websocket) {
    websocket.close()
  }
})
</script>

<style scoped>
* {
  box-sizing: border-box;
}

.ultra-modern-dashboard {
  min-height: 100vh;
  background: #fafafa;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  color: #1a1a1a;
}

/* Header */
.sleek-header {
  background: white;
  border-bottom: 1px solid #e5e5e5;
  position: sticky;
  top: 0;
  z-index: 100;
}

.header-inner {
  max-width: 1400px;
  margin: 0 auto;
  padding: 0 24px;
  height: 60px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.logo-section {
  flex-shrink: 0;
}

.logo {
  display: flex;
  align-items: center;
  gap: 8px;
}

.logo-icon {
  width: 32px;
  height: 32px;
  background: #4F46E5;
  color: white;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: bold;
  font-size: 18px;
}

.logo-text {
  font-size: 20px;
  font-weight: 700;
  color: #1a1a1a;
}

/* Navigation */
.main-nav {
  display: flex;
  gap: 4px;
}

.nav-item {
  padding: 8px 16px;
  border: none;
  background: transparent;
  color: #666;
  font-size: 14px;
  font-weight: 500;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.2s;
}

.nav-item:hover {
  background: #f5f5f5;
  color: #1a1a1a;
}

.nav-item.active {
  background: #4F46E5;
  color: white;
}

/* Header Actions */
.header-actions {
  display: flex;
  align-items: center;
  gap: 16px;
}

.connection-status {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: #f5f5f5;
  display: flex;
  align-items: center;
  justify-content: center;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #ccc;
  transition: all 0.3s;
}

.connection-status.connected .status-dot {
  background: #10B981;
  box-shadow: 0 0 0 3px rgba(16, 185, 129, 0.2);
}

.notification-btn {
  position: relative;
  width: 32px;
  height: 32px;
  border: none;
  background: transparent;
  color: #666;
  cursor: pointer;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s;
}

.notification-btn:hover {
  background: #f5f5f5;
}

.notification-badge {
  position: absolute;
  top: -2px;
  right: -2px;
  background: #EF4444;
  color: white;
  font-size: 10px;
  font-weight: bold;
  padding: 2px 5px;
  border-radius: 10px;
  min-width: 16px;
  text-align: center;
}

.user-avatar {
  width: 32px;
  height: 32px;
  background: #4F46E5;
  color: white;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
  cursor: pointer;
}

/* Main Container */
.main-container {
  max-width: 1400px;
  margin: 0 auto;
  padding: 24px;
}

/* Dashboard View */
.dashboard-view {
  display: grid;
  gap: 24px;
}

/* Hero Section */
.hero-section {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 20px;
}

.hero-stat {
  background: white;
  border-radius: 12px;
  padding: 24px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
  transition: all 0.3s;
}

.hero-stat:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}

.hero-value {
  display: flex;
  align-items: baseline;
  gap: 4px;
  margin-bottom: 8px;
}

.hero-number {
  font-size: 36px;
  font-weight: 700;
  color: #1a1a1a;
}

.hero-unit {
  font-size: 20px;
  font-weight: 400;
  color: #666;
}

.hero-label {
  font-size: 14px;
  color: #666;
  margin-bottom: 8px;
}

.hero-trend {
  font-size: 13px;
  font-weight: 600;
}

.hero-trend.up {
  color: #10B981;
}

.hero-trend.down {
  color: #4F46E5;
}

.hero-stat.alert .hero-number {
  color: #EF4444;
}

.hero-stat.alert .hero-trend {
  color: #DC2626;
}

/* Charts Section */
.charts-section {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 20px;
}

.chart-card {
  background: white;
  border-radius: 12px;
  padding: 24px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

.chart-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.chart-header h3 {
  font-size: 18px;
  font-weight: 600;
  margin: 0;
}

.time-selector {
  display: flex;
  gap: 4px;
}

.time-btn {
  padding: 4px 12px;
  border: 1px solid #e5e5e5;
  background: white;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  color: #666;
  cursor: pointer;
  transition: all 0.2s;
}

.time-btn:hover {
  border-color: #4F46E5;
  color: #4F46E5;
}

.time-btn.active {
  background: #4F46E5;
  color: white;
  border-color: #4F46E5;
}

.performance-chart {
  width: 100%;
  height: 300px;
}

.data-point {
  transition: all 0.3s;
}

.data-point:hover {
  r: 6;
  filter: drop-shadow(0 2px 4px rgba(79, 70, 229, 0.3));
}

/* Service Health */
.service-health-grid {
  display: grid;
  gap: 12px;
}

.service-health-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  border-radius: 8px;
  background: #f8f8f8;
  transition: all 0.2s;
}

.service-health-item:hover {
  background: #f0f0f0;
}

.service-icon {
  width: 40px;
  height: 40px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: bold;
  font-size: 14px;
  color: white;
}

.service-health-item.healthy .service-icon {
  background: #10B981;
}

.service-health-item.warning .service-icon {
  background: #F59E0B;
}

.service-health-item.error .service-icon {
  background: #EF4444;
}

.service-info {
  flex: 1;
}

.service-name {
  font-size: 14px;
  font-weight: 600;
  color: #1a1a1a;
}

.service-status {
  font-size: 12px;
  color: #666;
}

.service-metric {
  font-size: 14px;
  font-weight: 700;
}

.service-health-item.healthy .service-metric {
  color: #10B981;
}

.service-health-item.warning .service-metric {
  color: #F59E0B;
}

.service-health-item.error .service-metric {
  color: #EF4444;
}

/* Activity Section */
.activity-section {
  background: white;
  border-radius: 12px;
  padding: 24px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.section-header h3 {
  font-size: 18px;
  font-weight: 600;
  margin: 0;
}

.see-all-btn {
  border: none;
  background: none;
  color: #4F46E5;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
}

.activity-list {
  display: grid;
  gap: 16px;
}

.activity-item {
  display: flex;
  gap: 16px;
  padding: 16px;
  border-radius: 8px;
  background: #f8f8f8;
  transition: all 0.2s;
}

.activity-item:hover {
  background: #f0f0f0;
}

.activity-icon {
  width: 40px;
  height: 40px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.activity-icon.deployment {
  background: #EDE9FE;
  color: #7C3AED;
}

.activity-icon.alert {
  background: #FEF3C7;
  color: #D97706;
}

.activity-icon.scaling {
  background: #DBEAFE;
  color: #1D4ED8;
}

.activity-details {
  flex: 1;
}

.activity-title {
  font-size: 14px;
  font-weight: 600;
  color: #1a1a1a;
  margin-bottom: 4px;
}

.activity-description {
  font-size: 13px;
  color: #666;
  margin-bottom: 4px;
}

.activity-time {
  font-size: 12px;
  color: #999;
}

.activity-status {
  padding: 4px 12px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.activity-status.success {
  background: #D1FAE5;
  color: #065F46;
}

.activity-status.warning {
  background: #FEF3C7;
  color: #92400E;
}

.activity-status.info {
  background: #DBEAFE;
  color: #1E40AF;
}

/* Services View */
.services-view {
  background: white;
  border-radius: 12px;
  padding: 24px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

.view-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.view-header h1 {
  font-size: 24px;
  font-weight: 700;
  margin: 0;
}

.view-controls {
  display: flex;
  gap: 12px;
}

.control-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  border: 1px solid #e5e5e5;
  background: white;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 500;
  color: #666;
  cursor: pointer;
  transition: all 0.2s;
}

.control-btn:hover {
  border-color: #4F46E5;
  color: #4F46E5;
}

.control-btn.primary {
  background: #4F46E5;
  color: white;
  border-color: #4F46E5;
}

.control-btn.primary:hover {
  background: #4338CA;
}

.topology-container {
  height: 600px;
  background: #f8f8f8;
  border-radius: 8px;
  padding: 20px;
}

.topology-svg {
  width: 100%;
  height: 100%;
}

/* Metrics View */
.metrics-view {
  display: grid;
  gap: 24px;
}

.metrics-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
  gap: 20px;
}

.metric-card {
  background: white;
  border-radius: 12px;
  padding: 24px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

.metric-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.metric-header h3 {
  font-size: 16px;
  font-weight: 600;
  margin: 0;
  color: #666;
}

.metric-value {
  font-size: 24px;
  font-weight: 700;
  color: #1a1a1a;
}

.metric-value.error {
  color: #EF4444;
}

.metric-chart {
  height: 120px;
}

.bar-chart {
  display: flex;
  align-items: flex-end;
  gap: 4px;
  height: 100%;
}

.bar {
  flex: 1;
  background: #4F46E5;
  border-radius: 4px 4px 0 0;
  transition: all 0.3s;
}

.bar:hover {
  opacity: 0.8;
}

.error-chart .bar {
  background: #EF4444;
}

.donut-chart {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
}

.donut-chart svg {
  width: 100px;
  height: 100px;
}

.line-chart {
  height: 100%;
}

.line-chart svg {
  width: 100%;
  height: 100%;
}

/* Alerts View */
.alerts-view {
  display: grid;
  gap: 24px;
}

.alerts-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.alerts-header h1 {
  font-size: 24px;
  font-weight: 700;
  margin: 0;
}

.alert-summary {
  display: flex;
  gap: 16px;
}

.summary-item {
  padding: 6px 12px;
  border-radius: 6px;
  font-size: 14px;
  font-weight: 600;
}

.summary-item.critical {
  background: #FEE2E2;
  color: #DC2626;
}

.summary-item.warning {
  background: #FEF3C7;
  color: #D97706;
}

.summary-item.info {
  background: #DBEAFE;
  color: #1D4ED8;
}

.alerts-list {
  display: grid;
  gap: 16px;
}

.alert-card {
  background: white;
  border-radius: 12px;
  padding: 24px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
  display: flex;
  gap: 20px;
  align-items: center;
  border-left: 4px solid transparent;
  transition: all 0.3s;
}

.alert-card:hover {
  transform: translateX(4px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}

.alert-card.critical {
  border-left-color: #EF4444;
}

.alert-card.warning {
  border-left-color: #F59E0B;
}

.alert-icon {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.alert-card.critical .alert-icon {
  background: #FEE2E2;
  color: #DC2626;
}

.alert-card.warning .alert-icon {
  background: #FEF3C7;
  color: #D97706;
}

.alert-content {
  flex: 1;
}

.alert-content h3 {
  font-size: 16px;
  font-weight: 600;
  margin: 0 0 4px 0;
}

.alert-content p {
  font-size: 14px;
  color: #666;
  margin: 0 0 8px 0;
}

.alert-meta {
  font-size: 12px;
  color: #999;
  display: flex;
  gap: 8px;
}

.alert-action {
  padding: 8px 16px;
  border: 1px solid #e5e5e5;
  background: white;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 500;
  color: #4F46E5;
  cursor: pointer;
  transition: all 0.2s;
}

.alert-action:hover {
  background: #4F46E5;
  color: white;
  border-color: #4F46E5;
}

/* Responsive */
@media (max-width: 1024px) {
  .charts-section {
    grid-template-columns: 1fr;
  }
  
  .metrics-grid {
    grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
  }
}

@media (max-width: 768px) {
  .hero-section {
    grid-template-columns: 1fr;
  }
  
  .header-inner {
    padding: 0 16px;
  }
  
  .main-container {
    padding: 16px;
  }
  
  .main-nav {
    display: none;
  }
  
  .view-header {
    flex-direction: column;
    gap: 16px;
    align-items: flex-start;
  }
  
  .alerts-header {
    flex-direction: column;
    gap: 16px;
    align-items: flex-start;
  }
}
</style>