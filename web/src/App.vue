<template>
  <div class="dashboard">
    <div class="screen-bg"></div>
    <div class="screen-stars"></div>
    <div class="screen-scan"></div>

    <header class="topbar">
      <div class="topbar-side">
        <span class="corner-line"></span>
        <span class="system-tag">MEDTRUST RAFT</span>
      </div>
      <div class="title-wrap">
        <div class="title-kicker">医疗数据联盟链运行驾驶舱</div>
        <h1>MedTrust 联盟链可信医疗数据平台</h1>
      </div>
      <div class="topbar-side right">
        <span class="clock">{{ nowText }}</span>
        <span class="status-pill" :class="clusterStatus">
          <i></i>{{ clusterStatusText }}
        </span>
      </div>
    </header>

    <section class="kpi-strip" aria-label="关键指标">
      <div v-for="item in kpiItems" :key="item.label" class="kpi-card">
        <div class="kpi-label">{{ item.label }}</div>
        <div class="kpi-value">
          {{ item.value }}<span v-if="item.unit">{{ item.unit }}</span>
        </div>
        <div class="kpi-sub">{{ item.sub }}</div>
      </div>
    </section>

    <main class="screen-grid">
      <aside class="left-column">
        <section class="panel cluster-panel">
          <PanelTitle title="节点运行态势" accent="Raft Cluster" />
          <div class="cluster-summary">
            <div class="health-ring" :style="{ '--score': healthScore + '%' }">
              <span>{{ healthScore }}</span>
              <small>健康度</small>
            </div>
            <div class="summary-lines">
              <p><b>{{ onlineCount }}</b>/{{ nodes.length }} 节点在线</p>
              <p>多数派阈值 <b>{{ quorumText }}</b></p>
              <p>Leader <b>{{ leaderName }}</b></p>
              <p>故障转移 <b>{{ clusterFaultText }}</b></p>
            </div>
          </div>
          <div class="node-stack">
            <NodeCard
              v-for="node in nodes"
              :key="node.id"
              :node="node"
              :status="nodeStatuses[node.id]"
            />
          </div>
        </section>

        <section class="panel upload-shell">
          <PanelTitle title="模拟数据上链" accent="Write API" />
          <UploadPanel :leader-api="leaderAPI" @upload-success="onUploadSuccess" />
        </section>
      </aside>

      <section class="center-column">
        <section class="panel consensus-panel">
          <PanelTitle title="Raft 共识链路" accent="Client -> Commit -> Block" />
          <div class="flow-stage" :class="{ active: pulseFlow }">
            <div
              v-for="(step, index) in consensusSteps"
              :key="step.title"
              class="flow-node"
              :class="{ leader: step.kind === 'leader', commit: step.kind === 'commit' }"
            >
              <div class="flow-icon">{{ index + 1 }}</div>
              <div>
                <strong>{{ step.title }}</strong>
                <span>{{ step.desc }}</span>
              </div>
            </div>
          </div>

          <div class="topology">
            <svg class="route-map" viewBox="0 0 720 300" preserveAspectRatio="none" aria-hidden="true">
              <path class="map-line muted" d="M24 76 C160 24 218 122 326 76 S516 38 692 92" />
              <path class="map-line muted" d="M44 218 C132 152 250 214 358 154 S548 146 684 224" />
              <path class="map-line" d="M72 178 C196 92 274 246 392 128 S582 82 666 158" />
              <path class="map-line glow" d="M118 126 C230 54 334 190 462 98 S604 112 680 66" />
              <circle class="map-dot primary" cx="118" cy="126" r="5" />
              <circle class="map-dot" cx="286" cy="168" r="4" />
              <circle class="map-dot warn" cx="462" cy="98" r="5" />
              <circle class="map-dot" cx="620" cy="130" r="4" />
            </svg>
            <div class="client-orbit">
              <span>Client</span>
              <small>POST /upload</small>
            </div>
            <div class="topology-core">
              <div class="core-pulse"></div>
              <strong>{{ leaderName }}</strong>
              <span>Leader</span>
            </div>
            <div class="topology-nodes">
              <div
                v-for="node in nodes"
                :key="node.id"
                class="topology-node"
                :class="nodeClass(node)"
              >
                <b>{{ nodeShortName(node.id) }}</b>
                <span>{{ nodeRole(node.id) }}</span>
              </div>
            </div>
          </div>
        </section>

        <section class="panel chain-ledger">
          <PanelTitle title="区块链账本" accent="Hash Linked Blocks" />
          <div class="ledger-head">
            <div>
              <span>最新区块</span>
              <strong>#{{ latestBlock?.index ?? '--' }}</strong>
            </div>
            <div>
              <span>链完整性</span>
              <strong :class="chainIntegrity ? 'ok-text' : 'warn-text'">
                {{ chainIntegrity ? '校验通过' : '待确认' }}
              </strong>
            </div>
            <div>
              <span>最新哈希</span>
              <code>{{ shortHash(latestBlock?.hash) }}</code>
            </div>
          </div>
          <BlockChain :blocks="blocks" />
        </section>
      </section>

      <aside class="right-column">
        <section class="panel ranking-panel">
          <PanelTitle title="节点同步排行" accent="Commit Index" />
          <div class="sync-table">
            <div class="sync-row head">
              <span>节点</span><span>角色</span><span>同步</span><span>状态</span>
            </div>
            <div v-for="row in syncRows" :key="row.id" class="sync-row">
              <span>{{ nodeShortName(row.id) }}</span>
              <span :class="row.roleClass">{{ row.role }}</span>
              <span>{{ row.height }}</span>
              <span :class="row.faultClass">{{ row.fault }}</span>
            </div>
          </div>
        </section>

        <section class="panel radar-panel">
          <PanelTitle title="可信运行评分" accent="Integrity Radar" />
          <div class="radar-wrap">
            <svg viewBox="0 0 220 220" role="img" aria-label="可信运行评分雷达图">
              <g class="radar-grid">
                <polygon v-for="r in [90, 70, 50, 30]" :key="r" :points="radarPolygon(r)" />
                <line v-for="p in radarAxes" :key="p.label" x1="110" y1="110" :x2="p.x" :y2="p.y" />
              </g>
              <polygon class="radar-area" :points="radarScorePolygon" />
              <circle v-for="p in radarScorePoints" :key="p.label" :cx="p.x" :cy="p.y" r="3" />
              <text v-for="p in radarAxes" :key="p.label + '-t'" :x="p.tx" :y="p.ty">{{ p.label }}</text>
            </svg>
          </div>
        </section>

        <section class="panel log-shell">
          <PanelTitle title="共识事件流" accent="SSE / Polling" />
          <LogPanel :logs="logs" />
        </section>
      </aside>
    </main>

    <section class="bottom-grid">
      <div class="panel chart-panel">
        <PanelTitle title="上链记录统计" accent="Records / Latency" />
        <div class="combo-chart">
          <div
            v-for="point in history"
            :key="point.label"
            class="bar-slot"
            :title="`${point.label} 记录 ${point.records} 延迟 ${point.latency}ms`"
          >
            <span class="bar planned" :style="{ height: point.barA + '%' }"></span>
            <span class="bar done" :style="{ height: point.barB + '%' }"></span>
            <small>{{ point.label }}</small>
          </div>
          <svg class="line-overlay" viewBox="0 0 100 100" preserveAspectRatio="none">
            <polyline :points="latencyLine" />
          </svg>
        </div>
      </div>

      <div class="panel trend-panel">
        <PanelTitle title="区块增长趋势" accent="Block Height" />
        <div class="trend-canvas">
          <svg viewBox="0 0 720 220" preserveAspectRatio="none">
            <defs>
              <linearGradient id="areaFill" x1="0" x2="0" y1="0" y2="1">
                <stop offset="0%" stop-color="#2df7c5" stop-opacity=".38" />
                <stop offset="100%" stop-color="#2df7c5" stop-opacity="0" />
              </linearGradient>
            </defs>
            <path class="trend-area" :d="heightAreaPath" />
            <polyline class="trend-line" :points="heightLine" />
            <line x1="0" y1="170" x2="720" y2="170" class="avg-line" />
          </svg>
          <div class="trend-labels">
            <span v-for="point in history" :key="point.label">{{ point.label }}</span>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed, defineComponent, h, onMounted, onUnmounted, ref } from 'vue'
