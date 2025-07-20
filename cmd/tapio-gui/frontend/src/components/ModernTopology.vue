<template>
  <div class="modern-topology">
    <!-- Command Bar -->
    <div class="command-bar">
      <div class="command-content">
        <div class="view-controls">
          <div class="control-group">
            <label class="control-label">View</label>
            <select v-model="selectedView" class="control-select">
              <option value="logical">Logical View</option>
              <option value="physical">Physical View</option>
              <option value="security">Security View</option>
            </select>
          </div>
          
          <div class="control-group">
            <label class="control-label">Namespace</label>
            <select v-model="selectedNamespace" class="control-select">
              <option value="">All Namespaces</option>
              <option v-for="ns in namespaces" :key="ns" :value="ns">{{ ns }}</option>
            </select>
          </div>
          
          <div class="control-group">
            <label class="control-label">Time Window</label>
            <select v-model="timeWindow" class="control-select">
              <option value="1m">Last 1 minute</option>
              <option value="5m">Last 5 minutes</option>
              <option value="15m">Last 15 minutes</option>
              <option value="1h">Last hour</option>
            </select>
          </div>
        </div>
        
        <div class="action-controls">
          <button @click="toggleRealTime" :class="['real-time-btn', { active: realTimeEnabled }]">
            <div class="real-time-indicator"></div>
            <span>Real-time</span>
          </button>
          
          <button @click="centerView" class="action-btn">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
              <circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="1.5" fill="none"/>
              <circle cx="8" cy="8" r="2" fill="currentColor"/>
            </svg>
            Center
          </button>
          
          <button @click="fitToScreen" class="action-btn">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
              <path d="M2 3a1 1 0 011-1h3v2H4v2H2V3zM2 13v-3h2v2h2v2H3a1 1 0 01-1-1zM14 13a1 1 0 01-1 1h-3v-2h2v-2h2v3zM14 3v3h-2V4h-2V2h3a1 1 0 011 1z" fill="currentColor"/>
            </svg>
            Fit
          </button>
          
          <div class="zoom-controls">
            <button @click="zoomOut" class="zoom-btn">
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                <path d="M4 8h8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
              </svg>
            </button>
            <span class="zoom-level">{{ Math.round(zoomLevel * 100) }}%</span>
            <button @click="zoomIn" class="zoom-btn">
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                <path d="M8 4v8M4 8h8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
              </svg>
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Topology Canvas -->
    <div class="topology-canvas" ref="canvasContainer">
      <div class="canvas-background">
        <svg class="topology-svg" :viewBox="viewBox" @wheel="handleWheel" @mousedown="startPan" @mousemove="handlePan" @mouseup="endPan">
          <!-- Definitions -->
          <defs>
            <!-- Grid Pattern -->
            <pattern id="topology-grid" width="40" height="40" patternUnits="userSpaceOnUse">
              <path d="M 40 0 L 0 0 0 40" fill="none" stroke="rgba(99, 102, 241, 0.1)" stroke-width="1"/>
            </pattern>
            
            <!-- Flow Gradients -->
            <linearGradient id="flow-healthy" x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" style="stop-color:#10b981;stop-opacity:0.8" />
              <stop offset="100%" style="stop-color:#10b981;stop-opacity:0.3" />
            </linearGradient>
            
            <linearGradient id="flow-warning" x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" style="stop-color:#f59e0b;stop-opacity:0.8" />
              <stop offset="100%" style="stop-color:#f59e0b;stop-opacity:0.3" />
            </linearGradient>
            
            <linearGradient id="flow-error" x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" style="stop-color:#ef4444;stop-opacity:0.8" />
              <stop offset="100%" style="stop-color:#ef4444;stop-opacity:0.3" />
            </linearGradient>
            
            <!-- Service Node Gradients -->
            <radialGradient id="service-gradient" cx="50%" cy="30%">
              <stop offset="0%" style="stop-color:#ffffff;stop-opacity:1" />
              <stop offset="100%" style="stop-color:#f8fafc;stop-opacity:1" />
            </radialGradient>
            
            <!-- Arrow Markers -->
            <marker id="flow-arrow-healthy" markerWidth="12" markerHeight="12" refX="10" refY="6" orient="auto" markerUnits="strokeWidth">
              <path d="M2,2 L2,10 L10,6 z" fill="#10b981"/>
            </marker>
            
            <marker id="flow-arrow-warning" markerWidth="12" markerHeight="12" refX="10" refY="6" orient="auto" markerUnits="strokeWidth">
              <path d="M2,2 L2,10 L10,6 z" fill="#f59e0b"/>
            </marker>
            
            <marker id="flow-arrow-error" markerWidth="12" markerHeight="12" refX="10" refY="6" orient="auto" markerUnits="strokeWidth">
              <path d="M2,2 L2,10 L10,6 z" fill="#ef4444"/>
            </marker>
          </defs>
          
          <!-- Background Grid -->
          <rect x="-2000" y="-2000" width="4000" height="4000" fill="url(#topology-grid)" opacity="0.6"/>
          
          <!-- Service Connections -->
          <g class="connections-layer">
            <g v-for="connection in visibleConnections" :key="connection.id" class="connection-group">
              <!-- Connection Path -->
              <path 
                :d="getConnectionPath(connection)"
                :stroke="`url(#flow-${connection.status})`"
                :stroke-width="getConnectionWidth(connection)"
                fill="none"
                :marker-end="`url(#flow-arrow-${connection.status})`"
                class="connection-path"
                :class="`connection-${connection.status}`"
                @mouseenter="showConnectionTooltip(connection, $event)"
                @mouseleave="hideTooltip"
              />
              
              <!-- Connection Metrics -->
              <g v-if="connection.showMetrics" class="connection-metrics">
                <rect 
                  :x="getConnectionMidpoint(connection).x - 40"
                  :y="getConnectionMidpoint(connection).y - 15"
                  width="80"
                  height="30"
                  rx="6"
                  fill="rgba(255, 255, 255, 0.95)"
                  stroke="rgba(0, 0, 0, 0.1)"
                  stroke-width="1"
                />
                <text 
                  :x="getConnectionMidpoint(connection).x"
                  :y="getConnectionMidpoint(connection).y - 5"
                  text-anchor="middle"
                  class="connection-metric-primary"
                >
                  {{ formatThroughput(connection.throughput) }}
                </text>
                <text 
                  :x="getConnectionMidpoint(connection).x"
                  :y="getConnectionMidpoint(connection).y + 8"
                  text-anchor="middle"
                  class="connection-metric-secondary"
                >
                  {{ connection.latency }}ms
                </text>
              </g>
            </g>
          </g>
          
          <!-- Service Nodes -->
          <g class="nodes-layer">
            <g v-for="service in positionedServices" :key="service.id" 
               :transform="`translate(${service.x}, ${service.y})`"
               class="service-node"
               :class="{ 
                 selected: selectedService === service.id,
                 highlighted: highlightedServices.includes(service.id)
               }"
               @click="selectService(service)"
               @mouseenter="showServiceTooltip(service, $event)"
               @mouseleave="hideTooltip">
              
              <!-- Node Shadow -->
              <circle 
                :r="getNodeRadius(service) + 2"
                fill="rgba(0, 0, 0, 0.1)"
                cx="2"
                cy="2"
                class="node-shadow"
              />
              
              <!-- Node Background -->
              <circle 
                :r="getNodeRadius(service)"
                fill="url(#service-gradient)"
                :stroke="getNodeBorderColor(service)"
                stroke-width="3"
                class="node-background"
              />
              
              <!-- Service Icon Background -->
              <circle 
                :r="getNodeRadius(service) - 8"
                :fill="getServiceColor(service.type)"
                class="icon-background"
              />
              
              <!-- Service Icon -->
              <text 
                x="0" 
                y="6" 
                text-anchor="middle"
                class="service-icon"
                :style="{ fontSize: `${Math.max(16, getNodeRadius(service) / 2)}px` }"
              >
                {{ getServiceIcon(service.type) }}
              </text>
              
              <!-- Service Label -->
              <text 
                x="0" 
                :y="getNodeRadius(service) + 20"
                text-anchor="middle"
                class="service-label"
              >
                {{ service.name }}
              </text>
              
              <!-- Health Indicator -->
              <circle 
                :cx="getNodeRadius(service) - 8"
                :cy="-getNodeRadius(service) + 8"
                r="6"
                :fill="getHealthColor(service.health)"
                stroke="white"
                stroke-width="2"
                class="health-indicator"
              />
              
              <!-- Activity Pulse -->
              <circle 
                v-if="service.activity > 80"
                :r="getNodeRadius(service)"
                fill="none"
                :stroke="getServiceColor(service.type)"
                stroke-width="2"
                opacity="0.6"
                class="activity-pulse"
              />
              
              <!-- Metrics Overlay -->
              <g v-if="selectedService === service.id" class="metrics-overlay">
                <rect 
                  x="-60"
                  :y="getNodeRadius(service) + 35"
                  width="120"
                  height="50"
                  rx="8"
                  fill="rgba(0, 0, 0, 0.9)"
                  stroke="rgba(255, 255, 255, 0.2)"
                  stroke-width="1"
                />
                
                <text x="0" :y="getNodeRadius(service) + 50" text-anchor="middle" class="metric-label">CPU: {{ service.cpu }}%</text>
                <text x="0" :y="getNodeRadius(service) + 65" text-anchor="middle" class="metric-label">Memory: {{ service.memory }}%</text>
                <text x="0" :y="getNodeRadius(service) + 80" text-anchor="middle" class="metric-label">{{ service.requestsPerSec }}/sec</text>
              </g>
            </g>
          </g>
        </svg>
      </div>
      
      <!-- Minimap -->
      <div class="minimap" v-if="showMinimap">
        <svg class="minimap-svg" viewBox="0 0 200 120">
          <rect width="200" height="120" fill="rgba(0, 0, 0, 0.1)" stroke="rgba(0, 0, 0, 0.2)" stroke-width="1" rx="4"/>
          
          <!-- Minimap Services -->
          <circle v-for="service in positionedServices" :key="`mini-${service.id}`"
                  :cx="(service.x + 1000) / 20"
                  :cy="(service.y + 600) / 10"
                  r="3"
                  :fill="getServiceColor(service.type)"
                  opacity="0.8" />
          
          <!-- Viewport Rectangle -->
          <rect 
            :x="viewportRect.x"
            :y="viewportRect.y"
            :width="viewportRect.width"
            :height="viewportRect.height"
            fill="none"
            stroke="#3b82f6"
            stroke-width="2"
            rx="2"
          />
        </svg>
      </div>
    </div>

    <!-- Service Details Panel -->
    <div v-if="selectedServiceDetails" class="details-panel">
      <div class="details-header">
        <div class="service-summary">
          <div class="service-icon-large" :style="{ backgroundColor: getServiceColor(selectedServiceDetails.type) }">
            {{ getServiceIcon(selectedServiceDetails.type) }}
          </div>
          <div class="service-info">
            <h3 class="service-name">{{ selectedServiceDetails.name }}</h3>
            <p class="service-meta">{{ selectedServiceDetails.namespace }} • {{ selectedServiceDetails.type }}</p>
          </div>
        </div>
        
        <button @click="closeDetails" class="close-btn">
          <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
            <path d="M15 5L5 15M5 5l10 10" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
          </svg>
        </button>
      </div>
      
      <div class="details-content">
        <!-- Performance Metrics -->
        <div class="metric-section">
          <h4 class="section-title">Performance Metrics</h4>
          <div class="metrics-grid">
            <div class="metric-card">
              <div class="metric-header">
                <span class="metric-name">CPU Usage</span>
                <span class="metric-value">{{ selectedServiceDetails.cpu }}%</span>
              </div>
              <div class="metric-bar">
                <div class="metric-fill cpu-fill" :style="{ width: `${selectedServiceDetails.cpu}%` }"></div>
              </div>
            </div>
            
            <div class="metric-card">
              <div class="metric-header">
                <span class="metric-name">Memory Usage</span>
                <span class="metric-value">{{ selectedServiceDetails.memory }}%</span>
              </div>
              <div class="metric-bar">
                <div class="metric-fill memory-fill" :style="{ width: `${selectedServiceDetails.memory}%` }"></div>
              </div>
            </div>
            
            <div class="metric-card">
              <div class="metric-header">
                <span class="metric-name">Request Rate</span>
                <span class="metric-value">{{ selectedServiceDetails.requestsPerSec }}/sec</span>
              </div>
            </div>
            
            <div class="metric-card">
              <div class="metric-header">
                <span class="metric-name">Error Rate</span>
                <span class="metric-value" :class="{ critical: selectedServiceDetails.errorRate > 5 }">{{ selectedServiceDetails.errorRate }}%</span>
              </div>
            </div>
          </div>
        </div>
        
        <!-- Connected Services -->
        <div class="connections-section">
          <h4 class="section-title">Connected Services</h4>
          <div class="connections-list">
            <div v-for="conn in getServiceConnections(selectedServiceDetails.id)" :key="conn.id" 
                 class="connection-item"
                 :class="`connection-${conn.status}`">
              <div class="connection-direction">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                  <path d="M2 8h10M10 4l4 4-4 4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
                </svg>
              </div>
              <div class="connection-details">
                <span class="connection-target">{{ conn.target }}</span>
                <span class="connection-protocol">{{ conn.protocol }}</span>
              </div>
              <div class="connection-metrics">
                <span class="connection-throughput">{{ formatThroughput(conn.throughput) }}</span>
                <span class="connection-latency">{{ conn.latency }}ms</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Tooltip -->
    <div v-if="tooltip.visible" class="tooltip" :style="{ left: tooltip.x + 'px', top: tooltip.y + 'px' }">
      <div class="tooltip-content" v-html="tooltip.content"></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'

