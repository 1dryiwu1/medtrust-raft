<template>
  <div class="chain-timeline">
    <template v-if="displayedBlocks.length === 0">
      <div class="empty-chain">
        <span>#</span>
        <strong>暂无区块数据</strong>
        <small>启动集群并提交医疗记录后，这里会显示最新链上区块。</small>
      </div>
    </template>

    <button
      v-for="(block, index) in displayedBlocks"
      :key="block.index"
      class="chain-block"
      :class="{ latest: index === 0 }"
      @click="showDetail(block)"
    >
      <span class="block-index">#{{ block.index }}</span>
      <span class="block-body">
        <b>{{ payloadTitle(block.payload) }}</b>
        <code>{{ shortHash(block.hash) }}</code>
      </span>
      <span class="block-time">{{ formatTime(block.timestamp) }}</span>
    </button>
  </div>

  <Teleport to="body">
    <div v-if="modalVisible" class="modal-backdrop" @click.self="modalVisible = false">
      <div class="block-modal">
        <button class="modal-close" @click="modalVisible = false" aria-label="关闭">×</button>
        <h3>区块 #{{ selectedBlock?.index }} 详情</h3>
        <div class="kv-list">
          <p v-for="[key, value] in modalItems" :key="key">
            <span>{{ key }}</span>
            <code>{{ value }}</code>
          </p>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { computed, ref } from 'vue'

const props = defineProps({
  blocks: { type: Array, default: () => [] }
})

const modalVisible = ref(false)
const selectedBlock = ref(null)

const displayedBlocks = computed(() => {
  return [...props.blocks]
    .sort((a, b) => Number(b.index || 0) - Number(a.index || 0))
    .slice(0, 6)
})

const modalItems = computed(() => {
  if (!selectedBlock.value) return []
  const block = selectedBlock.value
  return [
    ['Index', block.index ?? '--'],
    ['Term', block.term ?? '--'],
    ['Payload', block.payload || '--'],
    ['Timestamp', formatTime(block.timestamp)],
    ['Hash', block.hash || '--'],
    ['PrevHash', block.prev_hash || '--'],
  ]
})

function showDetail(block) {
  selectedBlock.value = block
  modalVisible.value = true
}

function formatTime(ts) {
  if (!ts) return '--'
  return new Date(Number(ts) / 1e6).toLocaleTimeString('zh-CN', { hour12: false })
}

function shortHash(hash) {
  if (!hash) return '--'
  return `${String(hash).slice(0, 12)}...${String(hash).slice(-8)}`
}

function payloadTitle(payload) {
  if (!payload) return '空载荷'
  try {
    const data = JSON.parse(payload)
    return data.type ? `${data.type} / ${data.patient_id || 'patient'}` : '结构化医疗记录'
  } catch {
    return String(payload).slice(0, 42)
  }
}
</script>

<style scoped>
.chain-timeline {
  display: grid;
  gap: 10px;
  max-height: 340px;
  overflow: auto;
  padding-right: 4px;
}

.chain-block {
  position: relative;
  display: grid;
  grid-template-columns: 70px 1fr 82px;
  align-items: center;
  gap: 12px;
  width: 100%;
  border: 1px solid rgba(47, 138, 184, 0.28);
  border-radius: 2px;
  padding: 12px 14px;
  color: inherit;
  text-align: left;
  background:
    linear-gradient(90deg, rgba(255, 160, 47, .06), transparent 42%),
    rgba(4, 17, 36, 0.76);
  cursor: pointer;
  transition: border-color .2s ease, transform .2s ease, background .2s ease;
}

.chain-block::before {
  content: "";
  position: absolute;
  left: 34px;
  top: -11px;
  width: 1px;
  height: 10px;
  background: rgba(0, 245, 255, .44);
}

.chain-block:first-of-type::before {
  display: none;
}

.chain-block:hover,
.chain-block.latest {
  border-color: rgba(255, 160, 47, 0.72);
  background: rgba(8, 39, 72, 0.84);
}

.chain-block:hover {
  transform: translateY(-1px);
}

.block-index {
  color: var(--yellow);
  font-family: Consolas, monospace;
  font-size: 18px;
  font-weight: 800;
}

.block-body {
  min-width: 0;
}

.block-body b {
  display: block;
  overflow: hidden;
  color: #f3fbff;
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.block-body code,
.kv-list code {
  color: rgba(203, 230, 255, 0.62);
  font-family: Consolas, monospace;
  font-size: 11px;
  word-break: break-all;
}

.block-time {
  color: rgba(203, 230, 255, 0.56);
  font-family: Consolas, monospace;
  font-size: 11px;
  text-align: right;
}

.empty-chain {
  display: grid;
  place-items: center;
  min-height: 180px;
  border: 1px dashed rgba(69, 211, 255, 0.22);
  border-radius: 3px;
  color: rgba(203, 230, 255, .58);
  text-align: center;
}

.empty-chain span {
  color: #2df7c5;
  font-size: 28px;
  font-weight: 900;
}

.empty-chain strong {
  color: #f3fbff;
}

.empty-chain small {
  max-width: 300px;
}

.modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 200;
  display: grid;
  place-items: center;
  padding: 24px;
  background: rgba(2, 8, 18, .78);
  backdrop-filter: blur(10px);
}

.block-modal {
  position: relative;
  width: min(640px, 100%);
  border: 1px solid rgba(255, 160, 47, .38);
  border-radius: 3px;
  padding: 24px;
  background: #071322;
  box-shadow: 0 24px 80px rgba(0, 0, 0, .45);
}

.block-modal h3 {
  margin-bottom: 18px;
  color: #f3fbff;
  font-size: 18px;
}

.modal-close {
  position: absolute;
  top: 14px;
  right: 14px;
  width: 30px;
  height: 30px;
  border: 1px solid rgba(203, 230, 255, .2);
  border-radius: 2px;
  color: #dff8ff;
  background: transparent;
  cursor: pointer;
}

.kv-list {
  display: grid;
  gap: 10px;
}

.kv-list p {
  display: grid;
  grid-template-columns: 90px 1fr;
  gap: 12px;
}

.kv-list span {
  color: rgba(203, 230, 255, .54);
  font-size: 12px;
}

@media (max-width: 640px) {
  .chain-block {
    grid-template-columns: 58px 1fr;
  }

  .block-time {
    grid-column: 2;
    text-align: left;
  }
}
</style>
