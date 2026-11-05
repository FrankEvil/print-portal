<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api'
import { toast } from '../toast'
import type { AdminPrinter, Device, Model, PPDOption } from '../types'
import StatusBadge from '../components/StatusBadge.vue'
import SearchableSelect from '../components/SearchableSelect.vue'

const printers = ref<AdminPrinter[]>([])
const optionsFor = ref('')
const ppdOptions = ref<PPDOption[]>([])
const optionValues = ref<Record<string, string>>({})

// Create wizard
const showWizard = ref(false)
const step = ref(1)
const devices = ref<Device[]>([])
const scanning = ref<{ network: boolean; usb: boolean; all: boolean }>({ network: false, usb: false, all: false })
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
const rowMenu = ref<{ printer: AdminPrinter; top: number; left: number } | null>(null)

const brands = computed(() => {
  const set = new Set<string>()
  for (const m of models.value) if (m.brand) set.add(m.brand)
  return Array.from(set).sort((a, b) => a.localeCompare(b))
})

const visibleDevices = computed(() => devices.value)

const filteredModels = computed(() => {
  let list = models.value
  if (selectedBrand.value) list = list.filter((m) => m.brand === selectedBrand.value)
  return list
})

const modelSelectOptions = computed(() =>
  filteredModels.value.map((m) => ({ value: m.name, label: m.description, hint: m.brand })),
)

const selectedModelLabel = computed(() => {
  const match = models.value.find((m) => m.name === selectedModel.value)
  return match?.description || selectedModel.value
})

const deviceBrand = computed(() => {
  const device = selectedDevice.value
  if (!device) return ''
  if (device.brand) return device.brand
  const name = (device.model || device.name || '').trim()
  const first = name.split(/\s+/)[0] || ''
  return first === 'AirPrint' ? '' : first
})

const brandMissing = computed(() => {
  return !!deviceBrand.value && models.value.length > 0 && !brands.value.includes(deviceBrand.value)
})

// Single warning area above the brand/model selectors.
const driverWarning = computed(() => {
  if (useEverywhere.value) return ''
  if (brandMissing.value) {
    return `当前驱动库没有 ${deviceBrand.value} 品牌的驱动。请上传对应 PPD / 驱动包，或对支持 IPP Everywhere 的设备勾选免驱动。`
  }
  if (!models.value.length) return ''
  if (!modelSelectOptions.value.length) {
    return '当前品牌没有可用型号，请更换品牌，或上传 PPD / 驱动包。'
  }
  if (!selectedModel.value) {
    return '未自动匹配到驱动，请手动选择型号；找不到时请上传 PPD / 驱动包，或使用 IPP Everywhere。'
  }
  return ''
})

const editModelOptions = computed(() => {
  let list = models.value
  if (editBrand.value) list = list.filter((m) => m.brand === editBrand.value)
  return list.slice(0, 500)
})

const editCurrentDriverLabel = computed(() => {
  const match = models.value.find((m) => m.name === editForm.value.model)
  return match?.description || editForm.value.model || '—'
})

const editModelSelectOptions = computed(() => {
  const list = editModelOptions.value.map((m) => ({ value: m.name, label: m.description, hint: m.brand }))
  if (editForm.value.model && !list.some((o) => o.value === editForm.value.model)) {
    list.unshift({ value: editForm.value.model, label: `当前：${editCurrentDriverLabel.value}`, hint: '当前驱动' })
  }
  return list
})

function onWizardBrandChange() {
  if (selectedModel.value && !filteredModels.value.some((m) => m.name === selectedModel.value)) {
    selectedModel.value = ''
  }
  autoSelectDriver()
}

function onEditBrandChange() {
  if (editForm.value.model && !editModelOptions.value.some((m) => m.name === editForm.value.model)) {
    editForm.value.model = ''
  }
}

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
    autoSelectDriver()
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    modelsLoading.value = false
  }
}

type ScanScope = 'network' | 'usb' | 'all'