import NodeCard from './components/NodeCard.vue'
import UploadPanel from './components/UploadPanel.vue'
import LogPanel from './components/LogPanel.vue'
import BlockChain from './components/BlockChain.vue'

const PanelTitle = defineComponent({
  props: {
    title: { type: String, required: true },
    accent: { type: String, default: '' }
  },
  setup(props) {
    return () => h('div', { class: 'panel-title' }, [
      h('div', { class: 'panel-title-main' }, [
        h('i'),
        h('span', props.title)
      ]),
      h('small', props.accent)
    ])
  }
})

const nodes = ref([
  { id: 'node-1', api: 'http://127.0.0.1:8001', rpc: '7001', org: '中心医院' },
  { id: 'node-2', api: 'http://127.0.0.1:8002', rpc: '7002', org: '三甲医院' },
  { id: 'node-3', api: 'http://127.0.0.1:8003', rpc: '7003', org: '社区医院' },
])

const nodeStatuses = ref({})
const blocks = ref([])
const logs = ref([])
const leaderAPI = ref(null)
const now = ref(new Date())
const history = ref(seedHistory())
const pulseFlow = ref(false)

let eventSource = null
let pollTimer = null
let chainTimer = null
let clockTimer = null
let pulseTimer = null

const nowText = computed(() => {
  return now.value.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
})