interface Service {
  id: string
  name: string
  type: 'frontend' | 'api' | 'service' | 'database' | 'cache'
  namespace: string
  health: 'healthy' | 'warning' | 'error'
  cpu: number
  memory: number
  requestsPerSec: number
  errorRate: number
  activity: number
  x: number
  y: number
}

interface Connection {
  id: string
  source: string
  target: string
  protocol: string
  status: 'healthy' | 'warning' | 'error'
  throughput: number
  latency: number
  showMetrics: boolean
}

interface Tooltip {
  visible: boolean
  x: number
  y: number
  content: string
}

// Component state
const selectedView = ref('logical')
const selectedNamespace = ref('')
const timeWindow = ref('5m')
const realTimeEnabled = ref(true)
const selectedService = ref('')
const highlightedServices = ref<string[]>([])
const zoomLevel = ref(1)
const showMinimap = ref(true)

// Canvas state
const canvasContainer = ref<HTMLElement>()
const isPanning = ref(false)
const panStart = ref({ x: 0, y: 0 })
const panOffset = ref({ x: 0, y: 0 })

// Tooltip state
const tooltip = ref<Tooltip>({
  visible: false,
  x: 0,
  y: 0,
  content: ''
})

// Mock data
const namespaces = ['production', 'staging', 'development']

