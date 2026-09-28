<template>
  <div v-if="!session" class="auth-page">
    <section class="auth-brand">
      <div class="brand-mark">M</div>
      <p class="eyebrow">MEDTRUST HEALTH NETWORK</p>
      <h1>让每一份病历<br>可信、可查、可追溯</h1>
      <p class="brand-copy">医疗数据经联盟链共识确认后留存，患者掌握自己的病历视图，医生完成可信提交。</p>
      <div class="trust-row"><span>三节点共识</span><span>隐私访问控制</span><span>完整性校验</span></div>
    </section>
    <main class="auth-main">
      <div class="auth-box">
        <div class="mobile-brand"><b>MedTrust</b><span>可信医疗数据平台</span></div>
        <div class="auth-tabs">
          <button :class="{ active: mode === 'login' }" @click="mode='login'">登录</button>
          <button :class="{ active: mode === 'register' }" @click="mode='register'">患者注册</button>
        </div>
        <h2>{{ mode === 'login' ? '欢迎回来' : '创建患者账户' }}</h2>
        <p>{{ mode === 'login' ? '登录后进入您的专属医疗数据空间' : '注册后即可查看属于您的可信病历' }}</p>
        <form @submit.prevent="submitAuth">
          <label>用户名<input v-model.trim="credentials.username" autocomplete="username" placeholder="至少 3 个字符" required></label>
          <label>密码<input v-model="credentials.password" type="password" :autocomplete="mode === 'login' ? 'current-password' : 'new-password'" :minlength="mode === 'register' ? 12 : undefined" :placeholder="mode === 'login' ? '请输入密码' : '至少 12 个字符'" required></label>
          <div v-if="authError" class="form-error">{{ authError }}</div>
          <button class="primary wide" :disabled="loading">{{ loading ? '请稍候...' : mode === 'login' ? '登录平台' : '创建账户' }}</button>
        </form>
      </div>
    </main>
  </div>

  <div v-else-if="session.role === 'admin' && adminView" class="admin-screen">
    <button class="admin-return" @click="adminView=false">返回管理门户</button>
    <AdminDashboard />
  </div>

  <div v-else class="portal-layout">
    <aside class="portal-sidebar">
      <div class="portal-logo"><span>M</span><div><b>MedTrust</b><small>可信医疗平台</small></div></div>
      <nav>
        <button :class="{active: page==='home'}" @click="page='home'">概览</button>
        <button v-if="session.role==='patient'" :class="{active: page==='records'}" @click="page='records'">我的病历</button>
        <button v-if="session.role==='patient'" :class="{active: page==='consents'}" @click="openConsents">授权管理</button>
		<button v-if="session.role==='patient'" :class="{active: page==='drafts'}" @click="openDrafts">上链确认<span v-if="pendingDrafts" class="nav-count">{{ pendingDrafts }}</span></button>
        <button v-if="session.role==='doctor'" :class="{active: page==='upload'}" @click="page='upload'">上传病历</button>
		<button v-if="session.role==='doctor'" :class="{active: page==='drafts'}" @click="openDrafts">上链申请<span v-if="approvedDrafts" class="nav-count">{{ approvedDrafts }}</span></button>
        <button v-if="session.role==='doctor'" :class="{active: page==='records'}" @click="page='records'">提交记录</button>
        <button v-if="session.role==='admin'" :class="{active: page==='accounts'}" @click="openAccounts">账户管理</button>
        <button v-if="session.role==='admin'" @click="adminView=true">区块链后台</button>
        <button class="mobile-logout" @click="logout">退出</button>
      </nav>
      <div class="sidebar-foot"><div class="avatar">{{ session.username[0].toUpperCase() }}</div><div><b>{{ session.username }}</b><small>{{ roleLabel }}</small></div><button title="退出登录" @click="logout">退出</button></div>
    </aside>

    <main class="portal-main">
      <header><div><p>{{ today }}</p><h1>{{ pageTitle }}</h1></div><span class="secure-state"><i></i>安全连接</span></header>
      <div v-if="uploadMessage && page!=='upload' && page!=='accounts'" class="global-toast">{{ uploadMessage }}</div>

      <section v-if="page==='home'" class="portal-content">
        <div class="welcome-band"><div><small>{{ roleLabel }}工作空间</small><h2>{{ greeting }}，{{ session.real_name || session.username }}</h2><p>{{ homeMessage }}</p><span v-if="session.role==='doctor'" class="identity-badge" :class="{pending:!session.verified}">{{ session.verified?'执业身份已认证':'执业身份待认证' }}</span></div><div class="chain-seal"><b>SHA-256</b><span>可信存证</span></div></div>
        <div class="metric-grid"><article><span>相关病历</span><b>{{ records.length }}</b><small>区块链记录</small></article><article><span>已确认</span><b>{{ records.length }}</b><small>多数派共识完成</small></article><article><span>网络状态</span><b class="green">正常</b><small>三节点服务</small></article></div>
        <section class="section-block"><div class="section-head"><div><h3>最近病历</h3><p>按区块确认时间排序</p></div><button class="text-btn" @click="page='records'">查看全部</button></div><RecordTable :records="records.slice(-5).reverse()" @select="selectedRecord=$event" /></section>
      </section>

      <section v-else-if="page==='records'" class="portal-content">
        <div class="section-block"><div class="section-head"><div><h3>{{ session.role==='patient' ? '我的可信病历' : '授权病历与提交记录' }}</h3><p>点击记录查看版本关系、附件摘要和链上凭证</p></div><button class="secondary" @click="loadRecords">刷新</button></div><RecordTable :records="records.slice().reverse()" expanded @select="selectedRecord=$event" /></div>
      </section>

      <section v-else-if="page==='consents'" class="portal-content">
        <div class="section-block"><div class="section-head"><div><h3>医生访问授权</h3><p>授权到期或撤销后，医生将无法继续读取和提交您的病历</p></div></div><div class="doctor-grid"><article v-for="item in doctors" :key="item.username" class="doctor-card"><div class="doctor-avatar">{{ item.real_name?.[0] || '医' }}</div><div><h4>{{ item.real_name }}</h4><p>{{ item.hospital }} · {{ item.department }}</p><small>执业证号 {{ item.license_no }}</small></div><span class="verified-mark">已实名</span><button v-if="!activeConsent(item.username)" class="primary" @click="grantConsent(item.username)">授权 30 天</button><button v-else class="danger-btn" @click="revokeConsent(item.username)">撤销授权</button></article><div v-if="!doctors.length" class="empty-state">暂无已认证医生</div></div></div>
      </section>

      <section v-else-if="page==='upload'" class="portal-content form-layout">
        <form class="section-block record-form" @submit.prevent="uploadRecord">
		  <div class="section-head"><div><h3>新建病历上链申请</h3><p>保存后由患者核对具体内容并决定是否同意上链</p></div></div>
          <div class="form-grid"><label>患者用户名<input v-model.trim="record.patient_id" required placeholder="须已向您授权的患者"></label><label>就诊科室<input v-model.trim="record.department" required placeholder="例如：心内科"></label><label>诊断结果<input v-model.trim="record.diagnosis" required placeholder="请输入诊断"></label><label>病历类型<select v-model="record.type"><option>门诊记录</option><option>检查报告</option><option>处方记录</option><option>住院记录</option></select></label><label class="full">诊疗说明<textarea v-model.trim="record.notes" rows="5" placeholder="症状、检查结果、处置建议"></textarea></label><label class="full file-field">检查报告或处方附件<input type="file" multiple accept=".pdf,.png,.jpg,.jpeg,.txt,.csv" @change="selectFiles"><span>支持 PDF、图片和文本，单个文件不超过 10MB；文件加密保存，SHA-256 摘要写入病历存证。</span></label></div>
          <div v-if="selectedFiles.length" class="selected-files"><span v-for="file in selectedFiles" :key="file.name">{{ file.name }} · {{ formatSize(file.size) }}</span></div>
          <div v-if="uploadMessage" class="success-box">{{ uploadMessage }}</div>
		  <div class="form-actions"><button type="button" class="secondary" @click="resetRecord">清空</button><button class="primary" :disabled="loading">{{ loading ? '正在保存加密草稿...' : '发起患者上链确认' }}</button></div>
        </form>
		<aside class="submit-guide"><h3>上链过程</h3><ol><li><b>生成加密草稿</b><span>锁定病历和附件摘要</span></li><li><b>患者明确确认</b><span>同意这份具体数据上链</span></li><li><b>联盟链共识</b><span>摘要匹配后由多数节点确认</span></li></ol></aside>
      </section>

	  <section v-else-if="page==='drafts'" class="portal-content">
		<div class="section-block"><div class="section-head"><div><h3>{{ session.role==='patient'?'病历上链确认':'病历上链申请' }}</h3><p>{{ session.role==='patient'?'只有经您明确同意的具体病历内容才能进入联盟链':'患者确认的数据指纹与草稿一致后才能提交联盟链' }}</p></div><button class="secondary" @click="loadDrafts">刷新</button></div>
		  <div v-if="drafts.length" class="draft-list"><article v-for="item in sortedDrafts" :key="item.id" class="draft-card" @click="selectedDraft=item"><div class="draft-icon">{{ item.record?.version>1?'V'+item.record.version:'病历' }}</div><div class="draft-copy"><small>{{ item.id }}</small><h4>{{ item.record?.diagnosis }}</h4><p>{{ item.record?.hospital }} · {{ item.record?.department }} · {{ item.record?.doctor_name }}</p><code>{{ shortHash(item.record_hash) }}</code></div><div class="draft-meta"><span :class="`draft-state state-${item.status}`">{{ draftStatus(item.status) }}</span><time>{{ formatDateTime(item.created_at) }}</time></div></article></div>
		  <div v-else class="empty-state"><b>暂无上链确认记录</b><span>{{ session.role==='patient'?'医生发起申请后会显示在这里':'新建病历并发起患者确认后会显示在这里' }}</span></div>
		</div>
	  </section>

      <section v-else-if="page==='accounts'" class="portal-content form-layout">
        <form class="section-block record-form" @submit.prevent="createDoctor"><div class="section-head"><div><h3>实名创建医生账户</h3><p>管理员核验执业资料后创建，账号将标记为已认证</p></div></div><div class="form-grid"><label>医生用户名<input v-model.trim="doctor.username" required></label><label>初始密码<input v-model="doctor.password" type="password" minlength="12" maxlength="128" autocomplete="new-password" placeholder="至少 12 个字符" required></label><label>真实姓名<input v-model.trim="doctor.real_name" required></label><label>执业证号<input v-model.trim="doctor.license_no" required></label><label>所属医院<input v-model.trim="doctor.hospital" required></label><label>执业科室<input v-model.trim="doctor.department" required></label></div><div v-if="uploadMessage" class="success-box">{{ uploadMessage }}</div><div class="form-actions"><button class="primary">核验并创建账户</button></div></form>
        <aside class="submit-guide account-list"><h3>平台账户</h3><div v-for="user in users" :key="user.username" class="account-row"><span class="avatar">{{ user.username[0].toUpperCase() }}</span><div><b>{{ user.real_name || user.username }}</b><small>{{ {patient:'患者',doctor:user.verified?'实名医生':'待认证医生',admin:'管理员'}[user.role] }}</small></div></div></aside>
      </section>
    </main>
  </div>
  <RecordDetail v-if="selectedRecord" :block="selectedRecord" :role="session.role" :username="session.username" :verification="verification" @close="selectedRecord=null" @verify="verifyAttachment" @correct="correctRecord" />
	<DraftDetail v-if="selectedDraft" :draft="selectedDraft" :role="session.role" @close="selectedDraft=null" @approve="approveDraft" @reject="rejectDraft" @submit="submitDraft" />
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import AdminDashboard from './AdminDashboard.vue'
import RecordTable from './components/RecordTable.vue'
import RecordDetail from './components/RecordDetail.vue'
import DraftDetail from './components/DraftDetail.vue'

