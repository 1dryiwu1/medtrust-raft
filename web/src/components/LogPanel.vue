<template>
  <div class="log-panel">
    <div class="filter-row">
      <button
        v-for="filter in filters"
        :key="filter.key"
        :class="{ active: activeFilter === filter.key }"
        @click="activeFilter = activeFilter === filter.key ? '' : filter.key"
      >
        {{ filter.label }}
      </button>
    </div>

    <div class="log-list">
      <div v-for="(log, index) in filteredLogs" :key="`${log.ts}-${index}`" class="log-item" :class="log.type">
        <i></i>
        <p>{{ log.msg }}</p>
        <time>{{ log.ts }}</time>
      </div>
      <div v-if="filteredLogs.length === 0" class="empty-log">
        暂无事件，等待节点状态或新区块推送。
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'

const props = defineProps({
  logs: { type: Array, default: () => [] }
})

const filters = [
  { key: 'info', label: '信息' },
  { key: 'ok', label: '成功' },
  { key: 'warn', label: '告警' },
  { key: 'err', label: '错误' },
]

const activeFilter = ref('')

const filteredLogs = computed(() => {
  if (!activeFilter.value) return props.logs
  return props.logs.filter(log => log.type === activeFilter.value)
})
</script>

<style scoped>
.log-panel {
  display: grid;
  gap: 10px;
  min-height: 0;
}

.filter-row {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 6px;
}

.filter-row button {
  height: 28px;
  border: 1px solid rgba(47, 138, 184, .28);
  border-radius: 2px;
  color: rgba(203, 230, 255, .62);
  background: rgba(4, 17, 36, .64);
  font-size: 11px;
  cursor: pointer;
}

.filter-row button.active,
.filter-row button:hover {
  border-color: rgba(255, 160, 47, .66);
  color: var(--yellow);
}

.log-list {
  display: grid;
  align-content: start;
  gap: 8px;
  max-height: 290px;
  overflow: auto;
  padding-right: 4px;
}

.log-item {
  display: grid;
  grid-template-columns: 10px 1fr auto;
  gap: 8px;
  align-items: start;
  border: 1px solid rgba(47, 138, 184, .2);
  border-radius: 2px;
  padding: 8px 10px;
  background: rgba(4, 17, 36, .58);
}

.log-item i {
  width: 7px;
  height: 7px;
  margin-top: 6px;
  border-radius: 50%;
  background: var(--cyan);
  box-shadow: 0 0 10px rgba(45, 168, 255, .6);
}

.log-item.ok i {
  background: var(--mint);
  box-shadow: 0 0 10px rgba(0, 245, 255, .6);
}

.log-item.warn i {
  background: #ffd166;
  box-shadow: 0 0 10px rgba(255, 209, 102, .55);
}

.log-item.err i {
  background: #ff5b75;
  box-shadow: 0 0 10px rgba(255, 91, 117, .55);
}

.log-item p {
  color: rgba(223, 248, 255, .76);
  font-size: 12px;
  line-height: 1.45;
}

.log-item time {
  color: rgba(203, 230, 255, .42);
  font-family: Consolas, monospace;
  font-size: 10px;
  white-space: nowrap;
}

.empty-log {
  display: grid;
  place-items: center;
  min-height: 120px;
  border: 1px dashed rgba(69, 211, 255, .16);
  border-radius: 3px;
  color: rgba(203, 230, 255, .46);
  font-size: 12px;
  text-align: center;
}
</style>
