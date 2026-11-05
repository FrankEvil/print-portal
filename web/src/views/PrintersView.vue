<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'
import { toast } from '../toast'
import type { AdminPrinter, Device, Model, PPDOption } from '../types'
import StatusBadge from '../components/StatusBadge.vue'

const printers = ref<AdminPrinter[]>([])
const optionsFor = ref('')
const ppdOptions = ref<PPDOption[]>([])
const optionValues = ref<Record<string, string>>({})

// Create wizard
const showWizard = ref(false)
const step = ref(1)
const devices = ref<Device[]>([])
const scanning = ref(false)
const deviceFilter = ref<'all' | 'network' | 'usb'>('all')
const selectedDevice = ref<Device | null>(null)
const manualMode = ref<'' | 'tcp' | 'uri'>('')
const tcpIp = ref('')
const tcpPort = ref(9100)
const manualUri = ref('')
const models = ref<Model[]>([])
const modelTotal = ref(0)
const modelsLoading = ref(false)
const selectedBrand = ref('')
const selectedModel = ref('')
const modelSearch = ref('')
const useEverywhere = ref(false)
const uploading = ref(false)
const driverInput = ref<HTMLInputElement | null>(null)
const saving = ref(false)
const wizardForm = ref({ name: '', ppd_queue: '', raw_queue: '', is_default: false, is_shared: true })

// Edit dialog: name, driver, default/share only
const editingPrinter = ref<AdminPrinter | null>(null)
const editSaving = ref(false)
const editUploading = ref(false)
const editDriverInput = ref<HTMLInputElement | null>(null)
const editForm = ref({ name: '', model: '', is_default: false, is_shared: true })
const editBrand = ref('')
const editSearch = ref('')

const brands = computed(() => {
  const set = new Set<string>()
  for (const m of models.value) if (m.brand) set.add(m.brand)
  return Array.from(set).sort((a, b) => a.localeCompare(b))
})

const visibleDevices = computed(() => {
  if (deviceFilter.value === 'usb') return devices.value.filter((d) => d.device_uri.startsWith('usb://'))
  if (deviceFilter.value === 'network') return devices.value.filter((d) => !d.device_uri.startsWith('usb://'))
  return devices.value
})

const filteredModels = computed(() => {
  let list = models.value
  if (selectedBrand.value) list = list.filter((m) => m.brand === selectedBrand.value)
  const q = modelSearch.value.trim().toLowerCase()
  if (q) list = list.filter((m) => `${m.brand} ${m.description} ${m.name}`.toLowerCase().includes(q))
  return list
})

const brandMissing = computed(() => {
  const brand = selectedDevice.value?.brand
  return !!brand && models.value.length > 0 && !brands.value.includes(brand)
})

const editModelOptions = computed(() => {
  let list = models.value
  if (editBrand.value) list = list.filter((m) => m.brand === editBrand.value)
  const q = editSearch.value.trim().toLowerCase()
  if (q) list = list.filter((m) => `${m.brand} ${m.description} ${m.name}`.toLowerCase().includes(q))
  return list.slice(0, 500)
})

const editModelMissing = computed(() =>
  !!editForm.value.model && !editModelOptions.value.some((m) => m.name === editForm.value.model),
)

const isNetworkDevice = computed(() => {
  const uri = selectedDevice.value?.device_uri || ''
  return uri.startsWith('ipp://') || uri.startsWith('ipps://') || uri.startsWith('dnssd://')
})

