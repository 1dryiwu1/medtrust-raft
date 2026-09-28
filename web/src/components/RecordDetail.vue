<template>
  <div class="modal-backdrop" @click.self="$emit('close')">
    <section class="record-modal" role="dialog" aria-modal="true" aria-label="病历详情">
      <header><div><small>可信病历详情 · 区块 #{{ block.index }}</small><h2>{{ record.diagnosis || record.type || '医疗记录' }}</h2></div><button class="icon-close" title="关闭" @click="$emit('close')">×</button></header>
      <div class="detail-status"><span :class="record.status==='superseded'?'status-old':'status-ok'">{{ record.status==='superseded'?'历史版本':'当前有效版本' }}</span><span>版本 V{{ record.version || 1 }}</span><span>{{ record.hospital || '医疗机构' }} · {{ record.department || '科室未标注' }}</span></div>
      <div class="detail-grid"><div><small>患者账号</small><b>{{ record.patient_id }}</b></div><div><small>实名医生</small><b>{{ record.doctor_name || record.doctor_id }}</b></div><div><small>病历类型</small><b>{{ record.type || '医疗记录' }}</b></div><div><small>上链日期</small><b>{{ new Date(Number(block.timestamp)/1e6).toLocaleString('zh-CN') }}</b></div></div>
      <div class="detail-section"><h3>诊疗说明</h3><p>{{ record.notes || '暂无补充说明' }}</p></div>
      <div v-if="record.correction_reason" class="detail-section correction-note"><h3>更正说明</h3><p>{{ record.correction_reason }}，引用原区块 #{{ record.previous_index }}</p></div>
	  <div v-if="record.patient_consent_id" class="consent-evidence"><div><small>患者上链授权凭证</small><b>{{ record.patient_consent_id }}</b></div><div><small>患者确认时间</small><b>{{ new Date(record.patient_approved_at).toLocaleString('zh-CN') }}</b></div><code>{{ record.patient_approved_hash }}</code><span>该摘要与患者确认的病历内容一致</span></div>
      <div class="detail-section"><h3>附件哈希存证</h3><div v-if="!attachments.length" class="detail-empty">本病历未包含附件</div><div v-for="file in attachments" :key="file.id" class="attachment-row"><div><b>{{ file.file_name }}</b><code>{{ shortHash(file.sha256) }}</code></div><div><a :href="`/api/portal/attachments/${file.id}/download`">下载</a><button @click="$emit('verify',file)">校验</button></div><p v-if="verification[file.id]" :class="verification[file.id].valid?'verify-ok':'verify-bad'">{{ verification[file.id].valid?'哈希一致，文件完整':'哈希不一致，请立即核查' }}</p></div></div>
      <details class="evidence-panel"><summary>查看链上凭证</summary><dl><dt>病历编号</dt><dd>{{ record.record_id || '--' }}</dd><dt>区块哈希</dt><dd>{{ block.hash }}</dd><dt>前序哈希</dt><dd>{{ block.prev_hash }}</dd></dl></details>
      <form v-if="canCorrect" class="correction-form" @submit.prevent="submitCorrection"><h3>生成更正版本</h3><div class="form-grid"><label>更正后诊断<input v-model="form.diagnosis" required></label><label>科室<input v-model="form.department" required></label><label class="full">诊疗说明<textarea v-model="form.notes" rows="3"></textarea></label><label class="full">更正原因<input v-model="form.reason" required placeholder="例如：补充检查结果或修正录入错误"></label></div><button class="primary">提交更正并重新上链</button></form>
    </section>
  </div>
</template>

<script setup>
import { computed, reactive } from 'vue'
const props=defineProps({block:Object,role:String,username:String,verification:{type:Object,default:()=>({})}})
const emit=defineEmits(['close','verify','correct'])
const envelope=computed(()=>{try{return JSON.parse(props.block.payload)}catch{return {}}})
const record=computed(()=>envelope.value.record||{})
const attachments=computed(()=>Array.isArray(record.value.attachments)?record.value.attachments:[])
const canCorrect=computed(()=>props.role==='doctor'&&record.value.status!=='superseded'&&record.value.doctor_id===props.username)
const form=reactive({diagnosis:record.value.diagnosis||'',department:record.value.department||'',notes:record.value.notes||'',reason:''})
function shortHash(hash){return hash?`${hash.slice(0,12)}...${hash.slice(-8)}`:'--'}
function submitCorrection(){emit('correct',{index:props.block.index,...form})}
</script>
