<template>
  <div class="particle-background">
    <div 
      v-for="particle in particles" 
      :key="particle.id"
      class="particle"
      :style="particle.style"
    ></div>
  </div>
</template>

<script lang="ts" setup>
import { ref, onMounted, onUnmounted } from 'vue'

interface Particle {
  id: number
  x: number
  y: number
  size: number
  speed: number
  opacity: number
  style: {
    left: string
    top: string
    width: string
    height: string
    opacity: string
    animationDuration: string
    animationDelay: string
  }
}

const particles = ref<Particle[]>([])
const animationFrame = ref<number>()

const createParticle = (id: number): Particle => {
  const x = Math.random() * window.innerWidth
  const y = Math.random() * window.innerHeight
  const size = Math.random() * 3 + 1
  const speed = Math.random() * 2 + 0.5
  const opacity = Math.random() * 0.5 + 0.1

  return {
    id,
    x,
    y,
    size,
    speed,
    opacity,
    style: {
      left: `${x}px`,
      top: `${y}px`,
      width: `${size}px`,
      height: `${size}px`,
      opacity: `${opacity}`,
      animationDuration: `${20 + Math.random() * 20}s`,
      animationDelay: `${Math.random() * 5}s`
    }
  }
}

const updateParticles = () => {
  particles.value.forEach(particle => {
    particle.y -= particle.speed
    particle.opacity = Math.max(0, particle.opacity - 0.001)
    
    // Reset particle when it goes off screen or becomes invisible
    if (particle.y < -10 || particle.opacity <= 0) {
      particle.y = window.innerHeight + 10
      particle.x = Math.random() * window.innerWidth
      particle.opacity = Math.random() * 0.5 + 0.1
    }
    
    particle.style.left = `${particle.x}px`
    particle.style.top = `${particle.y}px`
    particle.style.opacity = `${particle.opacity}`
  })
  
  animationFrame.value = requestAnimationFrame(updateParticles)
}

onMounted(() => {
  // Create initial particles
  for (let i = 0; i < 50; i++) {
    particles.value.push(createParticle(i))
  }
  
  // Start animation
  updateParticles()
})

onUnmounted(() => {
  if (animationFrame.value) {
    cancelAnimationFrame(animationFrame.value)
  }
})
</script>

<style scoped>
.particle-background {
  position: fixed;
  top: 0;
  left: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
  z-index: -1;
  overflow: hidden;
}

.particle {
  position: absolute;
  background: radial-gradient(circle, rgba(74, 222, 128, 0.8) 0%, rgba(34, 211, 238, 0.4) 50%, transparent 100%);
  border-radius: 50%;
  animation: float linear infinite, twinkle ease-in-out infinite alternate;
  filter: blur(0.5px);
}

@keyframes float {
  from {
    transform: translateY(0) translateX(0);
  }
  to {
    transform: translateY(-100vh) translateX(10px);
  }
}

@keyframes twinkle {
  0% {
    opacity: 0.2;
    transform: scale(1);
  }
  100% {
    opacity: 0.8;
    transform: scale(1.2);
  }
}

/* Add some larger, slower moving particles for depth */
.particle:nth-child(5n) {
  background: radial-gradient(circle, rgba(168, 85, 247, 0.6) 0%, rgba(59, 130, 246, 0.3) 50%, transparent 100%);
  animation-duration: 35s, 4s;
}

.particle:nth-child(7n) {
  background: radial-gradient(circle, rgba(34, 211, 238, 0.7) 0%, rgba(74, 222, 128, 0.4) 50%, transparent 100%);
  animation-duration: 25s, 3s;
}

.particle:nth-child(11n) {
  background: radial-gradient(circle, rgba(251, 191, 36, 0.5) 0%, rgba(245, 158, 11, 0.3) 50%, transparent 100%);
  animation-duration: 40s, 5s;
}
</style>