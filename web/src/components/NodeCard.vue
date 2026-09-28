<template>
  <article class="node-card" :class="cardClass">
    <div class="node-top">
      <div>
        <strong>{{ displayName }}</strong>
        <span>{{ node.api }}</span>
      </div>
      <em :class="badgeClass">{{ badgeText }}</em>
    </div>

    <div class="node-bars">
      <span :style="{ width: syncPercent + '%' }"></span>
    </div>

    <div class="node-meta">
      <p>
        <span>任期</span>
        <b>Term {{ status?.term ?? '--' }}</b>
      </p>
      <p>
        <span>提交/应用</span>
        <b>{{ status?.commit_index ?? '--' }}/{{ status?.last_applied ?? '--' }}</b>
      </p>
      <p>
        <span>日志末尾</span>
        <b>{{ status?.last_log_index ?? '--' }}</b>
      </p>
      <p>
        <span>心跳/状态</span>
        <b>{{ faultText }}</b>
      </p>
    </div>
  </article>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  node: { type: Object, required: true },
  status: { type: Object, default: () => ({}) }
})

const displayName = computed(() => {
  const names = {
    'node-1': 'Node 1 中心医院',
    'node-2': 'Node 2 三甲医院',
    'node-3': 'Node 3 社区医院',
  }
  return names[props.node.id] || props.node.id
})

const cardClass = computed(() => ({
  leader: props.status?.online && props.status?.is_leader,
  offline: !props.status?.online,
}))

const badgeClass = computed(() => {
  if (!props.status?.online) return 'offline'
  if (props.status?.is_leader) return 'leader'
  return 'follower'
})

const badgeText = computed(() => {
  if (!props.status?.online) return '离线'
  if (props.status?.is_leader) return 'Leader'
  return props.status?.role || 'Follower'
})

const syncPercent = computed(() => {
  if (!props.status?.online) return 8
  const commit = Number(props.status?.commit_index || 0)
  const blocks = Number(props.status?.block_count || 0)
  return Math.max(28, Math.min(100, (commit || blocks || 1) * 12))
})

const latencyText = computed(() => {
  if (!props.status?.online) return '--'
  if (!Number.isFinite(props.status?.latency)) return '采样中'
  return `${Math.round(props.status.latency)}ms`
})

const faultText = computed(() => {
  if (!props.status?.online) return '离线'
  if (props.status?.is_leader) return `稳定 ${formatDuration(props.status?.role_duration_ms)}`
  if (props.status?.fault_state === 'heartbeat_overdue') return '心跳超时'
  if (props.status?.fault_state === 'leader_unknown') return '等待Leader'
  if (props.status?.fault_state === 'election_in_progress') return '选举中'
  const age = props.status?.last_heartbeat_age_ms
  if (Number.isFinite(age) && age >= 0) return `${Math.round(age)}ms`
  return latencyText.value
})

function formatDuration(ms) {
  if (!Number.isFinite(Number(ms)) || Number(ms) <= 0) return '刚刚'
  const seconds = Math.floor(Number(ms) / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  return `${Math.floor(minutes / 60)}h`
}
</script>

<style scoped>
.node-card {
  position: relative;
  overflow: hidden;
  border: 1px solid rgba(47, 138, 184, 0.3);
  border-radius: 3px;
  padding: 12px;
  background:
    linear-gradient(rgba(31, 184, 255, .026) 1px, transparent 1px) 0 0 / 100% 12px,
    linear-gradient(135deg, rgba(12, 33, 58, 0.82), rgba(5, 12, 24, 0.92)),
    rgba(5, 12, 24, 0.92);
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.018);
}

.node-card::after {
  content: "";
  position: absolute;
  inset: 0;
  border-radius: inherit;
  pointer-events: none;
  background: linear-gradient(90deg, transparent, rgba(255, 160, 47, 0.18), transparent);
  transform: translateX(-120%);
  animation: nodeSweep 4.8s ease-in-out infinite;
}

.node-card.leader {
  border-color: rgba(40, 231, 208, 0.7);
  box-shadow: inset 0 0 18px rgba(40, 231, 208, 0.08);
}

.node-card.offline {
  border-color: rgba(255, 91, 117, 0.24);
  filter: grayscale(.45);
  opacity: .58;
}

.node-top {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: flex-start;
}

.node-top strong {
  display: block;
  color: #f3fbff;
  font-size: 13px;
}

.node-top span {
  display: block;
  margin-top: 2px;
  color: rgba(203, 230, 255, 0.52);
  font-size: 11px;
  font-family: Consolas, monospace;
}

.node-top em {
  flex: 0 0 auto;
  min-width: 64px;
  border-radius: 2px;
  padding: 4px 8px;
  text-align: center;
  font-size: 11px;
  font-style: normal;
  font-weight: 800;
}

.node-top em.leader {
  color: #050a14;
  background: var(--yellow);
}

.node-top em.follower {
  color: #dff8ff;
  background: rgba(45, 168, 255, 0.24);
  border: 1px solid rgba(45, 168, 255, 0.32);
}

.node-top em.offline {
  color: #ffc0ca;
  background: rgba(255, 91, 117, 0.14);
  border: 1px solid rgba(255, 91, 117, 0.24);
}

.node-bars {
  height: 6px;
  margin: 14px 0 12px;
  border-radius: 1px;
  background: rgba(126, 188, 255, 0.09);
  overflow: hidden;
}

.node-bars span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, var(--yellow), var(--mint));
  transition: width .4s ease;
}

.node-meta {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 8px 12px;
}

.node-meta p {
  min-width: 0;
}

.node-meta span {
  display: block;
  color: rgba(203, 230, 255, 0.5);
  font-size: 11px;
}

.node-meta b {
  display: block;
  color: #f3fbff;
  font-family: Consolas, monospace;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
}

@keyframes nodeSweep {
  0%, 55% { transform: translateX(-120%); }
  78%, 100% { transform: translateX(120%); }
}
</style>
