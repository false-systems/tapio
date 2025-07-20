<script lang="ts" setup>
import UltraModernDashboard from './components/UltraModernDashboard.vue'
</script>

<template>
  <div id="app">
    <UltraModernDashboard/>
  </div>
</template>

<style>
@import url('https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap');

:root {
  /* Clean Enterprise Color System */
  --gray-50: #fafafa;
  --gray-100: #f4f4f5;
  --gray-200: #e4e4e7;
  --gray-300: #d4d4d8;
  --gray-400: #a1a1aa;
  --gray-500: #71717a;
  --gray-600: #52525b;
  --gray-700: #3f3f46;
  --gray-800: #27272a;
  --gray-900: #18181b;
  --gray-950: #09090b;
  
  --red-500: #ef4444;
  --red-600: #dc2626;
  --amber-500: #f59e0b;
  --amber-600: #d97706;
  --green-500: #22c55e;
  --green-600: #16a34a;
  --blue-500: #3b82f6;
  --blue-600: #2563eb;
  
  /* Minimal Shadows */
  --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.05);
  --shadow-md: 0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
  --shadow-lg: 0 10px 15px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -2px rgba(0, 0, 0, 0.05);
  
  /* Spacing */
  --space-xs: 0.25rem;
  --space-sm: 0.5rem;
  --space-md: 1rem;
  --space-lg: 1.5rem;
  --space-xl: 2rem;
  --space-2xl: 3rem;
  
  /* Clean Border Radius */
  --radius-sm: 0.375rem;
  --radius-md: 0.5rem;
  --radius-lg: 0.75rem;
  
  /* Clean Typography */
  --font-sans: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
  --font-mono: ui-monospace, SFMono-Regular, 'SF Mono', Consolas, monospace;
}

* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

body {
  font-family: var(--font-sans);
  background: #fafafa;
  color: var(--gray-900);
  overflow: hidden;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  font-size: 14px;
  line-height: 1.5;
}

#app {
  width: 100vw;
  height: 100vh;
  overflow: hidden;
  position: relative;
  background: transparent;
}

/* Professional Scrollbar */
::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}

::-webkit-scrollbar-track {
  background: transparent;
}

::-webkit-scrollbar-thumb {
  background: var(--gray-300);
  border-radius: 4px;
}

::-webkit-scrollbar-thumb:hover {
  background: var(--gray-400);
}

::selection {
  background: var(--blue-500);
  color: white;
}

button:focus-visible,
input:focus-visible,
[tabindex]:focus-visible {
  outline: 2px solid var(--blue-500);
  outline-offset: 2px;
}

/* Clean Typography */
.text-xs { font-size: 0.75rem; line-height: 1rem; }
.text-sm { font-size: 0.875rem; line-height: 1.25rem; }
.text-base { font-size: 1rem; line-height: 1.5rem; }
.text-lg { font-size: 1.125rem; line-height: 1.75rem; }
.text-xl { font-size: 1.25rem; line-height: 1.75rem; }
.text-2xl { font-size: 1.5rem; line-height: 2rem; }

.font-normal { font-weight: 400; }
.font-medium { font-weight: 500; }
.font-semibold { font-weight: 600; }
.font-bold { font-weight: 700; }

.text-gray-400 { color: var(--gray-400); }
.text-gray-500 { color: var(--gray-500); }
.text-gray-600 { color: var(--gray-600); }
.text-gray-700 { color: var(--gray-700); }
.text-gray-900 { color: var(--gray-900); }

/* Minimal utilities */
* { transition: none; }

.transition-colors {
  transition-property: color, background-color, border-color;
  transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1);
  transition-duration: 150ms;
}
</style>
