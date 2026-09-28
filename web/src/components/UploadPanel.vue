<template>
  <div class="upload-panel">
    <div class="leader-line">
      <span>写入目标</span>
      <code>{{ leaderApi || '正在探测 Leader...' }}</code>
    </div>

    <div class="template-row">
      <button
        v-for="item in templateList"
        :key="item.key"
        type="button"
        :class="{ active: form.record_type === item.key }"
        @click="selectTemplate(item.key)"
      >
        {{ item.label }}
      </button>
    </div>

    <div class="form-grid">
      <label class="field">
        <span>患者编号</span>
        <input v-model="form.patient_id" placeholder="P001" />
      </label>
      <label class="field">
        <span>就诊科室</span>
        <input v-model="form.department" placeholder="呼吸内科" />
      </label>
      <label class="field">
        <span>医生编号</span>
        <input v-model="form.doctor_id" placeholder="DOC-2026-001" />
      </label>
      <label class="field">
        <span>医院编号</span>
        <input v-model="form.hospital_id" placeholder="HOSP-001" />
      </label>
    </div>

    <label class="field">
      <span>诊断摘要</span>
      <textarea
        v-model="form.diagnosis"
        placeholder="输入诊断摘要，Ctrl+Enter 快速提交。"
        @keydown.ctrl.enter="upload"
      ></textarea>
    </label>

    <label class="field">
      <span>检查结果 / 处方说明</span>
      <textarea
        v-model="form.result"
        placeholder="输入检查结果、处方说明或其他需要存证的医疗摘要。"
        @keydown.ctrl.enter="upload"
      ></textarea>
    </label>

    <label class="field">
      <span>加密载荷摘要</span>
      <input v-model="form.encrypted" placeholder="AES256-GCM:record:..." />
    </label>

    <label class="field">
      <span>API Key 可选</span>
      <input v-model="apiKey" placeholder="后端启用 api_key 时填写" />
    </label>

    <div class="action-row">
      <button class="submit-btn" :disabled="uploading" @click="upload">
        <span>{{ uploading ? '提交中' : '提交上链' }}</span>
        <i></i>
      </button>
      <button class="validate-btn" :disabled="validating" @click="validateChain">
        {{ validating ? '校验中' : '校验链完整性' }}
      </button>
    </div>

    <div v-if="lastEvidence" class="evidence-card">
      <div>
        <span>存证编号</span>
        <code>{{ lastEvidence.certificate_id }}</code>
      </div>
      <div>
        <span>数据哈希</span>
        <code>{{ shortHash(lastEvidence.data_hash) }}</code>
      </div>
      <div>
        <span>Raft Index</span>
        <code>#{{ lastEvidence.index }}</code>
      </div>
    </div>

    <p v-if="result" class="result" :class="resultType">{{ result }}</p>
    <p v-if="validateResult" class="result" :class="validateType">{{ validateResult }}</p>
    <p class="hint">Follower 会返回 leader_addr，前端将自动重定向到 Leader 再提交。</p>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'

const props = defineProps({
  leaderApi: { type: String, default: null }
})

const emit = defineEmits(['upload-success'])

const apiKey = ref('')
const uploading = ref(false)
const validating = ref(false)
const result = ref('')
const resultType = ref('')
const validateResult = ref('')
const validateType = ref('')
const lastEvidence = ref(null)

const templateList = [
  { key: 'diagnosis', label: '诊断' },
  { key: 'prescription', label: '处方' },
  { key: 'examination', label: '检查' },
]

const form = reactive({
  record_type: 'diagnosis',
  patient_id: '',
  department: '',
  doctor_id: '',
  hospital_id: '',
  diagnosis: '',
  result: '',
  encrypted: '',
})

