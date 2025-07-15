<script lang="ts" setup>
import { ref, computed } from 'vue'
import StoryCard from './StoryCard.vue'

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
  stories: Story[]
}

interface Emits {
  (event: 'apply-fix', storyId: string, actionId: string): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const selectedSeverity = ref<string>('all')
const severityOptions = ['all', 'critical', 'high', 'medium', 'low']

const filteredStories = computed(() => {
  if (selectedSeverity.value === 'all') {
    return props.stories
  }
  return props.stories.filter(story => story.severity === selectedSeverity.value)
})

const getSeverityIcon = (severity: string) => {
  switch (severity) {
    case 'critical': return '🚨'
    case 'high': return '⚠️'
    case 'medium': return '🟡'
    case 'low': return '🔵'
    default: return '❓'
  }
}

const getSeverityCount = (severity: string) => {
  if (severity === 'all') return props.stories.length
  return props.stories.filter(story => story.severity === severity).length
}

const handleApplyFix = (storyId: string, actionId: string) => {
  emit('apply-fix', storyId, actionId)
}
</script>

<template>
  <div class="story-list">
    <!-- Filter Bar -->
    <div class="filter-bar">
      <div class="filter-options">
        <button
          v-for="severity in severityOptions"
          :key="severity"
          @click="selectedSeverity = severity"
          :class="[
            'filter-btn',
            { active: selectedSeverity === severity }
          ]"
        >
          <span class="filter-icon">{{ getSeverityIcon(severity) }}</span>
          <span class="filter-label">{{ severity === 'all' ? 'All' : severity.charAt(0).toUpperCase() + severity.slice(1) }}</span>
          <span class="filter-count">{{ getSeverityCount(severity) }}</span>
        </button>
      </div>
    </div>

    <!-- Stories Container -->
    <div class="stories-container">
      <div v-if="filteredStories.length === 0" class="empty-state">
        <div class="empty-icon">📋</div>
        <h3>No stories found</h3>
        <p v-if="selectedSeverity === 'all'">
          Your cluster is healthy! No issues detected.
        </p>
        <p v-else>
          No {{ selectedSeverity }} severity stories found.
        </p>
      </div>
      
      <div v-else class="stories-grid">
        <StoryCard
          v-for="story in filteredStories"
          :key="story.id"
          :story="story"
          @apply-fix="handleApplyFix"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.story-list {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.filter-bar {
  padding: 1.5rem 2rem;
  border-bottom: 1px solid rgba(74, 222, 128, 0.2);
  flex-shrink: 0;
  background: linear-gradient(135deg, rgba(15, 52, 96, 0.3) 0%, rgba(22, 33, 62, 0.3) 100%);
}

.filter-options {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
}

.filter-btn {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.75rem 1rem;
  background: linear-gradient(135deg, rgba(15, 52, 96, 0.5) 0%, rgba(30, 64, 175, 0.5) 100%);
  backdrop-filter: blur(10px);
  border: 1px solid rgba(74, 222, 128, 0.2);
  border-radius: 12px;
  color: rgba(148, 163, 184, 0.9);
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  font-size: 0.85rem;
  font-weight: 500;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  position: relative;
  overflow: hidden;
}

.filter-btn::before {
  content: '';
  position: absolute;
  top: 0;
  left: -100%;
  width: 100%;
  height: 100%;
  background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.1), transparent);
  transition: left 0.5s;
}

.filter-btn:hover::before {
  left: 100%;
}

.filter-btn:hover {
  background: linear-gradient(135deg, rgba(30, 64, 175, 0.7) 0%, rgba(37, 99, 235, 0.7) 100%);
  color: #ffffff;
  transform: translateY(-2px);
  box-shadow: 
    0 4px 15px rgba(0, 0, 0, 0.2),
    0 0 10px rgba(74, 222, 128, 0.2);
}

.filter-btn.active {
  background: linear-gradient(135deg, rgba(74, 222, 128, 0.8) 0%, rgba(34, 211, 238, 0.8) 100%);
  color: #ffffff;
  border-color: rgba(74, 222, 128, 0.6);
  box-shadow: 
    0 4px 20px rgba(74, 222, 128, 0.3),
    inset 0 1px 0 rgba(255, 255, 255, 0.2);
  transform: translateY(-1px);
}

.filter-icon {
  font-size: 0.9rem;
}

.filter-label {
  font-weight: 500;
}

.filter-count {
  background: linear-gradient(135deg, rgba(55, 65, 81, 0.8) 0%, rgba(75, 85, 99, 0.8) 100%);
  backdrop-filter: blur(5px);
  color: rgba(255, 255, 255, 0.9);
  padding: 0.25rem 0.6rem;
  border-radius: 12px;
  font-size: 0.7rem;
  font-weight: 600;
  border: 1px solid rgba(255, 255, 255, 0.1);
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.2);
  transition: all 0.3s ease;
}

.filter-btn.active .filter-count {
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.9) 0%, rgba(255, 255, 255, 0.8) 100%);
  color: #1a1a2e;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.15);
}

.stories-container {
  flex: 1;
  overflow-y: auto;
  padding: 1.5rem;
  animation: storiesSlideIn 0.4s ease-out;
}

@keyframes storiesSlideIn {
  from {
    opacity: 0;
    transform: translateY(20px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 300px;
  color: rgba(148, 163, 184, 0.8);
  text-align: center;
  animation: emptyStateFadeIn 0.6s ease-out;
}

@keyframes emptyStateFadeIn {
  from {
    opacity: 0;
    transform: scale(0.9);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}

.empty-icon {
  font-size: 4rem;
  margin-bottom: 1.5rem;
  opacity: 0.7;
  animation: emptyIconFloat 3s ease-in-out infinite;
}

@keyframes emptyIconFloat {
  0%, 100% { transform: translateY(0); }
  50% { transform: translateY(-10px); }
}

.empty-state h3 {
  margin-bottom: 0.75rem;
  color: #ffffff;
  font-size: 1.25rem;
  font-weight: 600;
}

.empty-state p {
  margin: 0;
  font-size: 0.9rem;
}

.stories-grid {
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
  animation: storiesGridFadeIn 0.5s ease-out;
}

@keyframes storiesGridFadeIn {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

/* Scrollbar styling */
.stories-container::-webkit-scrollbar {
  width: 6px;
}

.stories-container::-webkit-scrollbar-track {
  background: #0f3460;
}

.stories-container::-webkit-scrollbar-thumb {
  background: #1e40af;
  border-radius: 3px;
}

.stories-container::-webkit-scrollbar-thumb:hover {
  background: #2563eb;
}
</style>