const services = ref<Service[]>([
  {
    id: 'frontend',
    name: 'Frontend App',
    type: 'frontend',
    namespace: 'production',
    health: 'healthy',
    cpu: 45,
    memory: 62,
    requestsPerSec: 120,
    errorRate: 0.1,
    activity: 85,
    x: -200,
    y: -100
  },
  {
    id: 'api-gateway',
    name: 'API Gateway',
    type: 'api',
    namespace: 'production',
    health: 'healthy',
    cpu: 67,
    memory: 58,
    requestsPerSec: 890,
    errorRate: 1.2,
    activity: 95,
    x: 0,
    y: -100
  },
  {
    id: 'auth-service',
    name: 'Auth Service',
    type: 'service',
    namespace: 'production',
    health: 'warning',
    cpu: 78,
    memory: 85,
    requestsPerSec: 340,
    errorRate: 3.1,
    activity: 70,
    x: -150,
    y: 100
  },
  {
    id: 'user-service',
    name: 'User Service',
    type: 'service',
    namespace: 'production',
    health: 'healthy',
    cpu: 34,
    memory: 41,
    requestsPerSec: 560,
    errorRate: 0.8,
    activity: 88,
    x: 150,
    y: 100
  },
  {
    id: 'postgres',
    name: 'PostgreSQL',
    type: 'database',
    namespace: 'production',
    health: 'healthy',
    cpu: 23,
    memory: 67,
    requestsPerSec: 780,
    errorRate: 0.2,
    activity: 60,
    x: 0,
    y: 250
  },
  {
    id: 'redis',
    name: 'Redis Cache',
    type: 'cache',
    namespace: 'production',
    health: 'error',
    cpu: 89,
    memory: 91,
    requestsPerSec: 1200,
    errorRate: 8.5,
    activity: 95,
    x: -200,
    y: 250
  }
])