async function startScan(scope: ScanScope, after?: () => void, silent = false) {
  if (scanning.value[scope]) return
  scanning.value[scope] = true
  try {
    const res = await api.post<{ scan_id: string }>('/api/admin/discovery/scan', { scope })
    pollScan(scope, res.scan_id, after, silent)
  } catch (e) {
    scanning.value[scope] = false
    toast.error((e as Error).message)
    after?.()
  }
}

function pollScan(scope: ScanScope, scanID: string, after?: () => void, silent = false) {
  const timer = window.setInterval(async () => {
    try {
      const res = await api.get<{ status: string; devices: Device[] | null; error?: string }>(`/api/admin/discovery/scan/${scanID}`)
      const list = res.devices ?? []
      if (list.length) mergeDevices(list)
      if (res.status === 'done' || res.status === 'failed') {
        clearInterval(timer)
        scanning.value[scope] = false
        if (res.status === 'done') {
          reconcileScope(scope, list)
          if (!silent) toast.success(`扫描完成，发现 ${list.length} 个设备`)
        } else {
          toast.error(res.error || '扫描失败')
        }
        after?.()
      }
    } catch (e) {
      clearInterval(timer)
      scanning.value[scope] = false
      toast.error((e as Error).message)
      after?.()
    }
  }, 800)
}

function mergeDevices(list: Device[]) {
  const byURI = new Map<string, Device>()
  for (const d of devices.value) byURI.set(d.device_uri, d)
  for (const d of list) byURI.set(d.device_uri, d)
  devices.value = Array.from(byURI.values())
}

function isUSBDevice(d: Device) {
  return d.device_uri.startsWith('usb://')
}

// Keep devices from the other scan scope; replace this scope with the latest
// result so devices that went offline disappear after a completed scan.
function reconcileScope(scope: ScanScope, list: Device[]) {
  const inScope = (d: Device) => (scope === 'all' ? true : scope === 'usb' ? isUSBDevice(d) : !isUSBDevice(d))
  const others = devices.value.filter((d) => !inScope(d))
  const byURI = new Map<string, Device>()
  for (const d of list) byURI.set(d.device_uri, d)
  devices.value = [...others, ...Array.from(byURI.values())]
  if (selectedDevice.value && inScope(selectedDevice.value) && !byURI.has(selectedDevice.value.device_uri)) {
    selectedDevice.value = null
    toast.info('之前选择的设备已不在扫描结果中')
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
  useEverywhere.value = false
  wizardForm.value = { name: '', ppd_queue: '', raw_queue: '', is_default: false, is_shared: true }
  models.value = []
  modelTotal.value = 0
  // Local USB scan is fast; start LAN mDNS scan after it finishes.
  // Automatic scans stay silent; the device list itself is the feedback.
  startScan('usb', () => {
    if (showWizard.value) startScan('network', undefined, true)
  }, true)
}

function chooseDevice(d: Device) {
  selectedDevice.value = d
  manualMode.value = ''
  selectedModel.value = ''
  if (d.brand) selectedBrand.value = d.brand
  wizardForm.value.name = d.model || d.name || 'Printer'
  fillQueueNames()
  autoSelectDriver()
}

function normalizeDriverText(value: string) {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, ' ').trim()
}