const onlineCount = computed(() => Object.values(nodeStatuses.value).filter(s => s?.online).length)
const currentTerm = computed(() => Math.max(0, ...Object.values(nodeStatuses.value).map(s => Number(s?.term || 0))))
const latestBlock = computed(() => [...blocks.value].sort((a, b) => Number(b.index || 0) - Number(a.index || 0))[0])
const blockHeight = computed(() => Number(latestBlock.value?.index ?? blocks.value.length))
const healthScore = computed(() => Math.round((onlineCount.value / Math.max(nodes.value.length, 1)) * 100))

const clusterStatus = computed(() => {
  if (onlineCount.value === 0) return 'offline'
  if (onlineCount.value < nodes.value.length) return 'warning'
  return 'online'
})

const clusterStatusText = computed(() => {
  if (clusterStatus.value === 'online') return '集群运行中'
  if (clusterStatus.value === 'warning') return '部分节点离线'
  return '集群未连接'
})

const leaderNode = computed(() => {
  const found = nodes.value.find(n => nodeStatuses.value[n.id]?.is_leader)
  return found || null
})

const leaderStatus = computed(() => leaderNode.value ? nodeStatuses.value[leaderNode.value.id] : null)
const leaderName = computed(() => leaderNode.value ? nodeShortName(leaderNode.value.id) : '选举中')
const quorumText = computed(() => {
  const quorum = Math.max(...Object.values(nodeStatuses.value).map(s => Number(s?.quorum || 0)), 0)
  return quorum ? `${quorum}/${nodes.value.length}` : `--/${nodes.value.length}`
})
const clusterFaultText = computed(() => {
  if (onlineCount.value === 0) return '集群离线'
  if (!leaderNode.value) return '等待选举'
  const risky = Object.values(nodeStatuses.value).some(s => s?.online && ['heartbeat_overdue', 'leader_unknown', 'election_in_progress'].includes(s.fault_state))
  return risky ? '观察中' : '稳定'
})

const avgLatency = computed(() => {
  const samples = Object.values(nodeStatuses.value).filter(s => s?.online && Number.isFinite(s.latency))
  if (!samples.length) return '--'
  return Math.round(samples.reduce((sum, s) => sum + s.latency, 0) / samples.length)
})

const todayUploads = computed(() => {
  const today = new Date().toDateString()
  const count = blocks.value.filter(b => b.timestamp && new Date(Number(b.timestamp) / 1e6).toDateString() === today).length
  return count || blocks.value.length
})