const connections = ref<Connection[]>([
  {
    id: 'conn-1',
    source: 'frontend',
    target: 'api-gateway',
    protocol: 'HTTPS',
    status: 'healthy',
    throughput: 1200,
    latency: 45,
    showMetrics: false
  },
  {
    id: 'conn-2',
    source: 'api-gateway',
    target: 'auth-service',
    protocol: 'gRPC',
    status: 'warning',
    throughput: 340,
    latency: 120,
    showMetrics: false
  },
  {
    id: 'conn-3',
    source: 'api-gateway',
    target: 'user-service',
    protocol: 'gRPC',
    status: 'healthy',
    throughput: 560,
    latency: 32,
    showMetrics: false
  },
  {
    id: 'conn-4',
    source: 'user-service',
    target: 'postgres',
    protocol: 'TCP',
    status: 'healthy',
    throughput: 780,
    latency: 8,
    showMetrics: false
  },
  {
    id: 'conn-5',
    source: 'auth-service',
    target: 'redis',
    protocol: 'TCP',
    status: 'error',
    throughput: 200,
    latency: 250,
    showMetrics: false
  }
])

// Computed properties
const positionedServices = computed(() => {
  return services.value.filter(service => {
    if (selectedNamespace.value && service.namespace !== selectedNamespace.value) {
      return false
    }
    return true
  })
})

const visibleConnections = computed(() => {
  return connections.value.filter(conn => {
    const sourceExists = positionedServices.value.some(s => s.id === conn.source)
    const targetExists = positionedServices.value.some(s => s.id === conn.target)
    return sourceExists && targetExists
  })
})