const templates = {
  diagnosis: {
    record_type: 'diagnosis',
    patient_id: 'P001',
    department: '呼吸内科',
    doctor_id: 'DOC-2026-001',
    hospital_id: 'HOSP-001',
    diagnosis: '社区获得性肺炎，建议进行抗感染治疗并复查胸部影像。',
    result: '体温 38.2 摄氏度，白细胞轻度升高，胸部 CT 提示右下肺感染灶。',
    encrypted: 'AES256-GCM:diag:9f4a21c8',
  },
  prescription: {
    record_type: 'prescription',
    patient_id: 'P002',
    department: '心内科',
    doctor_id: 'DOC-2026-018',
    hospital_id: 'HOSP-001',
    diagnosis: '高血压二级，需持续监测血压并规范用药。',
    result: '处方：氨氯地平 5mg qd，建议两周后复诊。',
    encrypted: 'AES256-GCM:rx:3b77e5a1',
  },
  examination: {
    record_type: 'examination',
    patient_id: 'P003',
    department: '影像科',
    doctor_id: 'DOC-2026-031',
    hospital_id: 'HOSP-002',
    diagnosis: '术后复查，需确认影像报告未被篡改。',
    result: '胸部 CT 平扫结果哈希 sha256:a3f9c2d8e1b4，影像附件已脱敏保存。',
    encrypted: 'AES256-GCM:exam:84cd0aa9',
  },
}

selectTemplate('diagnosis')

function selectTemplate(key) {
  Object.assign(form, templates[key])
}

function buildPayload() {
  return {
    type: form.record_type,
    patient_id: form.patient_id.trim(),
    department: form.department.trim(),
    doctor_id: form.doctor_id.trim(),
    hospital_id: form.hospital_id.trim(),
    diagnosis: form.diagnosis.trim(),
    result: form.result.trim(),
    encrypted: form.encrypted.trim(),
    timestamp: new Date().toISOString(),
  }
}

async function upload() {
  const payloadData = buildPayload()
  if (!payloadData.patient_id || !payloadData.department || !payloadData.doctor_id || !payloadData.diagnosis) {
    showResult('请至少填写患者编号、科室、医生编号和诊断摘要', 'err')
    return
  }

  uploading.value = true
  result.value = ''
  validateResult.value = ''

  const headers = { 'Content-Type': 'application/json' }
  if (apiKey.value.trim()) headers['X-API-Key'] = apiKey.value.trim()

  const target = props.leaderApi || 'http://127.0.0.1:8001'
  try {
    const data = await postPayload(target, payloadData, headers)
    handleUploadOk(data)
  } catch (error) {
    if (error.status === 503 && error.data?.leader_addr) {
      try {
        const redirected = await postPayload(error.data.leader_addr, payloadData, headers)
        handleUploadOk(redirected, true)
      } catch (nextError) {
        showResult(nextError.message || '重定向提交失败', 'err')
      }
    } else {
      showResult(error.message || '提交失败，请确认节点正在运行', 'err')
    }
  } finally {
    uploading.value = false
  }
}

function handleUploadOk(data, redirected = false) {
  lastEvidence.value = data
  const prefix = redirected ? '已重定向 Leader 并提交成功' : '已提交至 Raft 日志'
  showResult(`${prefix}，存证编号 ${data.certificate_id}，Index=${data.index}`, 'ok')
  emit('upload-success', data.index)
}

