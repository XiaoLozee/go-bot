<script setup lang="ts">
type SummaryAccent = 'runtime' | 'connection' | 'plugin' | 'webui' | 'danger'

const props = defineProps<{
  summaryCards: Array<{
    accent: SummaryAccent
    label: string
    value: string
    note: string
  }>
}>()
</script>

<template>
  <section class="summary-grid workspace-summary-grid">
    <article v-for="card in summaryCards" :key="card.label" class="card stat-card" :class="`summary-accent-${card.accent}`">
      <span class="stat-label">{{ card.label }}</span>
      <strong class="stat-value">{{ card.value }}</strong>
      <p class="stat-note">{{ card.note }}</p>
    </article>
  </section>
</template>

<style scoped>
.summary-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
}

.workspace-summary-grid {
  margin-top: 2px;
}

.stat-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.stat-label {
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--accent);
}

.stat-value {
  font-size: 32px;
  font-weight: 700;
}

.summary-accent-runtime {
  border-color: rgba(56, 189, 248, 0.22);
}

.summary-accent-connection {
  border-color: rgba(96, 165, 250, 0.22);
}

.summary-accent-plugin {
  border-color: rgba(236, 72, 153, 0.22);
}

.summary-accent-webui {
  border-color: rgba(129, 140, 248, 0.22);
}

.summary-accent-danger {
  border-color: rgba(239, 68, 68, 0.22);
}
</style>
