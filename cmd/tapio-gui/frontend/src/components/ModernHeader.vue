<template>
  <header class="modern-header">
    <div class="header-glass">
      <div class="header-content">
        <!-- Logo & Brand -->
        <div class="brand">
          <div class="logo-container">
            <div class="logo-icon">
              <svg width="28" height="28" viewBox="0 0 28 28" fill="none">
                <defs>
                  <linearGradient id="logoGradient" x1="0%" y1="0%" x2="100%" y2="100%">
                    <stop offset="0%" style="stop-color:#667eea;stop-opacity:1" />
                    <stop offset="100%" style="stop-color:#764ba2;stop-opacity:1" />
                  </linearGradient>
                </defs>
                <circle cx="14" cy="14" r="12" fill="url(#logoGradient)" />
                <path d="M10 14l3 3 6-6" stroke="white" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"/>
              </svg>
            </div>
            <div class="logo-text">
              <span class="brand-name">Tapio</span>
              <span class="brand-tag">Intelligence Platform</span>
            </div>
          </div>
        </div>

        <!-- Navigation -->
        <nav class="nav-section">
          <div class="nav-pills">
            <button 
              v-for="tab in navigationTabs" 
              :key="tab.id"
              @click="$emit('tab-change', tab.id)"
              :class="['nav-pill', { active: activeTab === tab.id }]"
            >
              <component :is="'div'" v-html="tab.icon" class="pill-icon"></component>
              <span class="pill-text">{{ tab.label }}</span>
              <div v-if="tab.badge" class="pill-badge">{{ tab.badge }}</div>
            </button>
          </div>
        </nav>

        <!-- Status & Actions -->
        <div class="actions-section">
          <!-- Global Search -->
          <div class="search-wrapper">
            <div class="search-glass">
              <svg class="search-icon" width="18" height="18" viewBox="0 0 18 18" fill="none">
                <path d="M8 14A6 6 0 1 0 8 2a6 6 0 0 0 0 12zM15 15l-4-4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/>
              </svg>
              <input 
                type="text" 
                placeholder="Search clusters, services, metrics..."
                class="search-input"
                v-model="searchQuery"
              />
              <kbd class="search-shortcut">⌘K</kbd>
            </div>
          </div>

          <!-- Real-time Status -->
          <div class="status-section">
            <div class="status-indicator" :class="{ connected: isConnected, disconnected: !isConnected }">
              <div class="status-pulse"></div>
              <div class="status-content">
                <span class="status-label">{{ isConnected ? 'Live' : 'Offline' }}</span>
                <span class="status-detail">{{ isConnected ? lastUpdateTime : 'Disconnected' }}</span>
              </div>
            </div>
          </div>

          <!-- Settings & Profile -->
          <div class="profile-section">
            <button class="settings-btn">
              <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
                <path d="M10 12.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5z" stroke="currentColor" stroke-width="1.5"/>
                <path d="M17.5 10a7.5 7.5 0 0 1-.46 2.75l-1.46-.84a6 6 0 0 0 0-3.82l1.46-.84A7.5 7.5 0 0 1 17.5 10zM2.5 10a7.5 7.5 0 0 1 .46-2.75l1.46.84a6 6 0 0 0 0 3.82l-1.46.84A7.5 7.5 0 0 1 2.5 10zM13.25 2.5a7.5 7.5 0 0 1 2.75.46l-.84 1.46a6 6 0 0 0-3.82 0l-.84-1.46A7.5 7.5 0 0 1 13.25 2.5zM6.75 17.5a7.5 7.5 0 0 1-2.75-.46l.84-1.46a6 6 0 0 0 3.82 0l.84 1.46a7.5 7.5 0 0 1-2.75.46z" stroke="currentColor" stroke-width="1.5"/>
              </svg>
            </button>
            
            <div class="profile-avatar">
              <div class="avatar-ring">
                <div class="avatar-image">👤</div>
              </div>
              <div class="profile-info">
                <span class="profile-name">Admin</span>
                <span class="profile-role">Platform Owner</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

interface NavigationTab {
  id: string
  label: string
  icon: string
  badge?: string
}

defineProps<{
  activeTab: string
  isConnected: boolean
}>()

