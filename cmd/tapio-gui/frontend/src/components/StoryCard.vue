<script lang="ts" setup>
import { ref } from 'vue'

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

interface Props {
  story: Story
}

interface Emits {
  (event: 'apply-fix', storyId: string, actionId: string): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const expanded = ref(false)
const applying = ref<string | null>(null)

const getSeverityColor = (severity: string) => {
  switch (severity) {
    case 'critical': return 'var(--color-status-critical)'
    case 'high': return 'var(--color-status-error)'
    case 'medium': return 'var(--color-status-warning)'
    case 'low': return 'var(--color-status-info)'
    default: return 'var(--color-text-tertiary)'
  }
}

const getSeverityIcon = (severity: string) => {
  switch (severity) {
    case 'critical': return '◆'
    case 'high': return '▲'
    case 'medium': return '●'
    case 'low': return '■'
    default: return '○'
  }
}

const getCategoryIcon = (category: string) => {
  switch (category) {
    case 'memory': return '◐'
    case 'cpu': return '◈'
    case 'network': return '◉'
    case 'storage': return '◎'
    case 'database': return '◊'
    case 'security': return '◇'
    default: return '○'
  }
}

const getResourceIcon = (type: string) => {
  switch (type) {
    case 'pod': return '▫'
    case 'service': return '▪'
    case 'deployment': return '◉'
    case 'node': return '▬'
    case 'configmap': return '◆'
    default: return '•'
  }
}

const getRiskColor = (risk: string) => {
  switch (risk) {
    case 'low': return 'var(--color-status-success)'
    case 'medium': return 'var(--color-status-warning)'
    case 'high': return 'var(--color-status-error)'
    default: return 'var(--color-text-tertiary)'
  }
}

const formatTimestamp = (timestamp: string) => {
  try {
    const date = new Date(timestamp)
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffMins = Math.floor(diffMs / 60000)
    
    if (diffMins < 1) return 'Just now'
    if (diffMins < 60) return `${diffMins}m ago`
    const diffHours = Math.floor(diffMins / 60)
    if (diffHours < 24) return `${diffHours}h ago`
    const diffDays = Math.floor(diffHours / 24)
    return `${diffDays}d ago`
  } catch {
    return timestamp
  }
}

const applyFix = async (actionId: string) => {
  applying.value = actionId
  try {
    emit('apply-fix', props.story.id, actionId)
    // Note: The parent component will handle the actual API call
    // and refresh the stories list
  } finally {
    setTimeout(() => {
      applying.value = null
    }, 2000)
  }
}

const toggleExpanded = () => {
  expanded.value = !expanded.value
}

const copyCommand = async (command: string) => {
  try {
    await navigator.clipboard.writeText(command)
    // Could add a toast notification here
  } catch (err) {
    console.error('Failed to copy command:', err)
  }
}
</script>

<template>
  <div class="story-card" :class="{ expanded }" :data-severity="story.severity">
    <!-- Story Header -->
    <div class="story-header" @click="toggleExpanded">
      <div class="story-indicator" :data-severity="story.severity"></div>
      
      <div class="story-main">
        <div class="story-meta">
          <div class="severity-badge" :data-severity="story.severity">
            <span class="severity-icon">{{ getSeverityIcon(story.severity) }}</span>
            <span class="severity-text">{{ story.severity }}</span>
          </div>
          
          <div class="meta-separator"></div>
          
          <div class="category-badge">
            <span class="category-icon">{{ getCategoryIcon(story.category) }}</span>
            <span class="category-text">{{ story.category }}</span>
          </div>
          
          <div class="meta-separator"></div>
          
          <div class="timestamp">{{ formatTimestamp(story.timestamp) }}</div>
        </div>
        
        <div class="story-title">
          <h3>{{ story.title }}</h3>
        </div>
        
        <p class="story-description">{{ story.description }}</p>
      </div>
      
      <button class="expand-btn" :class="{ expanded }" @click.stop="toggleExpanded">
        <svg width="12" height="12" viewBox="0 0 12 12" fill="none">
          <path d="M3 5L6 8L9 5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" :transform="expanded ? 'rotate(180 6 6)' : ''"/>
        </svg>
      </button>
    </div>