async function load() {
  try {
    const res = await api.get<{ printers: AdminPrinter[] }>('/api/admin/printers')
    printers.value = res.printers
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function loadModels() {
  modelsLoading.value = true
  try {
    const res = await api.get<{ models: Model[]; total: number }>('/api/admin/drivers/models?limit=5000')
    models.value = res.models
    modelTotal.value = res.total
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    modelsLoading.value = false
  }
}

async function scanDevices(filter: 'all' | 'network' | 'usb') {
  scanning.value = true
  deviceFilter.value = filter
  try {
    const res = await api.get<{ devices: Device[] }>('/api/admin/discovery/printers')
    devices.value = res.devices
    toast.success(`发现 ${res.devices.length} 个设备`)
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    scanning.value = false
  }
}

function openWizard() {
  editingPrinter.value = null
  showWizard.value = true
  step.value = 1
  devices.value = []
  selectedDevice.value = null
  manualMode.value = ''
  tcpIp.value = ''
  tcpPort.value = 9100
  manualUri.value = ''
  selectedBrand.value = ''
  selectedModel.value = ''
  modelSearch.value = ''
  useEverywhere.value = false
  wizardForm.value = { name: '', ppd_queue: '', raw_queue: '', is_default: false, is_shared: true }
  models.value = []
  modelTotal.value = 0
  scanDevices('all')
}

function chooseDevice(d: Device) {
  selectedDevice.value = d
  manualMode.value = ''
  if (d.brand) selectedBrand.value = d.brand
  if (d.model) {
    const match = models.value.find((m) => m.description.toLowerCase() === d.model.toLowerCase() || m.description.toLowerCase().includes(d.model.toLowerCase()))
    if (match) {
      selectedModel.value = match.name
      selectedBrand.value = match.brand
    }
  }
  wizardForm.value.name = d.model || d.name || 'Printer'
  fillQueueNames()
}

function applyManualDevice() {
  if (manualMode.value === 'tcp') {
    if (!tcpIp.value.trim()) return toast.error('请输入打印机 IP 地址')
    const uri = `socket://${tcpIp.value.trim()}:${tcpPort.value || 9100}`
    selectedDevice.value = { id: 0, kind: 'printer', name: `TCP/IP ${tcpIp.value.trim()}`, model: '', brand: '', device_uri: uri, ip: tcpIp.value.trim(), source: 'network', metadata: '', last_seen: '' }
    wizardForm.value.name = `TCP/IP ${tcpIp.value.trim()}`
    fillQueueNames()
    return true
  }
  if (manualMode.value === 'uri') {
    if (!manualUri.value.trim()) return toast.error('请输入设备 URI')
    selectedDevice.value = { id: 0, kind: 'printer', name: 'Manual Printer', model: '', brand: '', device_uri: manualUri.value.trim(), ip: '', source: 'network', metadata: '', last_seen: '' }
    wizardForm.value.name = 'Manual Printer'
    fillQueueNames()
    return true
  }
  return false
}

function nextFromDevice() {
  if (manualMode.value) {
    if (!applyManualDevice()) return
    step.value = 2
    loadModelsIfNeeded()
    return
  }
  if (!selectedDevice.value) return toast.error('请选择设备或填写手动地址')
  step.value = 2
  loadModelsIfNeeded()
}

function loadModelsIfNeeded() {
  if (!models.value.length && !modelsLoading.value) loadModels()
}

function nextFromDriver() {
  if (!useEverywhere.value && !selectedModel.value) return toast.error('请选择驱动 / 型号')
  step.value = 3
  if (!wizardForm.value.name) wizardForm.value.name = selectedDevice.value?.model || selectedDevice.value?.name || 'Printer'
  fillQueueNames()
}

function fillQueueNames() {
  const base = slugify(wizardForm.value.name)
  if (!wizardForm.value.ppd_queue) wizardForm.value.ppd_queue = `${base}-ppd`
  if (!wizardForm.value.raw_queue) wizardForm.value.raw_queue = `${base}-raw`
}

function slugify(s: string) {
  return s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'printer'
}

async function uploadDriver(e: Event) {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  if (!f) return
  uploading.value = true
  try {
    const fd = new FormData()
    fd.append('file', f)
    await api.upload('/api/admin/drivers/install', fd)
    toast.success(`驱动 ${f.name} 已上传`)
    await loadModels()
  } catch (err) {
    toast.error((err as Error).message)
  } finally {
    uploading.value = false
    input.value = ''
  }
}

async function savePrinter() {
  if (!selectedDevice.value) return toast.error('设备信息丢失，请重新选择')
  if (!wizardForm.value.name.trim()) return toast.error('请输入打印机名称')
  if (!wizardForm.value.ppd_queue.trim() || !wizardForm.value.raw_queue.trim()) return toast.error('请填写队列名')
  saving.value = true
  try {
    await api.post('/api/admin/printers', {
      name: wizardForm.value.name.trim(),
      model: useEverywhere.value ? 'everywhere' : selectedModel.value,
      ppd_queue: wizardForm.value.ppd_queue.trim(),
      raw_queue: wizardForm.value.raw_queue.trim(),
      device_uri: selectedDevice.value.device_uri,
      is_default: wizardForm.value.is_default,
      is_shared: wizardForm.value.is_shared,
    })
    toast.success('打印机已创建')
    showWizard.value = false
    await load()
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    saving.value = false
  }
}

function syncEditBrand() {
  const p = editingPrinter.value
  if (!p) return
  const match = models.value.find((m) => m.name === p.model || m.description === p.model)
  editBrand.value = match?.brand || ''
}

function openEdit(p: AdminPrinter) {
  editingPrinter.value = p
  editForm.value = { name: p.name, model: p.model, is_default: p.is_default, is_shared: p.is_shared }
  editBrand.value = ''
  editSearch.value = ''
  if (!models.value.length) loadModels().then(syncEditBrand)
  else syncEditBrand()
}

async function saveEdit() {
  const p = editingPrinter.value
  if (!p) return
  if (!editForm.value.name.trim()) return toast.error('请输入打印机名称')
  if (!editForm.value.model) return toast.error('请选择驱动 / 型号')
  editSaving.value = true
  try {
    await api.put(`/api/admin/printers/${encodeURIComponent(p.name)}`, {
      name: editForm.value.name.trim(),
      model: editForm.value.model,
      ppd_queue: p.ppd_queue,
      raw_queue: p.raw_queue,
      device_uri: p.device_uri,
      is_default: editForm.value.is_default,
      is_shared: editForm.value.is_shared,
    })
    toast.success('打印机已更新')
    editingPrinter.value = null
    await load()
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    editSaving.value = false
  }
}

async function uploadEditDriver(e: Event) {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  if (!f) return
  editUploading.value = true
  try {
    const fd = new FormData()
    fd.append('file', f)
    await api.upload('/api/admin/drivers/install', fd)
    toast.success(`驱动 ${f.name} 已上传`)
    await loadModels()
    syncEditBrand()
  } catch (err) {
    toast.error((err as Error).message)
  } finally {
    editUploading.value = false
    input.value = ''
  }
}

async function remove(p: AdminPrinter) {
  if (!confirm(`确定删除打印机 ${p.name} 及其队列？`)) return
  try {
    await api.del(`/api/admin/printers/${encodeURIComponent(p.name)}`)
    toast.success('已删除')
    await load()
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function action(p: AdminPrinter, name: string) {
  if (name === 'purge' && !confirm('确定清空该打印机的所有任务？')) return
  try {
    const res = await api.post<{ cups_job_id?: string }>(`/api/admin/printers/${encodeURIComponent(p.name)}/${name}`)
    toast.success(name === 'test' ? `测试页已提交 ${res.cups_job_id || ''}` : '操作成功')
    await load()
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function loadOptions(p: AdminPrinter) {
  optionsFor.value = p.name
  try {
    const res = await api.get<{ options: PPDOption[] }>(`/api/admin/printers/${encodeURIComponent(p.name)}/ppd-options`)
    ppdOptions.value = res.options
    optionValues.value = {}
    for (const o of res.options) optionValues.value[o.keyword] = o.default
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function saveOptions() {
  if (!optionsFor.value) return
  try {
    await api.put(`/api/admin/printers/${encodeURIComponent(optionsFor.value)}/ppd-options`, { options: optionValues.value })
    toast.success('PPD 默认选项已保存')
  } catch (e) {
    toast.error((e as Error).message)
  }
}

onMounted(load)
</script>

<template>
  <div class="admin-header">
    <div>
      <h1 class="admin-title">打印机</h1>
      <p class="admin-sub">管理物理打印机、PPD / RAW 双队列与默认选项</p>
    </div>
    <button class="btn btn-primary" @click="openWizard">添加打印机</button>
  </div>

  <section class="card">
    <div v-if="!printers.length" class="empty">还没有打印机，点击右上角「添加打印机」开始</div>
    <div v-else class="table-wrap">
      <table class="table">
        <thead>
          <tr><th>名称</th><th>队列</th><th>设备地址</th><th>状态</th><th>队列长度</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="p in printers" :key="p.id">
            <td>
              <strong>{{ p.name }}</strong>
              <div class="admin-sub">{{ p.model || '—' }}{{ p.is_default ? ' · 默认' : '' }}</div>
            </td>
            <td>
              <div>{{ p.ppd_queue }} <span class="admin-sub">(PPD)</span></div>
              <div>{{ p.raw_queue }} <span class="admin-sub">(RAW)</span></div>
            </td>
            <td style="max-width: 260px; word-break: break-all">{{ p.device_uri }}</td>
            <td><StatusBadge :status="p.state" /></td>
            <td>{{ p.queue_length }}</td>
            <td>
              <div style="display: flex; flex-wrap: wrap; gap: 6px">
                <button class="btn btn-ghost btn-sm" @click="openEdit(p)">编辑</button>
                <button class="btn btn-ghost btn-sm" @click="action(p, 'test')">测试页</button>
                <button v-if="p.state === 'stopped'" class="btn btn-ghost btn-sm" @click="action(p, 'resume')">恢复</button>
                <button v-else class="btn btn-ghost btn-sm" @click="action(p, 'pause')">暂停</button>
                <button v-if="p.accepting" class="btn btn-ghost btn-sm" @click="action(p, 'reject')">拒绝任务</button>
                <button v-else class="btn btn-ghost btn-sm" @click="action(p, 'accept')">接受任务</button>
                <button class="btn btn-ghost btn-sm" @click="action(p, 'purge')">清空</button>
                <button class="btn btn-ghost btn-sm" @click="loadOptions(p)">PPD 选项</button>
                <button class="btn btn-danger btn-sm" @click="remove(p)">删除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>

  <!-- Create printer wizard -->
  <Teleport to="body">
    <div v-if="showWizard" class="modal-overlay" @click.self="showWizard = false">
      <div class="modal wizard-modal">
        <div class="modal-head">
          <h2 class="card-title">{{ editingPrinter ? '编辑打印机' : '添加打印机' }}</h2>
          <button class="modal-close" @click="showWizard = false">×</button>
        </div>

        <div class="wizard-steps">
          <div class="wizard-step" :class="{ active: step === 1, done: step > 1 }"><span>1</span>选择设备</div>
          <div class="wizard-step" :class="{ active: step === 2, done: step > 2 }"><span>2</span>选择驱动</div>
          <div class="wizard-step" :class="{ active: step === 3 }"><span>3</span>配置</div>
        </div>

        <!-- Step 1: device -->
        <div v-if="step === 1" class="wizard-body">
          <div class="wizard-section-head">
            <div>
              <h3 class="wizard-title">可用设备</h3>
              <p class="card-desc">自动发现局域网 IPP/AirPrint 与 USB 打印机</p>
            </div>
            <div style="display: flex; gap: 8px">
              <button class="btn btn-ghost btn-sm" :disabled="scanning" @click="scanDevices('network')">扫描局域网</button>
              <button class="btn btn-ghost btn-sm" :disabled="scanning" @click="scanDevices('usb')">扫描 USB</button>
              <button class="btn btn-ghost btn-sm" :disabled="scanning" @click="scanDevices('all')">{{ scanning ? '扫描中…' : '全部扫描' }}</button>
            </div>
          </div>

          <div v-if="scanning" class="empty">正在扫描设备…</div>
          <div v-else-if="!visibleDevices.length" class="empty">未发现设备，请检查打印机是否连接并开启</div>
          <div v-else class="device-grid">
            <label v-for="d in visibleDevices" :key="d.device_uri" class="device-card" :class="{ selected: selectedDevice?.device_uri === d.device_uri }" @click="chooseDevice(d)">
              <input type="radio" :checked="selectedDevice?.device_uri === d.device_uri" style="display: none" @click.stop="chooseDevice(d)" />
              <div class="device-icon">{{ d.device_uri.startsWith('usb://') ? 'USB' : 'LAN' }}</div>
              <div class="device-main">
                <div class="device-name">{{ d.model || d.name }}</div>
                <div class="device-sub">{{ d.device_uri }}</div>
              </div>
              <span class="status-badge is-neutral">{{ d.source === 'direct' ? 'USB' : '网络' }}</span>
            </label>
          </div>

          <div class="wizard-divider"></div>
          <div class="manual-section">
            <div class="manual-tabs">
              <button
                class="btn btn-sm"
                :class="manualMode === 'tcp' ? 'btn-primary' : 'btn-ghost'"
                @click="manualMode = manualMode === 'tcp' ? '' : 'tcp'"
              >
                Standard TCP/IP (RAW/9100)
              </button>
              <button
                class="btn btn-sm"
                :class="manualMode === 'uri' ? 'btn-primary' : 'btn-ghost'"
                @click="manualMode = manualMode === 'uri' ? '' : 'uri'"
              >
                手动输入设备 URI
              </button>
            </div>

            <div v-if="manualMode === 'tcp'" class="manual-panel">
              <div class="form-grid">
                <div class="field">
                  <label>打印机 IP 地址</label>
                  <input v-model="tcpIp" class="input" placeholder="例如：192.168.1.100" />
                </div>
                <div class="field">
                  <label>端口</label>
                  <input v-model.number="tcpPort" type="number" class="input" placeholder="9100" />
                </div>
              </div>
              <p class="admin-sub" style="margin-top: 10px">将生成地址：socket://{{ tcpIp || '打印机IP' }}:{{ tcpPort || 9100 }}</p>
            </div>

            <div v-if="manualMode === 'uri'" class="manual-panel">
              <div class="field">
                <label>设备 URI</label>
                <input v-model="manualUri" class="input" placeholder="usb://... / ipp://... / socket://..." />
              </div>
              <p class="admin-sub" style="margin-top: 10px">高级选项：可直接粘贴 CUPS 支持的 device-uri</p>
            </div>
          </div>
        </div>

        <!-- Step 2: driver -->
        <div v-else-if="step === 2" class="wizard-body">
          <div class="wizard-section-head">
            <div>
              <h3 class="wizard-title">选择驱动</h3>
              <p class="card-desc">已安装 {{ modelTotal }} 个驱动型号；按品牌、型号或文件名查找</p>
            </div>
            <button class="btn btn-ghost btn-sm" :disabled="uploading" @click="driverInput?.click()">
              {{ uploading ? '上传中…' : '上传新驱动' }}
            </button>
            <input ref="driverInput" type="file" accept=".ppd,.deb,.tar.gz,.tgz" style="display: none" @change="uploadDriver" />
          </div>

          <label v-if="isNetworkDevice" class="check-line">
            <input v-model="useEverywhere" type="checkbox" />
            使用 IPP Everywhere / AirPrint 免驱动
          </label>

          <div v-if="modelsLoading" class="empty">正在加载驱动型号…</div>
          <template v-else>
            <p v-if="brandMissing" class="card-desc" style="color: var(--warning); margin-bottom: 12px">
              当前驱动库没有 {{ selectedDevice?.brand }} 品牌的驱动。请上传对应 PPD / 驱动包，或对支持 IPP Everywhere 的设备勾选免驱动。
            </p>
            <div class="driver-filter-grid" :class="{ disabled: useEverywhere }">
              <div class="field">
                <label>品牌</label>
                <select v-model="selectedBrand" class="select" :disabled="useEverywhere">
                  <option value="">全部品牌</option>
                  <option v-for="b in brands" :key="b" :value="b">{{ b }}</option>
                </select>
              </div>
              <div class="field">
                <label>型号</label>
                <select v-model="selectedModel" class="select" :disabled="useEverywhere">
                  <option value="">请选择型号</option>
                  <option v-for="m in filteredModels.slice(0, 500)" :key="m.name" :value="m.name">{{ m.description }}</option>
                </select>
              </div>
            </div>
            <div class="field" style="margin-top: 12px">
              <label>搜索品牌、型号或文件名</label>
              <input v-model="modelSearch" class="input" placeholder="例如：Brother、DCP-7060、brlaser" :disabled="useEverywhere" />
            </div>
            <div v-if="!useEverywhere && !filteredModels.length" class="empty">没有匹配的驱动型号，尝试上传 PPD 或更换关键词</div>
            <div v-if="selectedModel" class="selected-driver">
              已选择：<strong>{{ selectedModel }}</strong>
            </div>
          </template>
        </div>

        <!-- Step 3: config -->
        <div v-else class="wizard-body">
          <div class="wizard-section-head">
            <h3 class="wizard-title">配置</h3>
          </div>
          <div class="form-grid">
            <div class="field full"><label>打印机名称</label><input v-model="wizardForm.name" class="input" @blur="fillQueueNames" /></div>
            <div class="field"><label>PPD 队列名</label><input v-model="wizardForm.ppd_queue" class="input" :disabled="!!editingPrinter" /></div>
            <div class="field"><label>RAW 队列名</label><input v-model="wizardForm.raw_queue" class="input" :disabled="!!editingPrinter" /></div>
            <div class="field full"><label>设备地址</label><input :value="selectedDevice?.device_uri" class="input" disabled /></div>
            <div class="field full"><label>驱动</label><input :value="useEverywhere ? 'IPP Everywhere（免驱动）' : selectedModel" class="input" disabled /></div>
            <div class="field"><label><input v-model="wizardForm.is_default" type="checkbox" /> 设为默认打印机</label></div>
            <div class="field"><label><input v-model="wizardForm.is_shared" type="checkbox" /> 局域网共享</label></div>
          </div>
        </div>

        <div class="modal-foot">
          <button v-if="step > 1" class="btn btn-ghost" @click="step--">上一步</button>
          <span style="flex: 1"></span>
          <button v-if="step === 1" class="btn btn-primary" @click="nextFromDevice">下一步</button>
          <button v-if="step === 2" class="btn btn-primary" @click="nextFromDriver">下一步</button>
          <button v-if="step === 3" class="btn btn-primary" :disabled="saving" @click="savePrinter">{{ saving ? '保存中…' : '保存' }}</button>
        </div>
      </div>
    </div>
  </Teleport>

  <!-- Edit dialog: only name, driver, default and shared are editable -->
  <Teleport to="body">
    <div v-if="editingPrinter" class="modal-overlay" @click.self="editingPrinter = null">
      <div class="modal edit-modal">
        <div class="modal-head">
          <div>
            <h2 class="card-title">编辑打印机</h2>
            <p class="card-desc">{{ editingPrinter.name }}</p>
          </div>
          <button class="modal-close" @click="editingPrinter = null">×</button>
        </div>

        <div class="edit-summary">
          <div class="edit-summary-row">
            <StatusBadge :status="editingPrinter.state" />
            <span>PPD 队列：{{ editingPrinter.ppd_queue }}</span>
            <span>RAW 队列：{{ editingPrinter.raw_queue }}</span>
          </div>
          <div><span class="admin-sub">设备地址：</span>{{ editingPrinter.device_uri }}</div>
        </div>

        <section class="edit-section">
          <h3 class="wizard-title">基本信息</h3>
          <div class="field" style="margin-top: 10px">
            <label>打印机名称</label>
            <input v-model="editForm.name" class="input" />
          </div>
        </section>

        <section class="edit-section">
          <div class="section-head" style="margin-bottom: 10px">
            <h3 class="wizard-title">驱动</h3>
            <button class="btn btn-ghost btn-sm" :disabled="editUploading" @click="editDriverInput?.click()">
              {{ editUploading ? '上传中…' : '上传新驱动' }}
            </button>
            <input ref="editDriverInput" type="file" accept=".ppd,.deb,.tar.gz,.tgz" style="display: none" @change="uploadEditDriver" />
          </div>
          <p class="card-desc" style="margin-bottom: 10px">当前驱动：{{ editingPrinter.model || '—' }}</p>
          <div class="driver-filter-grid">
            <div class="field">
              <label>品牌</label>
              <select v-model="editBrand" class="select">
                <option value="">全部品牌</option>
                <option v-for="b in brands" :key="b" :value="b">{{ b }}</option>
              </select>
            </div>
            <div class="field">
              <label>型号</label>
              <select v-model="editForm.model" class="select">
                <option v-if="editModelMissing" :value="editForm.model">
                  当前：{{ editForm.model }}
                </option>
                <option v-for="m in editModelOptions" :key="m.name" :value="m.name">{{ m.description }}</option>
              </select>
            </div>
          </div>
          <div class="field" style="margin-top: 12px">
            <label>搜索型号</label>
            <input v-model="editSearch" class="input" placeholder="品牌、型号或文件名" />
          </div>
        </section>

        <section class="edit-section">
          <h3 class="wizard-title" style="margin-bottom: 10px">选项</h3>
          <label class="check-line"><input v-model="editForm.is_default" type="checkbox" /> 设为默认打印机</label>
          <label class="check-line"><input v-model="editForm.is_shared" type="checkbox" /> 局域网共享</label>
        </section>

        <div class="modal-foot">
          <button class="btn btn-ghost" @click="editingPrinter = null">取消</button>
          <button class="btn btn-primary" :disabled="editSaving" @click="saveEdit">{{ editSaving ? '保存中…' : '保存' }}</button>
        </div>
      </div>
    </div>
  </Teleport>

  <section v-if="optionsFor" class="card" style="margin-top: 20px">
    <div class="section-head">
      <h2 class="card-title">PPD 默认选项 · {{ optionsFor }}</h2>
      <button class="btn btn-ghost btn-sm" @click="optionsFor = ''">关闭</button>
    </div>
    <div v-if="!ppdOptions.length" class="empty">该打印机 PPD 没有可配置选项，或 CUPS 未返回选项</div>
    <div v-else class="form-grid">
      <div v-for="o in ppdOptions" :key="o.keyword" class="field">
        <label>{{ o.text }}（{{ o.keyword }}）</label>
        <select v-model="optionValues[o.keyword]" class="select">
          <option v-for="c in o.choices" :key="c" :value="c">{{ c }}</option>
        </select>
      </div>
    </div>
    <button v-if="ppdOptions.length" class="btn btn-primary" style="margin-top: 16px" @click="saveOptions">保存默认选项</button>
  </section>
</template>