const selectedServiceDetails = computed(() => {
  return selectedService.value ? services.value.find(s => s.id === selectedService.value) : null
})

const viewBox = computed(() => {
  const zoom = zoomLevel.value
  const offsetX = panOffset.value.x
  const offsetY = panOffset.value.y
  const width = 1000 / zoom
  const height = 600 / zoom
  const x = -500 / zoom + offsetX
  const y = -300 / zoom + offsetY
  return `${x} ${y} ${width} ${height}`
})

const viewportRect = computed(() => {
  // Calculate viewport rectangle for minimap
  const zoom = zoomLevel.value
  const x = Math.max(0, Math.min(150, (panOffset.value.x + 500) / 20))
  const y = Math.max(0, Math.min(90, (panOffset.value.y + 300) / 10))
  const width = Math.min(50, 200 / zoom)
  const height = Math.min(30, 120 / zoom)
  return { x, y, width, height }
})

// Methods
const toggleRealTime = () => {
  realTimeEnabled.value = !realTimeEnabled.value
}

const centerView = () => {
  panOffset.value = { x: 0, y: 0 }
  zoomLevel.value = 1
}

const fitToScreen = () => {
  // Calculate bounds of all services
  const services = positionedServices.value
  if (services.length === 0) return
  
  const minX = Math.min(...services.map(s => s.x)) - 100
  const maxX = Math.max(...services.map(s => s.x)) + 100
  const minY = Math.min(...services.map(s => s.y)) - 100
  const maxY = Math.max(...services.map(s => s.y)) + 100
  
  const width = maxX - minX
  const height = maxY - minY
  
  const scaleX = 800 / width
  const scaleY = 500 / height
  const scale = Math.min(scaleX, scaleY, 2)
  
  zoomLevel.value = scale * 0.8
  panOffset.value = {
    x: -(minX + width / 2),
    y: -(minY + height / 2)
  }
}

const zoomIn = () => {
  zoomLevel.value = Math.min(3, zoomLevel.value * 1.2)
}

const zoomOut = () => {
  zoomLevel.value = Math.max(0.3, zoomLevel.value / 1.2)
}

const handleWheel = (event: WheelEvent) => {
  event.preventDefault()
  const delta = event.deltaY > 0 ? 0.9 : 1.1
  zoomLevel.value = Math.max(0.3, Math.min(3, zoomLevel.value * delta))
}

const startPan = (event: MouseEvent) => {
  isPanning.value = true
  panStart.value = { x: event.clientX, y: event.clientY }
}

const handlePan = (event: MouseEvent) => {
  if (!isPanning.value) return
  
  const deltaX = (event.clientX - panStart.value.x) / zoomLevel.value
  const deltaY = (event.clientY - panStart.value.y) / zoomLevel.value
  
  panOffset.value = {
    x: panOffset.value.x + deltaX,
    y: panOffset.value.y + deltaY
  }
  
  panStart.value = { x: event.clientX, y: event.clientY }
}

const endPan = () => {
  isPanning.value = false
}

const selectService = (service: Service) => {
  if (selectedService.value === service.id) {
    selectedService.value = ''
    highlightedServices.value = []
  } else {
    selectedService.value = service.id
    // Highlight connected services
    const connected = connections.value
      .filter(conn => conn.source === service.id || conn.target === service.id)
      .map(conn => conn.source === service.id ? conn.target : conn.source)
    highlightedServices.value = connected
  }
}

const closeDetails = () => {
  selectedService.value = ''
  highlightedServices.value = []
}

const getNodeRadius = (service: Service) => {
  const baseRadius = 30
  const activityBonus = (service.activity / 100) * 10
  return baseRadius + activityBonus
}

const getNodeBorderColor = (service: Service) => {
  const colors = {
    healthy: '#10b981',
    warning: '#f59e0b',
    error: '#ef4444'
  }
  return colors[service.health]
}

const getServiceColor = (type: string) => {
  const colors = {
    frontend: '#3b82f6',
    api: '#8b5cf6',
    service: '#06b6d4',
    database: '#10b981',
    cache: '#f59e0b'
  }
  return colors[type] || '#6b7280'
}

const getServiceIcon = (type: string) => {
  const icons = {
    frontend: '🌐',
    api: '🔌',
    service: '⚙️',
    database: '🗄️',
    cache: '💾'
  }
  return icons[type] || '📦'
}

const getHealthColor = (health: string) => {
  const colors = {
    healthy: '#10b981',
    warning: '#f59e0b',
    error: '#ef4444'
  }
  return colors[health]
}

