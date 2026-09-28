<template>
  <div class="modal-backdrop" @click.self="$emit('close')">
    <section class="record-modal draft-modal" role="dialog" aria-modal="true" aria-label="病历上链确认">
      <header>
        <div><small>上链确认单 · {{ draft.id }}</small><h2>{{ record.diagnosis || '待确认病历' }}</h2></div>
        <button class="icon-close" title="关闭" @click="$emit('close')">×</button>
      </header>
      <div class="detail-status">
        <span :class="`draft-state state-${draft.status}`">{{ statusLabel }}</span>
        <span>版本 V{{ record.version || 1 }}</span>
        <span>{{ record.hospital }} · {{ record.department }}</span>
      </div>
      <div class="approval-callout">
        <b>{{ role === 'patient' ? '请确认这是您同意上链的完整内容' : '患者确认后才可提交联盟链' }}</b>
        <p>诊断、说明、科室和附件摘要共同生成下方 SHA-256 指纹；任何内容变化都会使本次确认失效。</p>
      </div>
      <div class="detail-grid">
        <div><small>患者账号</small><b>{{ record.patient_id }}</b></div>
        <div><small>实名医生</small><b>{{ record.doctor_name || record.doctor_id }}</b></div>
        <div><small>病历类型</small><b>{{ record.type || '医疗记录' }}</b></div>
        <div><small>创建时间</small><b>{{ formatTime(draft.created_at) }}</b></div>
      </div>
      <div class="detail-section"><h3>诊疗说明</h3><p>{{ record.notes || '暂无补充说明' }}</p></div>
      <div v-if="record.correction_reason" class="detail-section correction-note"><h3>更正说明</h3><p>{{ record.correction_reason }}，引用原区块 #{{ record.previous_index }}</p></div>
      <div class="detail-section">
        <h3>附件清单</h3>
        <div v-if="!attachments.length" class="detail-empty">本病历未包含附件</div>
        <div v-for="file in attachments" :key="file.id" class="attachment-row"><div><b>{{ file.file_name }}</b><code>{{ file.sha256 }}</code></div><span>{{ formatSize(file.size) }}</span></div>
      </div>
      <div class="record-fingerprint"><small>患者确认的数据指纹</small><code>{{ draft.record_hash }}</code><span v-if="draft.consent_id">授权凭证 {{ draft.consent_id }}</span></div>
      <div v-if="draft.status === 'rejected'" class="rejection-note">拒绝原因：{{ draft.rejection_reason || '患者未填写原因' }}</div>
      <div v-if="role === 'patient' && draft.status === 'pending_patient_approval'" class="approval-actions">
        <label>拒绝原因（选填）<textarea v-model.trim="reason" rows="2" placeholder="说明需要医生修改的内容"></textarea></label>
        <div><button class="danger-btn" @click="$emit('reject',{id:draft.id,reason})">拒绝上链</button><button class="primary" @click="$emit('approve',draft.id)">确认内容并同意上链</button></div>
      </div>
      <div v-if="role === 'doctor' && draft.status === 'approved'" class="approval-actions doctor-submit"><p>患者已于 {{ formatTime(draft.approved_at) }} 确认此数据指纹。</p><button class="primary" @click="$emit('submit',draft.id)">提交联盟链</button></div>
    </section>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
const props=defineProps({draft:Object,role:String})
defineEmits(['close','approve','reject','submit'])
const reason=ref('')
const record=computed(()=>props.draft.record||{})
const attachments=computed(()=>Array.isArray(record.value.attachments)?record.value.attachments:[])
const statusLabel=computed(()=>({pending_patient_approval:'等待患者确认',approved:'患者已同意',rejected:'患者已拒绝',submitted:'已提交上链'}[props.draft.status]||props.draft.status))
function formatTime(value){return value?new Date(value).toLocaleString('zh-CN'):'--'}
function formatSize(size){return size<1024?`${size} B`:size<1048576?`${(size/1024).toFixed(1)} KB`:`${(size/1048576).toFixed(1)} MB`}
</script>