    <!-- Expanded Content -->
    <div v-if="expanded" class="story-content">
      <!-- Root Cause -->
      <div v-if="story.root_cause" class="content-section">
        <h4 class="section-title">
          <span class="section-icon">◆</span>
          Root Cause Analysis
        </h4>
        <div class="content-box">
          <p class="root-cause">{{ story.root_cause }}</p>
        </div>
      </div>

      <!-- Prediction -->
      <div v-if="story.prediction" class="content-section">
        <h4 class="section-title">
          <span class="section-icon">▲</span>
          Predicted Impact
        </h4>
        <div class="content-box prediction-box">
          <p class="prediction">{{ story.prediction }}</p>
        </div>
      </div>

      <!-- Affected Resources -->
      <div v-if="story.resources.length > 0" class="content-section">
        <h4 class="section-title">
          <span class="section-icon">●</span>
          Affected Resources
        </h4>
        <div class="resources-grid">
          <div
            v-for="resource in story.resources"
            :key="`${resource.type}-${resource.name}`"
            class="resource-item"
          >
            <div class="resource-icon">{{ getResourceIcon(resource.type) }}</div>
            <div class="resource-details">
              <div class="resource-name">{{ resource.name }}</div>
              <div class="resource-meta">
                <span class="resource-type">{{ resource.type }}</span>
                <span class="resource-separator" v-if="resource.namespace">•</span>
                <span class="resource-namespace" v-if="resource.namespace">{{ resource.namespace }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Recommended Actions -->
      <div v-if="story.actions.length > 0" class="content-section">
        <h4 class="section-title">
          <span class="section-icon">🛠️</span>
          Recommended Actions
        </h4>
        <div class="actions-list">
          <div
            v-for="action in story.actions"
            :key="action.id"
            class="action-item"
          >
            <div class="action-header">
              <div class="action-info">
                <div class="action-title">{{ action.title }}</div>
                <div class="action-description">{{ action.description }}</div>
              </div>
              <div class="action-meta">
                <div class="risk-badge" :data-risk="action.risk">
                  {{ action.risk }} risk
                </div>
                <button
                  @click="applyFix(action.id)"
                  :disabled="applying === action.id"
                  class="apply-btn"
                  :class="action.risk"
                >
                  <span v-if="applying === action.id">⟳</span>
                  <span v-else>🚀</span>
                  {{ applying === action.id ? 'Applying...' : 'Apply Fix' }}
                </button>
              </div>
            </div>
            
            <!-- Commands -->
            <div v-if="action.commands.length > 0" class="commands-section">
              <div class="commands-header">Commands:</div>
              <div
                v-for="(command, index) in action.commands"
                :key="index"
                class="command-item"
              >
                <code class="command-text">{{ command }}</code>
                <button
                  @click="copyCommand(command)"
                  class="copy-btn"
                  title="Copy command"
                >
                  📋
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Hubble-inspired Technical Cards */
.story-card {
  background: var(--color-bg-secondary);
  border: 1px solid var(--color-border-primary);
  border-radius: var(--radius-md);
  overflow: hidden;
  transition: all 0.2s ease;
  box-shadow: var(--shadow-sm);
  position: relative;
}

.story-card::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  width: 4px;
  height: 100%;
  background: var(--color-status-info);
  transition: all 0.2s ease;
}

.story-card[data-severity="critical"]::before {
  background: var(--color-status-error);
}

.story-card[data-severity="high"]::before {
  background: var(--color-status-warning);
}

.story-card[data-severity="medium"]::before {
  background: var(--color-status-info);
}

.story-card[data-severity="low"]::before {
  background: var(--color-status-success);
}

.story-card:hover {
  border-color: var(--color-border-secondary);
  box-shadow: var(--shadow-md);
}

.story-card.expanded {
  border-color: var(--color-border-accent);
  box-shadow: var(--shadow-lg);
}

.story-header {
  padding: var(--space-lg);
  cursor: pointer;
  transition: all 0.2s ease;
  border-left: 4px solid transparent;
}

.story-header:hover {
  background: var(--color-surface-primary);
}

.story-meta {
  display: flex;
  align-items: center;
  gap: var(--space-md);
  margin-bottom: var(--space-md);
  flex-wrap: wrap;
}

.severity-badge, .category-badge {
  display: inline-flex;
  align-items: center;
  gap: var(--space-xs);
  padding: var(--space-xs) var(--space-sm);
  border-radius: var(--radius-sm);
  font-size: 0.75rem;
  font-weight: 500;
  border: 1px solid var(--color-border-primary);
  transition: all 0.2s ease;
}

.severity-badge {
  background: var(--color-surface-secondary);
  color: var(--color-text-primary);
}

.severity-badge[data-severity="critical"] {
  background: rgba(255, 51, 51, 0.1);
  border-color: var(--color-status-error);
  color: var(--color-status-error);
}

.severity-badge[data-severity="high"] {
  background: rgba(255, 184, 0, 0.1);
  border-color: var(--color-status-warning);
  color: var(--color-status-warning);
}

.severity-badge[data-severity="medium"] {
  background: rgba(0, 153, 255, 0.1);
  border-color: var(--color-status-info);
  color: var(--color-status-info);
}

.severity-badge[data-severity="low"] {
  background: rgba(0, 240, 120, 0.1);
  border-color: var(--color-status-success);
  color: var(--color-status-success);
}

.severity-badge {
  color: white;
}

.category-badge {
  background: var(--color-surface-primary);
  color: var(--color-text-tertiary);
}

.timestamp {
  color: #94a3b8;
  font-size: 0.8rem;
  margin-left: auto;
}

.story-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 0.75rem;
}