defineEmits<{
  'tab-change': [tabId: string]
}>()

const searchQuery = ref('')

const lastUpdateTime = computed(() => {
  return new Date().toLocaleTimeString('en-US', { 
    hour12: false, 
    hour: '2-digit', 
    minute: '2-digit', 
    second: '2-digit' 
  })
})

const navigationTabs: NavigationTab[] = [
  {
    id: 'overview',
    label: 'Overview',
    icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M2 3h5v5H2zM9 3h5v3H9zM9 8h5v5H9zM2 10h5v3H2z" stroke="currentColor" stroke-width="1.2" fill="currentColor" fill-opacity="0.1"/></svg>'
  },
  {
    id: 'topology',
    label: 'Service Map',
    icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><circle cx="4" cy="4" r="2" stroke="currentColor" stroke-width="1.2" fill="currentColor" fill-opacity="0.1"/><circle cx="12" cy="4" r="2" stroke="currentColor" stroke-width="1.2" fill="currentColor" fill-opacity="0.1"/><circle cx="8" cy="12" r="2" stroke="currentColor" stroke-width="1.2" fill="currentColor" fill-opacity="0.1"/><path d="M6 5l4 6M10 5l-4 6" stroke="currentColor" stroke-width="1.2"/></svg>'
  },
  {
    id: 'metrics',
    label: 'Metrics',
    icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M2 14V6l3-3 2 2 3-3 4 4v8" stroke="currentColor" stroke-width="1.2" fill="currentColor" fill-opacity="0.1"/></svg>',
    badge: '3'
  },
  {
    id: 'incidents',
    label: 'Incidents',
    icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M8 1l2 6h5l-4 3 1.5 5.5L8 12l-4.5 3.5L5 10l-4-3h5l2-6z" stroke="currentColor" stroke-width="1.2" fill="currentColor" fill-opacity="0.1"/></svg>',
    badge: '2'
  },
  {
    id: 'traces',
    label: 'Traces',
    icon: '<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M2 4h12M2 8h12M2 12h8" stroke="currentColor" stroke-width="1.2" stroke-linecap="round"/></svg>'
  }
]
</script>

<style scoped>
.modern-header {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 1000;
  height: 64px;
}

.header-glass {
  height: 100%;
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(20px) saturate(180%);
  border-bottom: 1px solid rgba(0, 0, 0, 0.08);
  box-shadow: 
    0 1px 0 0 rgba(255, 255, 255, 0.7) inset,
    0 0.5px 2px rgba(0, 0, 0, 0.04),
    0 2px 8px rgba(0, 0, 0, 0.06);
}

.header-content {
  display: flex;
  align-items: center;
  height: 100%;
  max-width: 1400px;
  margin: 0 auto;
  padding: 0 24px;
  gap: 32px;
}

/* Brand Section */
.brand {
  flex-shrink: 0;
}

.logo-container {
  display: flex;
  align-items: center;
  gap: 12px;
}

.logo-icon {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
}

.logo-icon::after {
  content: '';
  position: absolute;
  inset: -4px;
  border-radius: 50%;
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.1), rgba(118, 75, 162, 0.1));
  z-index: -1;
}

.logo-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.brand-name {
  font-size: 18px;
  font-weight: 700;
  color: #1a1d29;
  letter-spacing: -0.5px;
}

.brand-tag {
  font-size: 11px;
  font-weight: 500;
  color: #6b7280;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

/* Navigation */
.nav-section {
  flex: 1;
  display: flex;
  justify-content: center;
}

.nav-pills {
  display: flex;
  gap: 6px;
  padding: 6px;
  background: rgba(0, 0, 0, 0.04);
  border-radius: 12px;
  backdrop-filter: blur(10px);
}

.nav-pill {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: #6b7280;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  position: relative;
  overflow: hidden;
}

.nav-pill::before {
  content: '';
  position: absolute;
  inset: 0;
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.08), rgba(118, 75, 162, 0.08));
  opacity: 0;
  transition: opacity 0.2s;
}

.nav-pill:hover::before {
  opacity: 1;
}