const chainIntegrity = computed(() => {
  const sorted = [...blocks.value].sort((a, b) => Number(a.index || 0) - Number(b.index || 0))
  if (sorted.length < 2) return true
  for (let i = 1; i < sorted.length; i += 1) {
    if (sorted[i].prev_hash && sorted[i - 1].hash && sorted[i].prev_hash !== sorted[i - 1].hash) {
      return false
    }
  }
  return true
})

const kpiItems = computed(() => [
  { label: '当前 Leader', value: leaderName.value, unit: '', sub: leaderAPI.value || '等待选举结果' },
  { label: '集群健康度', value: healthScore.value, unit: '%', sub: `${onlineCount.value}/${nodes.value.length} 节点在线` },
  { label: '最新区块高度', value: blockHeight.value, unit: '', sub: shortHash(latestBlock.value?.hash) },
  { label: '今日上链记录', value: todayUploads.value, unit: '条', sub: '来自 /api/records' },
  { label: '平均响应延迟', value: avgLatency.value, unit: avgLatency.value === '--' ? '' : 'ms', sub: '状态轮询实时采样' },
  { label: '故障转移状态', value: clusterFaultText.value, unit: '', sub: `多数派 ${quorumText.value}` },
])

const consensusSteps = computed(() => [
  { title: '医疗记录提交', desc: '前端写入 payload', kind: 'client' },
  { title: `${leaderName.value} 接收`, desc: '仅 Leader 可写', kind: 'leader' },
  { title: '日志复制', desc: replicationText.value, kind: 'replicate' },
  { title: '多数派提交', desc: `CommitIndex ${maxCommitIndex.value}`, kind: 'commit' },
  { title: '区块持久化', desc: `Height ${blockHeight.value}`, kind: 'block' },
])

const maxCommitIndex = computed(() => Math.max(0, ...Object.values(nodeStatuses.value).map(s => Number(s?.commit_index || 0))))
const replicationText = computed(() => {
  const peers = leaderStatus.value?.peers || []
  if (!peers.length) return `${Math.max(onlineCount.value - 1, 0)} 个 Follower 同步`
  const synced = peers.filter(p => p.state === 'synced').length
  const lagging = peers.filter(p => Number(p.lag || 0) > 0).length
  return `${synced}/${peers.length} 已同步，${lagging} 个滞后`
})

const syncRows = computed(() => {
  const peerMap = new Map((leaderStatus.value?.peers || []).map(p => [p.id, p]))
  return nodes.value.map(node => {
    const s = nodeStatuses.value[node.id] || {}
    const peer = peerMap.get(node.id)
    const role = !s.online ? 'Offline' : s.is_leader ? 'Leader' : (s.role || 'Follower')
    const fault = faultLabel(s, peer)
    return {
      id: node.id,
      role,
      roleClass: !s.online ? 'danger-text' : s.is_leader ? 'ok-text' : 'cyan-text',
      height: peer ? peer.match_index : (s.last_applied ?? s.commit_index ?? s.block_count ?? '--'),
      latency: s.online && Number.isFinite(s.latency) ? `${Math.round(s.latency)}ms` : '--',
      fault: fault.text,
      faultClass: fault.className,
      commit: Number(s.commit_index || 0)
    }
  }).sort((a, b) => b.commit - a.commit)
})

const radarMetrics = computed(() => [
  { label: '在线率', value: healthScore.value },
  { label: '一致性', value: chainIntegrity.value ? 96 : 42 },
  { label: '提交效率', value: avgLatency.value === '--' ? 35 : Math.max(35, 100 - Number(avgLatency.value)) },
  { label: 'Leader稳定', value: leaderStatus.value?.role_duration_ms ? Math.min(96, 55 + leaderStatus.value.role_duration_ms / 1000) : 30 },
  { label: '数据完整', value: blockHeight.value > 0 ? 88 : 55 },
])

const radarAxes = computed(() => radarMetrics.value.map((m, i) => radarPoint(90, i, radarMetrics.value.length, m.label)))
const radarScorePoints = computed(() => radarMetrics.value.map((m, i) => radarPoint((m.value / 100) * 90, i, radarMetrics.value.length, m.label)))
const radarScorePolygon = computed(() => radarScorePoints.value.map(p => `${p.x},${p.y}`).join(' '))