.story-title h3 {
  margin: 0;
  color: #ffffff;
  font-size: 1.3rem;
  font-weight: 700;
  text-shadow: 0 2px 10px rgba(0, 0, 0, 0.3);
  line-height: 1.4;
}

.expand-btn {
  background: linear-gradient(135deg, rgba(15, 52, 96, 0.6) 0%, rgba(30, 64, 175, 0.6) 100%);
  backdrop-filter: blur(10px);
  border: 1px solid rgba(74, 222, 128, 0.3);
  border-radius: 8px;
  color: rgba(148, 163, 184, 0.9);
  font-size: 1rem;
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  padding: 0.5rem;
  width: 36px;
  height: 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.2);
}

.expand-btn:hover {
  color: #4ade80;
  background: linear-gradient(135deg, rgba(74, 222, 128, 0.2) 0%, rgba(34, 211, 238, 0.2) 100%);
  border-color: rgba(74, 222, 128, 0.5);
  transform: scale(1.1);
  box-shadow: 
    0 4px 15px rgba(74, 222, 128, 0.3),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
}

.expand-btn.expanded {
  transform: rotate(90deg) scale(1.05);
  color: #4ade80;
  background: linear-gradient(135deg, rgba(74, 222, 128, 0.3) 0%, rgba(34, 211, 238, 0.3) 100%);
  border-color: rgba(74, 222, 128, 0.6);
  box-shadow: 
    0 4px 15px rgba(74, 222, 128, 0.4),
    inset 0 1px 0 rgba(255, 255, 255, 0.15);
}

.story-description {
  margin: 0;
  color: rgba(255, 255, 255, 0.8);
  line-height: 1.6;
  font-size: 1rem;
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.3);
}

.story-content {
  border-top: 1px solid rgba(74, 222, 128, 0.3);
  padding: 2rem;
  background: linear-gradient(135deg, rgba(10, 20, 40, 0.7) 0%, rgba(15, 52, 96, 0.5) 100%);
  backdrop-filter: blur(10px);
  position: relative;
  animation: contentSlideDown 0.4s ease-out;
}

@keyframes contentSlideDown {
  from {
    opacity: 0;
    transform: translateY(-20px);
    max-height: 0;
  }
  to {
    opacity: 1;
    transform: translateY(0);
    max-height: 1000px;
  }
}

.content-section {
  margin-bottom: 1.5rem;
}

.content-section:last-child {
  margin-bottom: 0;
}