// Find a matching PPD for the selected device, so the user doesn't have to
// search manually every time. Falls back to an explicit warning when nothing
// matches.
function autoSelectDriver() {
  const device = selectedDevice.value
  if (!device || selectedModel.value || !models.value.length) return
  const target = normalizeDriverText(device.model || device.name)
  if (!target) return
  let candidates = models.value
  if (deviceBrand.value) {
    const sameBrand = candidates.filter((m) => m.brand.toLowerCase() === deviceBrand.value.toLowerCase())
    if (sameBrand.length) candidates = sameBrand
  }
  let best: Model | null = null
  for (const model of candidates) {
    const desc = normalizeDriverText(model.description)
    if (desc === target) {
      best = model
      break
    }
    if (!best && (desc.includes(target) || target.includes(desc))) best = model
  }
  if (best) {
    selectedModel.value = best.name
    selectedBrand.value = best.brand
  }
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

function toggleRowMenu(p: AdminPrinter, event: MouseEvent) {
  if (rowMenu.value?.printer.id === p.id) {
    rowMenu.value = null
    return
  }
  const button = event.currentTarget as HTMLElement
  const rect = button.getBoundingClientRect()
  const menuWidth = 180
  const menuHeight = 160
  const left = Math.max(8, Math.min(rect.right - menuWidth, window.innerWidth - menuWidth - 8))
  const top = rect.bottom + menuHeight > window.innerHeight ? rect.top - menuHeight - 6 : rect.bottom + 6
  rowMenu.value = { printer: p, top, left }
}

function closeRowMenu(event: MouseEvent) {
  const target = event.target as Element
  if (target.closest('.row-menu') || target.closest('[data-row-menu]')) return
  rowMenu.value = null
}

function menuLoadOptions() {
  const p = rowMenu.value?.printer
  rowMenu.value = null
  if (p) loadOptions(p)
}

async function menuPurge() {
  const p = rowMenu.value?.printer
  rowMenu.value = null
  if (p) await action(p, 'purge')
}

async function menuRemove() {
  const p = rowMenu.value?.printer
  rowMenu.value = null
  if (p) await remove(p)
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

function prettyModel(p: AdminPrinter) {
  if (p.model === 'everywhere') return 'IPP Everywhere（免驱动）'
  const match = models.value.find((m) => m.name === p.model)
  if (match?.description) return match.description
  const base = (p.model.split('/').pop() || p.model).replace(/\.(gz|ppd)$/i, '')
  return base.replace(/\s+CUPS$/i, '') || '—'
}

function prettyDevice(uri: string) {
  if (uri.startsWith('usb://')) {
    return 'USB · ' + decodeURIComponent(uri.slice(6).split('?')[0]).replace('/', ' ')
  }
  if (uri.startsWith('dnssd://')) {
    const raw = decodeURIComponent(uri.slice(8)).split('/')[0].split('._')[0]
    return 'Bonjour · ' + raw.replace(/-/g, ' ').replace(/\s+@\s+/g, ' @ ')
  }
  if (uri.startsWith('ipp://') || uri.startsWith('ipps://')) {
    try {
      const parsed = new URL(uri)
      return (uri.startsWith('ipps://') ? 'IPPS · ' : 'IPP · ') + parsed.hostname
    } catch {
      /* fall through */
    }
  }
  if (uri.startsWith('socket://')) return 'TCP/IP · ' + uri.slice(9)
  if (uri.startsWith('lpd://')) return 'LPD · ' + uri.slice(6)
  return uri.length > 60 ? uri.slice(0, 60) + '…' : uri
}

onMounted(() => {
  load()
  loadModels()
  document.addEventListener('click', closeRowMenu)
})

onUnmounted(() => {
  document.removeEventListener('click', closeRowMenu)
})
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
          <tr><th>名称</th><th>队列</th><th>设备</th><th>状态</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="p in printers" :key="p.id">
            <td class="printer-name-cell">
              <strong>{{ p.name }}</strong>
              <div class="model-label">{{ prettyModel(p) }}<span v-if="p.is_default"> · 默认</span></div>
            </td>
            <td class="queue-cell">
              <div class="queue-line"><span class="queue-tag is-ppd">PPD</span><span class="queue-name">{{ p.ppd_queue }}</span></div>
              <div class="queue-line"><span class="queue-tag is-raw">RAW</span><span class="queue-name">{{ p.raw_queue }}</span></div>
            </td>
            <td><div class="device-summary" :title="p.device_uri">{{ prettyDevice(p.device_uri) }}</div></td>
            <td>
              <div class="status-cell">
                <StatusBadge :status="p.state" />
                <span class="queue-count">队列 {{ p.queue_length }}</span>
              </div>
            </td>
            <td>
              <div class="row-actions">
                <button class="btn btn-ghost btn-sm" @click="openEdit(p)">编辑</button>
                <button class="btn btn-ghost btn-sm" @click="action(p, 'test')">测试页</button>
                <button v-if="p.state === 'stopped'" class="btn btn-ghost btn-sm" @click="action(p, 'resume')">恢复</button>
                <button v-else class="btn btn-ghost btn-sm" @click="action(p, 'pause')">暂停</button>
                <button v-if="p.accepting" class="btn btn-ghost btn-sm" @click="action(p, 'reject')">拒绝任务</button>
                <button v-else class="btn btn-ghost btn-sm" @click="action(p, 'accept')">接受任务</button>
                <button class="row-menu-trigger" data-row-menu title="更多操作" @click.stop="toggleRowMenu(p, $event)">⋮</button>
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
              <button class="btn btn-ghost btn-sm" :disabled="scanning.network" title="只扫描局域网 IPP/AirPrint 设备" @click="startScan('network')">
                {{ scanning.network ? '局域网扫描中…' : '局域网扫描' }}
              </button>
              <button class="btn btn-ghost btn-sm" :disabled="scanning.all" title="扫描局域网和本地/USB 设备" @click="startScan('all')">
                {{ scanning.all ? '扫描中…' : '扫描' }}
              </button>
            </div>
          </div>

          <div v-if="scanning.network || scanning.usb || scanning.all" class="admin-sub" style="margin-bottom: 10px">
            正在扫描，发现设备会自动加入列表…
          </div>
          <div v-if="!visibleDevices.length && !scanning.network && !scanning.usb && !scanning.all" class="empty">未发现设备，请检查打印机是否连接并开启</div>
          <div v-else-if="visibleDevices.length" class="device-grid">
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
            <p v-if="driverWarning" class="driver-warning">
              {{ driverWarning }}
            </p>
            <div class="driver-filter-grid" :class="{ disabled: useEverywhere }">
              <div class="field">
                <label>品牌</label>
                <select v-model="selectedBrand" class="select" :disabled="useEverywhere" @change="onWizardBrandChange">
                  <option value="">全部品牌</option>
                  <option v-for="b in brands" :key="b" :value="b">{{ b }}</option>
                </select>
              </div>
              <div class="field">
                <label>型号</label>
                <SearchableSelect v-model="selectedModel" :options="modelSelectOptions" placeholder="搜索并选择型号" :disabled="useEverywhere" />
              </div>
            </div>
            <div v-if="selectedModel" class="selected-driver">
              已选择：<strong>{{ selectedModelLabel }}</strong>
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
          <p class="card-desc" style="margin-bottom: 10px">当前驱动：{{ editCurrentDriverLabel }}</p>
          <div class="driver-filter-grid">
            <div class="field">
              <label>品牌</label>
              <select v-model="editBrand" class="select" @change="onEditBrandChange">
                <option value="">全部品牌</option>
                <option v-for="b in brands" :key="b" :value="b">{{ b }}</option>
              </select>
            </div>
            <div class="field">
              <label>型号</label>
              <SearchableSelect v-model="editForm.model" :options="editModelSelectOptions" placeholder="搜索并选择型号" />
            </div>
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

  <!-- Row action dropdown (teleported so the table cannot clip it) -->
  <Teleport to="body">
    <div
      v-if="rowMenu"
      class="row-menu"
      :style="{ top: rowMenu.top + 'px', left: rowMenu.left + 'px' }"
      @click.stop
    >
      <button class="row-menu-item" @click="menuLoadOptions">PPD 默认选项</button>
      <button class="row-menu-item" @click="menuPurge">清空队列</button>
      <div class="row-menu-divider"></div>
      <button class="row-menu-item is-danger" @click="menuRemove">删除打印机</button>
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