const latencyLine = computed(() => {
  if (history.value.length < 2) return ''
  return history.value.map((p, i) => {
    const x = (i / (history.value.length - 1)) * 100
    const y = 88 - ((100 - Math.min(p.latency, 100)) / 100) * 58
    return `${x},${y}`
  }).join(' ')
})

const heightLine = computed(() => historyLine('height'))
const heightAreaPath = computed(() => {
  const points = heightLine.value
  if (!points) return ''
  return `M0,210 L${points.replaceAll(' ', ' L')} L720,210 Z`
})

function seedHistory() {
  const list = []
  for (let i = 11; i >= 0; i -= 1) {
    list.push({
      label: `${12 - i}`,
      records: Math.max(0, Math.round(Math.sin(i / 2) * 4 + 8)),
      height: Math.max(0, 10 - i),
      latency: Math.round(42 + Math.cos(i) * 11),
      barA: 28 + Math.random() * 45,
      barB: 18 + Math.random() * 60,
    })
  }
  return list
}

function addLog(msg, type = 'info') {
  const ts = new Date().toLocaleTimeString('zh-CN', { hour12: false })
  logs.value.unshift({ ts, msg, type })
  if (logs.value.length > 60) logs.value.pop()
}

function nodeShortName(id) {
  const map = { 'node-1': 'Node 1', 'node-2': 'Node 2', 'node-3': 'Node 3' }
  return map[id] || id || '--'
}

function nodeRole(id) {
  const s = nodeStatuses.value[id]
  if (!s?.online) return 'Offline'
  if (s.is_leader) return 'Leader'
  if (s.election_state === 'campaigning') return 'Candidate'
  return s.role || 'Follower'
}

function nodeClass(node) {
  const s = nodeStatuses.value[node.id]
  return {
    online: s?.online,
    offline: !s?.online,
    leader: s?.is_leader
  }
}

function shortHash(hash) {
  if (!hash) return '--'
  return `${String(hash).slice(0, 8)}...${String(hash).slice(-6)}`
}

function faultLabel(status, peer) {
  if (!status?.online) return { text: '离线', className: 'danger-text' }
  if (status.is_leader) return { text: formatDuration(status.role_duration_ms), className: 'ok-text' }
  if (Number(peer?.lag || 0) > 0) return { text: `滞后 ${peer.lag}`, className: 'warn-text' }
  if (status.fault_state === 'heartbeat_overdue') return { text: '心跳超时', className: 'warn-text' }
  if (status.fault_state === 'leader_unknown') return { text: '等待Leader', className: 'warn-text' }
  if (status.fault_state === 'election_in_progress') return { text: '选举中', className: 'cyan-text' }
  return { text: '正常', className: 'ok-text' }
}