const getConnectionPath = (connection: Connection) => {
  const source = services.value.find(s => s.id === connection.source)
  const target = services.value.find(s => s.id === connection.target)
  
  if (!source || !target) return ''
  
  // Calculate curved path
  const dx = target.x - source.x
  const dy = target.y - source.y
  const distance = Math.sqrt(dx * dx + dy * dy)
  
  const sourceRadius = getNodeRadius(source)
  const targetRadius = getNodeRadius(target)
  
  // Calculate start and end points on node borders
  const startX = source.x + (dx / distance) * sourceRadius
  const startY = source.y + (dy / distance) * sourceRadius
  const endX = target.x - (dx / distance) * targetRadius
  const endY = target.y - (dy / distance) * targetRadius
  
  // Create curved path
  const midX = (startX + endX) / 2
  const midY = (startY + endY) / 2
  const controlOffset = Math.min(50, distance / 4)
  const controlX = midX + controlOffset * (dy / distance)
  const controlY = midY - controlOffset * (dx / distance)
  
  return `M ${startX} ${startY} Q ${controlX} ${controlY} ${endX} ${endY}`
}

const getConnectionWidth = (connection: Connection) => {
  const baseWidth = 2
  const throughputWidth = Math.min(8, (connection.throughput / 1000) * 6)
  return baseWidth + throughputWidth
}

const getConnectionMidpoint = (connection: Connection) => {
  const source = services.value.find(s => s.id === connection.source)
  const target = services.value.find(s => s.id === connection.target)
  
  if (!source || !target) return { x: 0, y: 0 }
  
  return {
    x: (source.x + target.x) / 2,
    y: (source.y + target.y) / 2
  }
}

const formatThroughput = (value: number) => {
  if (value >= 1000) {
    return `${(value / 1000).toFixed(1)}k/s`
  }
  return `${value}/s`
}

const showServiceTooltip = (service: Service, event: MouseEvent) => {
  tooltip.value = {
    visible: true,
    x: event.clientX + 10,
    y: event.clientY + 10,
    content: `
      <div class="tooltip-service">
        <div class="tooltip-header">
          <strong>${service.name}</strong>
          <span class="health-badge health-${service.health}">${service.health}</span>
        </div>
        <div class="tooltip-metrics">
          <div>CPU: ${service.cpu}%</div>
          <div>Memory: ${service.memory}%</div>
          <div>Requests: ${service.requestsPerSec}/sec</div>
          <div>Error Rate: ${service.errorRate}%</div>
        </div>
      </div>
    `
  }
}

const showConnectionTooltip = (connection: Connection, event: MouseEvent) => {
  const source = services.value.find(s => s.id === connection.source)
  const target = services.value.find(s => s.id === connection.target)
  
  tooltip.value = {
    visible: true,
    x: event.clientX + 10,
    y: event.clientY + 10,
    content: `
      <div class="tooltip-connection">
        <div class="tooltip-header">
          <strong>${source?.name} → ${target?.name}</strong>
          <span class="status-badge status-${connection.status}">${connection.status}</span>
        </div>
        <div class="tooltip-metrics">
          <div>Protocol: ${connection.protocol}</div>
          <div>Throughput: ${formatThroughput(connection.throughput)}</div>
          <div>Latency: ${connection.latency}ms</div>
        </div>
      </div>
    `
  }
}

const hideTooltip = () => {
  tooltip.value.visible = false
}

const getServiceConnections = (serviceId: string) => {
  return connections.value
    .filter(conn => conn.source === serviceId || conn.target === serviceId)
    .map(conn => ({
      ...conn,
      target: conn.source === serviceId ? conn.target : conn.source,
      direction: conn.source === serviceId ? 'outbound' : 'inbound'
    }))
}

// Lifecycle
onMounted(() => {
  fitToScreen()
  
  // Add global event listeners
  document.addEventListener('mousemove', handlePan)
  document.addEventListener('mouseup', endPan)
})

onUnmounted(() => {
  document.removeEventListener('mousemove', handlePan)
  document.removeEventListener('mouseup', endPan)
})
</script>

<style scoped>
.modern-topology {
  height: 100vh;
  display: flex;
  flex-direction: column;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: #1f2937;
  overflow: hidden;
}

/* Command Bar */
.command-bar {
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(20px);
  border-bottom: 1px solid rgba(0, 0, 0, 0.08);
  padding: 16px 24px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

.command-content {
  display: flex;
  justify-content: space-between;
  align-items: center;
  max-width: 1400px;
  margin: 0 auto;
}

.view-controls {
  display: flex;
  gap: 24px;
  align-items: center;
}

.control-group {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.control-label {
  font-size: 12px;
  font-weight: 600;
  color: #6b7280;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.control-select {
  padding: 8px 12px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  border-radius: 8px;
  background: white;
  font-size: 14px;
  font-weight: 500;
  color: #374151;
  min-width: 140px;
  transition: all 0.2s;
}

.control-select:focus {
  outline: none;
  border-color: #667eea;
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.1);
}

.action-controls {
  display: flex;
  gap: 12px;
  align-items: center;
}

.real-time-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  border-radius: 8px;
  background: white;
  font-size: 14px;
  font-weight: 500;
  color: #6b7280;
  cursor: pointer;
  transition: all 0.2s;
}

