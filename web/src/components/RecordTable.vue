<template>
  <div v-if="records?.length" class="record-table">
    <article v-for="block in records" :key="block.index" class="record-row record-clickable" @click="$emit('select', block)">
      <div class="record-date"><b>{{ dateOf(block) }}</b><span>#{{ block.index }}</span></div>
      <div class="record-main"><b>{{ valueOf(block, 'diagnosis') || valueOf(block, 'type') || '加密医疗存证' }}</b><span>{{ valueOf(block, 'department') || '隐私字段已保护' }} · {{ valueOf(block, 'doctor_name') || valueOf(block, 'doctor_id') || '身份已匿名' }}</span><p v-if="expanded">{{ valueOf(block, 'notes') || '点击查看可信存证详情' }}</p></div>
      <div class="record-flags"><span v-if="valueOf(block, 'version')" class="version-tag">V{{ valueOf(block, 'version') }}</span><span class="chain-status" :class="{muted:valueOf(block,'status')==='superseded'}">{{ valueOf(block,'status')==='superseded'?'已被更正':'已上链' }}</span></div>
    </article>
  </div>
  <div v-else class="empty-state"><b>暂无病历记录</b><span>新的可信病历将在完成共识后显示在这里</span></div>
</template>

<script setup>
defineProps({ records: Array, expanded: Boolean })
defineEmits(['select'])
function recordOf(block) { try { const payload=JSON.parse(block.payload); return payload.record || payload } catch { return {} } }
function valueOf(block, key) { return recordOf(block)[key] }
function dateOf(block) { return new Date(Number(block.timestamp)/1e6).toLocaleDateString('zh-CN',{month:'2-digit',day:'2-digit'}) }
</script>