function formatDuration(ms) {
  if (!Number.isFinite(Number(ms)) || Number(ms) <= 0) return '刚刚'
  const seconds = Math.floor(Number(ms) / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  return `${Math.floor(minutes / 60)}h`
}

function radarPoint(radius, index, total, label) {
  const angle = -Math.PI / 2 + (Math.PI * 2 * index) / total
  const x = 110 + Math.cos(angle) * radius
  const y = 110 + Math.sin(angle) * radius
  const tx = 110 + Math.cos(angle) * 105
  const ty = 112 + Math.sin(angle) * 105
  return { x, y, tx, ty, label }
}

function radarPolygon(radius) {
  return radarMetrics.value.map((_, i) => {
    const p = radarPoint(radius, i, radarMetrics.value.length, '')
    return `${p.x},${p.y}`
  }).join(' ')
}

function historyLine(field) {
  if (history.value.length < 2) return ''
  const max = Math.max(1, ...history.value.map(p => Number(p[field] || 0)))
  return history.value.map((p, i) => {
    const x = (i / (history.value.length - 1)) * 720
    const y = 190 - (Number(p[field] || 0) / max) * 150
    return `${x.toFixed(1)},${y.toFixed(1)}`
  }).join(' ')
}

function updateHistory() {
  const label = new Date().toLocaleTimeString('zh-CN', { minute: '2-digit', second: '2-digit', hour12: false })
  const latency = avgLatency.value === '--' ? 80 : Number(avgLatency.value)
  const records = blocks.value.length
  const prev = history.value[history.value.length - 1]
  const delta = Math.max(0, records - Number(prev?.records || 0))
  history.value = [
    ...history.value.slice(-11),
    {
      label,
      records,
      height: blockHeight.value,
      latency,
      barA: Math.min(92, 24 + records * 5),
      barB: Math.min(95, 20 + Math.max(delta, 1) * 16),
    }
  ]
}

async function discoverNodes() {
  for (const n of nodes.value) {
    try {
      const r = await fetch(`${n.api}/api/cluster/nodes`, { signal: AbortSignal.timeout(2000) })
      const list = await r.json()
      if (Array.isArray(list) && list.length > 0) {
        nodes.value = list.map(x => ({
          id: x.id,
          api: x.api_addr,
          rpc: String(Number((x.api_addr || '').split(':').pop()) - 1000),
          org: nodeShortName(x.id)
        }))
        addLog(`发现 ${nodes.value.length} 个联盟节点`, 'ok')
        return
      }
    } catch {}
  }
  addLog('节点发现失败，使用默认三节点配置', 'warn')
}

async function pollNodes() {
  const next = { ...nodeStatuses.value }
  await Promise.allSettled(nodes.value.map(async n => {
    const started = performance.now()
    try {
      const r = await fetch(`${n.api}/api/node/status`, { signal: AbortSignal.timeout(1400) })
      const d = await r.json()
      const latency = performance.now() - started
      next[n.id] = { ...d, online: true, latency }
      if (d.is_leader) leaderAPI.value = n.api
      if (d.leader_addr) leaderAPI.value = d.leader_addr
    } catch {
      next[n.id] = { ...(next[n.id] || {}), online: false, latency: null }
    }
  }))
  nodeStatuses.value = next
}

async function pollChain() {
  for (const n of nodes.value) {
    try {
      const r = await fetch(`${n.api}/api/records?limit=80`, { signal: AbortSignal.timeout(1400) })
      const data = await r.json()
      if (Array.isArray(data)) {
        blocks.value = data
        updateHistory()
        return
      }
    } catch {}
  }
}

function connectSSE() {
  if (eventSource) eventSource.close()
  const api = leaderAPI.value || nodes.value[0]?.api
  if (!api) return
  try {
    eventSource = new EventSource(`${api}/api/events/stream`)
    eventSource.onopen = () => addLog('SSE 实时事件流已连接', 'ok')
    eventSource.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data)
        if (ev.type === 'block') {
          pulseConsensus()
          addLog(`新区块 #${ev.data.index} 已上链`, 'ok')
          pollChain()
        }
        if (ev.type === 'status') {
          addLog('节点状态发生变化', 'info')
          pollNodes()
        }
      } catch {}
    }
    eventSource.onerror = () => {
      if (eventSource) eventSource.close()
      setTimeout(connectSSE, 3000)
    }
  } catch {}
}

function pulseConsensus() {
  pulseFlow.value = true
  if (pulseTimer) clearTimeout(pulseTimer)
  pulseTimer = setTimeout(() => {
    pulseFlow.value = false
  }, 1800)
}

function onUploadSuccess(index) {
  pulseConsensus()
  addLog(`写入请求已提交至 Raft 日志，Index=${index}`, 'ok')
  setTimeout(pollChain, 600)
  setTimeout(pollChain, 1800)
  setTimeout(pollNodes, 1000)
}

onMounted(async () => {
  addLog('前端驾驶舱启动，正在连接联盟链集群', 'info')
  clockTimer = setInterval(() => {
    now.value = new Date()
  }, 1000)
  await discoverNodes()
  await pollNodes()
  await pollChain()
  connectSSE()
  pollTimer = setInterval(pollNodes, 2000)
  chainTimer = setInterval(pollChain, 7000)
})

onUnmounted(() => {
  if (eventSource) eventSource.close()
  if (pollTimer) clearInterval(pollTimer)
  if (chainTimer) clearInterval(chainTimer)
  if (clockTimer) clearInterval(clockTimer)
  if (pulseTimer) clearTimeout(pulseTimer)
})
</script>