.real-time-btn:hover {
  background: rgba(0, 0, 0, 0.04);
}

.real-time-btn.active {
  background: rgba(16, 185, 129, 0.1);
  border-color: rgba(16, 185, 129, 0.3);
  color: #065f46;
}

.real-time-indicator {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #ef4444;
}

.real-time-btn.active .real-time-indicator {
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
  font-size: 14px;
  font-weight: 500;
  color: #6b7280;
  cursor: pointer;
  transition: all 0.2s;
}

.action-btn:hover {
  background: rgba(0, 0, 0, 0.04);
  color: #374151;
}

.zoom-controls {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px;
  background: rgba(0, 0, 0, 0.04);
  border-radius: 8px;
}

.zoom-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: #6b7280;
  cursor: pointer;
  transition: all 0.2s;
}

.zoom-btn:hover {
  background: rgba(0, 0, 0, 0.1);
  color: #374151;
}

.zoom-level {
  font-size: 12px;
  font-weight: 600;
  color: #374151;
  min-width: 40px;
  text-align: center;
  font-variant-numeric: tabular-nums;
}

/* Topology Canvas */
.topology-canvas {
  flex: 1;
  position: relative;
  overflow: hidden;
}

.canvas-background {
  width: 100%;
  height: 100%;
  position: relative;
}

.topology-svg {
  width: 100%;
  height: 100%;
  cursor: grab;
}

.topology-svg:active {
  cursor: grabbing;
}

/* Service Nodes */
.service-node {
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.service-node:hover {
  transform: scale(1.05);
}

.service-node.selected {
  filter: drop-shadow(0 8px 16px rgba(102, 126, 234, 0.4));
}

.service-node.highlighted {
  filter: drop-shadow(0 4px 8px rgba(0, 0, 0, 0.2));
}

.node-shadow {
  opacity: 0.3;
}

.node-background {
  transition: all 0.3s;
}

.icon-background {
  opacity: 0.1;
}

.service-icon {
  fill: white;
  font-weight: bold;
  pointer-events: none;
}

.service-label {
  font-size: 12px;
  font-weight: 600;
  fill: #374151;
  pointer-events: none;
}

.health-indicator {
  transition: all 0.3s;
}

.activity-pulse {
  animation: activity-pulse 2s infinite;
}

@keyframes activity-pulse {
  0%, 100% { 
    opacity: 0.6;
    transform: scale(1);
  }
  50% { 
    opacity: 0.3;
    transform: scale(1.1);
  }
}

.metrics-overlay {
  opacity: 0;
  animation: fade-in 0.3s ease-out forwards;
}

@keyframes fade-in {
  from { opacity: 0; transform: translateY(5px); }
  to { opacity: 1; transform: translateY(0); }
}

.metric-label {
  font-size: 10px;
  fill: rgba(255, 255, 255, 0.9);
  font-weight: 500;
}

/* Connections */
.connection-path {
  cursor: pointer;
  transition: all 0.3s;
}

.connection-path:hover {
  stroke-width: 6px !important;
  filter: drop-shadow(0 2px 4px rgba(0, 0, 0, 0.2));
}

.connection-metric-primary {
  font-size: 11px;
  font-weight: 600;
  fill: #374151;
}

.connection-metric-secondary {
  font-size: 9px;
  fill: #6b7280;
}

/* Minimap */
.minimap {
  position: absolute;
  bottom: 20px;
  right: 20px;
  width: 200px;
  height: 120px;
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(10px);
  border-radius: 8px;
  padding: 8px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}

.minimap-svg {
  width: 100%;
  height: 100%;
}

/* Details Panel */
.details-panel {
  position: fixed;
  top: 80px;
  right: 24px;
  width: 360px;
  max-height: calc(100vh - 120px);
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(20px);
  border-radius: 16px;
  box-shadow: 0 10px 25px rgba(0, 0, 0, 0.15);
  border: 1px solid rgba(255, 255, 255, 0.2);
  overflow: hidden;
  z-index: 1000;
  animation: slide-in 0.3s ease-out;
}

@keyframes slide-in {
  from { 
    opacity: 0; 
    transform: translateX(100%); 
  }
  to { 
    opacity: 1; 
    transform: translateX(0); 
  }
}

.details-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 20px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.08);
}