const session = ref(null), mode = ref('login'), page = ref('home'), loading = ref(false), authError = ref(''), uploadMessage = ref(''), adminView = ref(false), records = ref([]), users = ref([]), doctors = ref([]), consents = ref([]), drafts = ref([]), selectedFiles = ref([]), selectedRecord = ref(null), selectedDraft = ref(null), verification = ref({})
const credentials = ref({username:'',password:''}), record = ref({patient_id:'',department:'',diagnosis:'',type:'门诊记录',notes:''}), doctor = ref({username:'',password:'',real_name:'',license_no:'',hospital:'',department:''})
const headers = () => ({'Content-Type':'application/json'})
const roleLabel = computed(()=>({patient:'患者',doctor:'医生',admin:'管理员'}[session.value?.role]||''))
const pageTitle = computed(()=>({home:'工作台',records:session.value?.role==='patient'?'我的病历':'授权病历',consents:'授权管理',drafts:session.value?.role==='patient'?'上链确认':'上链申请',upload:'上传病历',accounts:'账户管理'}[page.value]))
const pendingDrafts=computed(()=>drafts.value.filter(item=>item.status==='pending_patient_approval').length)
const approvedDrafts=computed(()=>drafts.value.filter(item=>item.status==='approved').length)
const sortedDrafts=computed(()=>drafts.value.slice().sort((a,b)=>new Date(b.created_at)-new Date(a.created_at)))
const greeting = computed(()=>new Date().getHours()<12?'上午好':new Date().getHours()<18?'下午好':'晚上好')
const today = new Date().toLocaleDateString('zh-CN',{year:'numeric',month:'long',day:'numeric',weekday:'long'})
const homeMessage = computed(()=>session.value?.role==='patient'?'这里汇集了属于您的可信医疗记录。':session.value?.role==='doctor'?'从患者身份核验开始，完成一份可信病历提交。':'管理平台账户并查看联盟链运行状态。')