.nav-pill.active {
  background: white;
  color: #667eea;
  box-shadow: 
    0 1px 3px rgba(0, 0, 0, 0.1),
    0 0 0 1px rgba(255, 255, 255, 0.9) inset;
}

.nav-pill.active::before {
  opacity: 0;
}

.pill-icon {
  opacity: 0.8;
  transition: opacity 0.2s;
}

.nav-pill.active .pill-icon {
  opacity: 1;
}

.pill-badge {
  background: linear-gradient(135deg, #ff6b6b, #ee5a24);
  color: white;
  font-size: 11px;
  font-weight: 600;
  padding: 2px 6px;
  border-radius: 8px;
  min-width: 18px;
  text-align: center;
  box-shadow: 0 1px 3px rgba(238, 90, 36, 0.4);
}

/* Actions Section */
.actions-section {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-shrink: 0;
}

/* Search */
.search-wrapper {
  position: relative;
}

.search-glass {
  display: flex;
  align-items: center;
  gap: 12px;
  background: rgba(0, 0, 0, 0.04);
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 10px;
  padding: 8px 12px;
  transition: all 0.2s;
  backdrop-filter: blur(10px);
}

.search-glass:focus-within {
  background: white;
  border-color: rgba(102, 126, 234, 0.3);
  box-shadow: 
    0 0 0 3px rgba(102, 126, 234, 0.1),
    0 2px 8px rgba(0, 0, 0, 0.1);
}

.search-icon {
  color: #9ca3af;
  flex-shrink: 0;
}

.search-input {
  border: none;
  outline: none;
  background: transparent;
  font-size: 14px;
  color: #374151;
  width: 240px;
}

.search-input::placeholder {
  color: #9ca3af;
}

.search-shortcut {
  font-size: 11px;
  color: #9ca3af;
  background: rgba(0, 0, 0, 0.06);
  border: 1px solid rgba(0, 0, 0, 0.1);
  border-radius: 4px;
  padding: 2px 5px;
  font-family: system-ui;
}

/* Status */
.status-section {
  position: relative;
}

.status-indicator {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.04);
  border: 1px solid rgba(0, 0, 0, 0.08);
  transition: all 0.2s;
}

.status-indicator.connected {
  background: rgba(16, 185, 129, 0.1);
  border-color: rgba(16, 185, 129, 0.2);
}

.status-indicator.disconnected {
  background: rgba(239, 68, 68, 0.1);
  border-color: rgba(239, 68, 68, 0.2);
}

.status-pulse {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #ef4444;
}

.status-indicator.connected .status-pulse {
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

.status-content {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.status-label {
  font-size: 12px;
  font-weight: 600;
  color: #374151;
}

.status-detail {
  font-size: 10px;
  color: #9ca3af;
  font-variant-numeric: tabular-nums;
}

/* Settings & Profile */
.profile-section {
  display: flex;
  align-items: center;
  gap: 12px;
}

.settings-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border: none;
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.04);
  border: 1px solid rgba(0, 0, 0, 0.08);
  color: #6b7280;
  cursor: pointer;
  transition: all 0.2s;
}

.settings-btn:hover {
  background: white;
  color: #374151;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

.profile-avatar {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  padding: 4px;
  border-radius: 8px;
  transition: background 0.2s;
}

.profile-avatar:hover {
  background: rgba(0, 0, 0, 0.04);
}

.avatar-ring {
  position: relative;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: linear-gradient(135deg, #667eea, #764ba2);
  padding: 2px;
}

.avatar-image {
  width: 100%;
  height: 100%;
  border-radius: 50%;
  background: white;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
}

.profile-info {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.profile-name {
  font-size: 13px;
  font-weight: 600;
  color: #374151;
}

.profile-role {
  font-size: 11px;
  color: #9ca3af;
}

/* Responsive */
@media (max-width: 1024px) {
  .search-input {
    width: 180px;
  }
  
  .profile-info {
    display: none;
  }
  
  .nav-pills {
    gap: 4px;
  }
  
  .nav-pill {
    padding: 8px 12px;
  }
}

@media (max-width: 768px) {
  .brand-tag {
    display: none;
  }
  
  .search-wrapper {
    display: none;
  }
  
  .status-detail {
    display: none;
  }
}
</style>