.service-summary {
  display: flex;
  align-items: center;
  gap: 12px;
}

.service-icon-large {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  color: white;
}

.service-info {
  flex: 1;
}

.service-name {
  font-size: 18px;
  font-weight: 700;
  color: #111827;
  margin: 0;
}

.service-meta {
  font-size: 14px;
  color: #6b7280;
  margin: 2px 0 0 0;
}

.close-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: none;
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.04);
  color: #6b7280;
  cursor: pointer;
  transition: all 0.2s;
}

.close-btn:hover {
  background: rgba(0, 0, 0, 0.1);
  color: #374151;
}

.details-content {
  padding: 20px;
  max-height: calc(100vh - 200px);
  overflow-y: auto;
}

.metric-section,
.connections-section {
  margin-bottom: 24px;
}

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: #374151;
  margin: 0 0 12px 0;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.metrics-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 12px;
}

.metric-card {
  padding: 12px;
  background: rgba(0, 0, 0, 0.02);
  border-radius: 8px;
  border: 1px solid rgba(0, 0, 0, 0.05);
}

.metric-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.metric-name {
  font-size: 12px;
  font-weight: 500;
  color: #6b7280;
}

.metric-value {
  font-size: 14px;
  font-weight: 700;
  color: #111827;
}

.metric-value.critical {
  color: #ef4444;
}

.metric-bar {
  width: 100%;
  height: 4px;
  background: rgba(0, 0, 0, 0.1);
  border-radius: 2px;
  overflow: hidden;
}

.metric-fill {
  height: 100%;
  border-radius: 2px;
  transition: width 0.3s ease;
}

.metric-fill.cpu-fill {
  background: linear-gradient(90deg, #3b82f6, #1d4ed8);
}

.metric-fill.memory-fill {
  background: linear-gradient(90deg, #10b981, #047857);
}

.connections-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.connection-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  background: rgba(0, 0, 0, 0.02);
  border-radius: 8px;
  border: 1px solid rgba(0, 0, 0, 0.05);
  transition: all 0.2s;
}

.connection-item:hover {
  background: rgba(0, 0, 0, 0.04);
}

.connection-item.connection-healthy {
  border-left: 3px solid #10b981;
}

.connection-item.connection-warning {
  border-left: 3px solid #f59e0b;
}

.connection-item.connection-error {
  border-left: 3px solid #ef4444;
}

.connection-direction {
  color: #6b7280;
}

.connection-details {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.connection-target {
  font-size: 14px;
  font-weight: 600;
  color: #111827;
}

.connection-protocol {
  font-size: 12px;
  color: #6b7280;
}

.connection-metrics {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 2px;
}

.connection-throughput {
  font-size: 12px;
  font-weight: 600;
  color: #374151;
}

.connection-latency {
  font-size: 11px;
  color: #6b7280;
}

/* Tooltip */
.tooltip {
  position: fixed;
  z-index: 2000;
  pointer-events: none;
  animation: tooltip-fade-in 0.2s ease-out;
}

@keyframes tooltip-fade-in {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}

.tooltip-content {
  background: rgba(26, 32, 44, 0.95);
  color: white;
  padding: 12px;
  border-radius: 8px;
  font-size: 12px;
  line-height: 1.4;
  backdrop-filter: blur(8px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  max-width: 250px;
}

.tooltip-service,
.tooltip-connection {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.tooltip-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  padding-bottom: 6px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.2);
}

.tooltip-metrics {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 4px;
  font-size: 11px;
}

.health-badge,
.status-badge {
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
}

.health-badge.health-healthy,
.status-badge.status-healthy {
  background: rgba(16, 185, 129, 0.2);
  color: #6ee7b7;
}

.health-badge.health-warning,
.status-badge.status-warning {
  background: rgba(245, 158, 11, 0.2);
  color: #fbbf24;
}

.health-badge.health-error,
.status-badge.status-error {
  background: rgba(239, 68, 68, 0.2);
  color: #fca5a5;
}

/* Responsive Design */
@media (max-width: 1024px) {
  .view-controls {
    gap: 16px;
  }
  
  .details-panel {
    width: 320px;
  }
  
  .minimap {
    width: 160px;
    height: 96px;
  }
}

@media (max-width: 768px) {
  .command-content {
    flex-direction: column;
    gap: 16px;
    align-items: stretch;
  }
  
  .view-controls {
    justify-content: space-between;
  }
  
  .details-panel {
    position: relative;
    top: auto;
    right: auto;
    width: 100%;
    max-height: 50vh;
    margin: 16px;
  }
  
  .minimap {
    display: none;
  }
}
</style>