.section-title {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin: 0 0 1rem 0;
  font-size: 1.1rem;
  font-weight: 600;
  background: linear-gradient(135deg, #4ade80 0%, #22d3ee 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  text-shadow: 0 0 10px rgba(74, 222, 128, 0.3);
}

.section-icon {
  font-size: 1rem;
}

.root-cause, .prediction {
  margin: 0;
  color: #ffffff;
  line-height: 1.6;
  background: linear-gradient(135deg, rgba(26, 26, 46, 0.8) 0%, rgba(15, 52, 96, 0.6) 100%);
  backdrop-filter: blur(10px);
  padding: 1.25rem;
  border-radius: 12px;
  border-left: 4px solid transparent;
  border-image: linear-gradient(45deg, #4ade80, #22d3ee) 1;
  box-shadow: 
    0 4px 15px rgba(0, 0, 0, 0.2),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
  font-size: 1rem;
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.3);
}

/* Hubble-style Resource Topology */
.resources-topology {
  display: flex;
  flex-direction: column;
  gap: var(--space-md);
  background: var(--color-surface-primary);
  border-radius: var(--radius-md);
  padding: var(--space-lg);
  border: 1px solid var(--color-border-primary);
}

.resource-node {
  position: relative;
  background: var(--color-bg-secondary);
  border: 1px solid var(--color-border-primary);
  border-radius: var(--radius-md);
  padding: var(--space-md);
  transition: all 0.2s ease;
}

.resource-node:hover {
  border-color: var(--color-border-accent);
  box-shadow: var(--shadow-glow);
}

.resource-node.resource-pod {
  border-left: 3px solid var(--color-status-info);
}

.resource-node.resource-service {
  border-left: 3px solid var(--color-status-success);
}

.resource-node.resource-deployment {
  border-left: 3px solid var(--color-status-warning);
}

.resource-node.resource-node {
  border-left: 3px solid var(--color-accent-primary);
}

.resource-header {
  display: flex;
  align-items: center;
  gap: var(--space-sm);
  margin-bottom: var(--space-sm);
}

.resource-type-badge {
  background: var(--color-surface-secondary);
  color: var(--color-text-secondary);
  padding: var(--space-xs) var(--space-sm);
  border-radius: var(--radius-sm);
  font-size: 0.75rem;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  font-family: var(--font-mono);
}

.resource-namespace {
  color: var(--color-text-tertiary);
  font-size: 0.75rem;
  font-family: var(--font-mono);
}

.resource-name {
  color: var(--color-text-primary);
  font-weight: 600;
  font-size: 0.875rem;
  margin-bottom: var(--space-sm);
  font-family: var(--font-mono);
}

.resource-status {
  display: flex;
  align-items: center;
  gap: var(--space-xs);
}

.status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--color-status-error);
}

.status-dot.affected {
  background: var(--color-status-error);
  box-shadow: 0 0 6px rgba(255, 51, 51, 0.4);
}

.status-label {
  font-size: 0.75rem;
  color: var(--color-text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.resource-connection {
  position: absolute;
  bottom: -8px;
  left: 50%;
  transform: translateX(-50%);
  width: 2px;
  height: 16px;
  background: linear-gradient(180deg, var(--color-border-secondary) 0%, transparent 100%);
}

.actions-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-lg);
}

.action-item {
  background: var(--color-bg-secondary);
  border: 1px solid var(--color-border-primary);
  border-radius: var(--radius-md);
  padding: var(--space-lg);
  transition: all 0.2s ease;
}

.action-item:hover {
  border-color: var(--color-border-secondary);
  box-shadow: var(--shadow-md);
}

.action-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: var(--space-lg);
  margin-bottom: var(--space-md);
}

.action-info {
  flex: 1;
}

.action-title {
  color: var(--color-text-primary);
  font-weight: 600;
  margin-bottom: var(--space-xs);
  font-size: 0.875rem;
}

.action-description {
  color: var(--color-text-secondary);
  font-size: 0.8rem;
  line-height: 1.4;
}