async function api(path, options={}) { const baseHeaders=options.body instanceof FormData?{}:headers();const res=await fetch('/api'+path,{...options,headers:{...baseHeaders,...(options.headers||{})}}); if(!res.ok){let d={};try{d=await res.json()}catch{};throw new Error(d.error||'请求失败')}; return res.status===204?null:res.json() }
async function submitAuth(){loading.value=true;authError.value='';try{if(mode.value==='register'){await api('/auth/register',{method:'POST',body:JSON.stringify(credentials.value)});mode.value='login';authError.value='注册成功，请登录';return}const data=await api('/auth/login',{method:'POST',body:JSON.stringify(credentials.value)});session.value=data;record.value.department=data.department||'';await Promise.all([loadRecords(),loadDrafts()])}catch(e){authError.value=e.message}finally{loading.value=false}}
async function logout(){try{await api('/auth/logout',{method:'POST'})}catch{}session.value=null;adminView.value=false;page.value='home'}
async function loadRecords(){try{records.value=await api('/portal/records')}catch{records.value=[]}}
async function uploadRecord(){loading.value=true;uploadMessage.value='';try{const attachments=[];for(const file of selectedFiles.value){const form=new FormData();form.append('patient_id',record.value.patient_id);form.append('file',file);attachments.push(await api('/portal/attachments',{method:'POST',body:form}))}const data=await api('/portal/record-drafts',{method:'POST',body:JSON.stringify({payload:{...record.value,attachments}})});uploadMessage.value=`上链申请 ${data.id} 已发送，等待患者确认；数据指纹 ${shortHash(data.record_hash)}`;resetRecord();await loadDrafts();page.value='drafts'}catch(e){uploadMessage.value=e.message}finally{loading.value=false}}
function resetRecord(){record.value={patient_id:'',department:session.value?.department||'',diagnosis:'',type:'门诊记录',notes:''};selectedFiles.value=[]}
function selectFiles(event){selectedFiles.value=Array.from(event.target.files||[])}
function formatSize(size){return size<1024?`${size} B`:size<1048576?`${(size/1024).toFixed(1)} KB`:`${(size/1048576).toFixed(1)} MB`}
async function openConsents(){page.value='consents';doctors.value=await api('/portal/doctors');consents.value=await api('/portal/consents')}
function activeConsent(username){const item=consents.value.find(c=>c.doctor===username);return item?.active&&new Date(item.expires_at)>new Date()}
async function grantConsent(username){const expires=new Date(Date.now()+30*86400000).toISOString();await api('/portal/consents',{method:'POST',body:JSON.stringify({doctor:username,expires_at:expires})});await openConsents()}
async function revokeConsent(username){await api(`/portal/consents/${encodeURIComponent(username)}`,{method:'DELETE'});await openConsents()}
async function openDrafts(){page.value='drafts';await loadDrafts()}
async function loadDrafts(){if(!session.value||session.value.role==='admin'){drafts.value=[];return}try{drafts.value=await api('/portal/record-drafts')}catch{drafts.value=[]}}
async function approveDraft(id){try{await api(`/portal/record-drafts/${id}/approve`,{method:'POST',body:'{}'});uploadMessage.value='已确认该病历内容，同意提交联盟链';selectedDraft.value=null;await loadDrafts()}catch(e){uploadMessage.value=e.message}}
async function rejectDraft({id,reason}){try{await api(`/portal/record-drafts/${id}/reject`,{method:'POST',body:JSON.stringify({reason})});uploadMessage.value='已拒绝本次上链申请';selectedDraft.value=null;await loadDrafts()}catch(e){uploadMessage.value=e.message}}
async function submitDraft(id){loading.value=true;try{const data=await api(`/portal/record-drafts/${id}/submit`,{method:'POST',body:'{}'});uploadMessage.value=`患者授权已核验，病历已提交共识：${data.certificate_id}`;selectedDraft.value=null;await loadDrafts();setTimeout(loadRecords,700)}catch(e){uploadMessage.value=e.message}finally{loading.value=false}}
async function verifyAttachment(file){verification.value={...verification.value,[file.id]:await api(`/portal/attachments/${file.id}/verify`)}}
async function correctRecord(update){loading.value=true;try{const data=await api(`/portal/records/${update.index}/correct`,{method:'POST',body:JSON.stringify(update)});selectedRecord.value=null;uploadMessage.value=`更正草稿 ${data.id} 已发送，等待患者重新确认`;await loadDrafts();page.value='drafts'}catch(e){uploadMessage.value=e.message}finally{loading.value=false}}
async function openAccounts(){page.value='accounts';uploadMessage.value='';try{users.value=await api('/auth/users')}catch{users.value=[]}}
async function createDoctor(){try{const data=await api('/auth/doctors',{method:'POST',body:JSON.stringify(doctor.value)});doctor.value={username:'',password:'',real_name:'',license_no:'',hospital:'',department:''};users.value=await api('/auth/users');uploadMessage.value=`实名医生 ${data.real_name} 已认证并创建`}catch(e){uploadMessage.value=e.message}}
function draftStatus(status){return {pending_patient_approval:'等待患者确认',approved:'患者已同意',rejected:'患者已拒绝',submitted:'已提交上链'}[status]||status}
function shortHash(hash){return hash?`${hash.slice(0,10)}...${hash.slice(-8)}`:'--'}
function formatDateTime(value){return value?new Date(value).toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'}):'--'}
onMounted(async()=>{try{const me=await api('/auth/me');session.value=me.authenticated?me:null;if(session.value){record.value.department=me.department||'';await Promise.all([loadRecords(),loadDrafts()])}}catch{session.value=null}})
</script>