async function postPayload(target, payloadData, headers) {
  const res = await fetch(`${target}/api/record/upload`, {
    method: 'POST',
    headers,
    body: JSON.stringify({ payload: payloadData }),
    signal: AbortSignal.timeout(5000)
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    const error = new Error(data.error || (res.status === 401 ? 'API Key 认证失败' : '提交失败'))
    error.status = res.status
    error.data = data
    throw error
  }
  return data
}

async function validateChain() {
  validating.value = true
  validateResult.value = ''
  const target = props.leaderApi || 'http://127.0.0.1:8001'
  try {
    const res = await fetch(`${target}/api/chain/validate`, { signal: AbortSignal.timeout(5000) })
    const data = await res.json().catch(() => ({}))
    if (!res.ok) throw new Error(data.error || '链完整性校验失败')
    if (data.valid) {
      showValidate(`链完整性校验通过，共 ${data.block_count} 个业务区块，最新哈希 ${shortHash(data.latest_hash)}`, 'ok')
    } else {
      showValidate(`链完整性异常：区块 #${data.invalid_index} 校验失败`, 'err')
    }
  } catch (error) {
    showValidate(error.message || '链完整性校验失败，请确认节点正在运行', 'err')
  } finally {
    validating.value = false
  }
}

function shortHash(hash) {
  if (!hash) return '--'
  return `${String(hash).slice(0, 12)}...${String(hash).slice(-8)}`
}

function showResult(message, type) {
  result.value = message
  resultType.value = type
}

function showValidate(message, type) {
  validateResult.value = message
  validateType.value = type
}
</script>

<style scoped>
.upload-panel {
  display: grid;
  gap: 12px;
}

.leader-line {
  display: grid;
  gap: 4px;
  border: 1px solid rgba(47, 138, 184, 0.28);
  border-radius: 3px;
  padding: 10px 12px;
  background: rgba(4, 17, 36, .64);
}

.leader-line span,
.field span,
.evidence-card span {
  color: rgba(203, 230, 255, .54);
  font-size: 11px;
}

.leader-line code,
.evidence-card code {
  color: var(--mint);
  font-family: Consolas, monospace;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.template-row,
.action-row {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
}

.action-row {
  grid-template-columns: 1fr 1fr;
}

.template-row button,
.submit-btn,
.validate-btn {
  border: 1px solid rgba(47, 138, 184, .3);
  border-radius: 2px;
  color: rgba(223, 248, 255, .78);
  background: rgba(4, 17, 36, .72);
  cursor: pointer;
  transition: border-color .2s ease, color .2s ease, background .2s ease, transform .2s ease;
}

.template-row button {
  height: 34px;
  font-weight: 700;
}

.template-row button.active,
.template-row button:hover,
.validate-btn:hover:not(:disabled) {
  border-color: rgba(255, 160, 47, .68);
  color: var(--yellow);
  background: rgba(255, 160, 47, .08);
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}

.field {
  display: grid;
  gap: 6px;
}

.field textarea,
.field input {
  width: 100%;
  border: 1px solid rgba(47, 138, 184, .3);
  border-radius: 3px;
  color: #e8f8ff;
  background: rgba(2, 8, 18, .58);
  outline: none;
  transition: border-color .2s ease, box-shadow .2s ease;
}

.field textarea {
  min-height: 76px;
  resize: vertical;
  padding: 10px 12px;
  font-family: inherit;
  font-size: 12px;
  line-height: 1.5;
}

.field input {
  height: 36px;
  padding: 0 12px;
}

.field textarea:focus,
.field input:focus {
  border-color: rgba(255, 160, 47, .74);
  box-shadow: 0 0 0 2px rgba(255, 160, 47, .12);
}

.submit-btn,
.validate-btn {
  height: 42px;
  font-weight: 900;
}

.submit-btn {
  position: relative;
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 8px;
  color: #050a14;
  background: linear-gradient(90deg, var(--yellow), #ffd166);
  overflow: hidden;
}

.submit-btn:hover:not(:disabled),
.validate-btn:hover:not(:disabled) {
  transform: translateY(-1px);
}

.submit-btn:disabled,
.validate-btn:disabled {
  color: rgba(203, 230, 255, .45);
  background: rgba(69, 211, 255, .08);
  cursor: not-allowed;
}

.submit-btn i {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #051421;
}

.evidence-card {
  display: grid;
  gap: 8px;
  border: 1px solid rgba(0, 245, 255, .24);
  border-radius: 3px;
  padding: 10px 12px;
  background: rgba(0, 245, 255, .06);
}

.evidence-card div {
  display: grid;
  gap: 3px;
  min-width: 0;
}

.result {
  border-radius: 3px;
  padding: 10px 12px;
  font-size: 12px;
  line-height: 1.5;
}

.result.ok {
  border: 1px solid rgba(0, 245, 255, .3);
  color: var(--mint);
  background: rgba(0, 245, 255, .08);
}

.result.err {
  border: 1px solid rgba(255, 91, 117, .28);
  color: #ff8ea0;
  background: rgba(255, 91, 117, .08);
}

.hint {
  color: rgba(203, 230, 255, .48);
  font-size: 11px;
  line-height: 1.5;
}

@media (max-width: 720px) {
  .form-grid,
  .action-row {
    grid-template-columns: 1fr;
  }
}
</style>