.action-meta {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.risk-badge {
  padding: var(--space-xs) var(--space-sm);
  border-radius: var(--radius-sm);
  font-size: 0.75rem;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  border: 1px solid;
}

.risk-badge[data-risk="low"] {
  background: rgba(0, 240, 120, 0.1);
  color: var(--color-status-success);
  border-color: var(--color-status-success);
}

.risk-badge[data-risk="medium"] {
  background: rgba(255, 184, 0, 0.1);
  color: var(--color-status-warning);
  border-color: var(--color-status-warning);
}

.risk-badge[data-risk="high"] {
  background: rgba(255, 51, 51, 0.1);
  color: var(--color-status-error);
  border-color: var(--color-status-error);
}

.apply-btn {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem 1.5rem;
  border: 1px solid rgba(255, 255, 255, 0.2);
  border-radius: 12px;
  cursor: pointer;
  font-size: 0.9rem;
  font-weight: 600;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  background: linear-gradient(135deg, rgba(30, 64, 175, 0.8) 0%, rgba(37, 99, 235, 0.8) 100%);
  backdrop-filter: blur(10px);
  color: white;
  box-shadow: 
    0 4px 15px rgba(30, 64, 175, 0.3),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
  position: relative;
  overflow: hidden;
}

.apply-btn::before {
  content: '';
  position: absolute;
  top: 0;
  left: -100%;
  width: 100%;
  height: 100%;
  background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.2), transparent);
  transition: left 0.6s;
}

.apply-btn:hover::before {
  left: 100%;
}

.apply-btn:hover:not(:disabled) {
  background: linear-gradient(135deg, rgba(37, 99, 235, 0.9) 0%, rgba(59, 130, 246, 0.9) 100%);
  transform: translateY(-3px) scale(1.05);
  box-shadow: 
    0 8px 25px rgba(30, 64, 175, 0.4),
    0 0 20px rgba(74, 222, 128, 0.2),
    inset 0 1px 0 rgba(255, 255, 255, 0.2);
  border-color: rgba(74, 222, 128, 0.3);
}

.apply-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.apply-btn.high {
  background: linear-gradient(135deg, rgba(220, 38, 38, 0.8) 0%, rgba(239, 68, 68, 0.8) 100%);
  box-shadow: 
    0 4px 15px rgba(220, 38, 38, 0.3),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
}

.apply-btn.high:hover:not(:disabled) {
  background: linear-gradient(135deg, rgba(239, 68, 68, 0.9) 0%, rgba(248, 113, 113, 0.9) 100%);
  box-shadow: 
    0 8px 25px rgba(220, 38, 38, 0.4),
    0 0 20px rgba(239, 68, 68, 0.3),
    inset 0 1px 0 rgba(255, 255, 255, 0.2);
}

.apply-btn span {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.commands-section {
  margin-top: 0.75rem;
  padding-top: 0.75rem;
  border-top: 1px solid #374151;
}

.commands-header {
  color: #94a3b8;
  font-size: 0.85rem;
  margin-bottom: 0.5rem;
}

.command-item {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
}

.command-text {
  flex: 1;
  background: linear-gradient(135deg, rgba(15, 23, 42, 0.9) 0%, rgba(30, 64, 175, 0.1) 100%);
  backdrop-filter: blur(5px);
  color: #4ade80;
  padding: 0.75rem 1rem;
  border-radius: 8px;
  font-family: 'SF Mono', 'Monaco', 'Menlo', 'Ubuntu Mono', monospace;
  font-size: 0.85rem;
  border: 1px solid rgba(74, 222, 128, 0.3);
  overflow-x: auto;
  box-shadow: 
    0 2px 10px rgba(0, 0, 0, 0.2),
    inset 0 1px 0 rgba(255, 255, 255, 0.05);
  text-shadow: 0 0 10px rgba(74, 222, 128, 0.3);
}

.copy-btn {
  background: linear-gradient(135deg, rgba(55, 65, 81, 0.8) 0%, rgba(75, 85, 99, 0.8) 100%);
  backdrop-filter: blur(5px);
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: rgba(148, 163, 184, 0.9);
  padding: 0.6rem;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.3s ease;
  font-size: 0.8rem;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.2);
}

.copy-btn:hover {
  background: linear-gradient(135deg, rgba(75, 85, 99, 0.9) 0%, rgba(107, 114, 128, 0.9) 100%);
  color: #ffffff;
  transform: translateY(-2px) scale(1.05);
  box-shadow: 
    0 4px 15px rgba(0, 0, 0, 0.3),
    0 0 10px rgba(74, 222, 128, 0.2);
  border-color: rgba(74, 222, 128, 0.3);
}
</style>