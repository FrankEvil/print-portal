<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { TrimApp } from '@trimjs/web-app'
import { api, apiUrl } from '../api'
import { toast } from '../toast'
import type { Job, PPDOption, Printer } from '../types'
import { formatJobParams } from '../jobDisplay'
import StatusBadge from '../components/StatusBadge.vue'

const printers = ref<Printer[]>([])
const selectedPrinter = ref<number | null>(null)
const ppdOptions = ref<PPDOption[]>([])
const file = ref<File | null>(null)
const nasPath = ref('')
const nasPicking = ref(false)
const authMode = ref<'token' | 'fnos'>('token')
const fileUrl = ref('')
const dragging = ref(false)
const submitting = ref(false)
const jobs = ref<Job[]>([])
const pageCount = ref<number | null>(null)
const pageCountLoading = ref(false)

const form = ref({
  page_range: '',
  duplex: 'one-sided',
  copies: 1,
  paper_size: 'A4',
  orientation: 'portrait',
  color_mode: 'monochrome',
  scaling: 'auto',
})

let pollTimer: number | undefined

const nasName = computed(() => nasPath.value.split(/[\\/]/).pop() || '')
const nasPreviewUrl = computed(() => nasPath.value ? apiUrl(`/api/nas/preview?path=${encodeURIComponent(nasPath.value)}`) : '')
const isNasPdf = computed(() => nasPath.value.toLowerCase().endsWith('.pdf'))
const isNasImage = computed(() => ['.jpg', '.jpeg', '.png', '.webp'].some((ext) => nasPath.value.toLowerCase().endsWith(ext)))
const isPdf = computed(() => file.value?.name.toLowerCase().endsWith('.pdf'))
const showPageRange = computed(() => !isImage.value && !isNasImage.value)
const isImage = computed(() => {
  const n = file.value?.name.toLowerCase() || ''
  return ['.jpg', '.jpeg', '.png', '.webp'].some((ext) => n.endsWith(ext))
})
const hasPrinters = computed(() => printers.value.length > 0)

const paperChoices = computed(() => {
  const opt = ppdOptions.value.find((o) => o.keyword === 'PageSize' || /page size/i.test(o.text))
  if (opt && opt.choices.length) return opt.choices
  return ['A4', 'A5', 'Letter']
})

const supportsColor = computed(() => ppdOptions.value.some((o) => /color/i.test(o.keyword) || /color/i.test(o.text)))

const pageRangePlaceholder = computed(() => {
  if (pageCount.value) return `全部（共 ${pageCount.value} 页），如 1-${Math.min(pageCount.value, 2)}`
  return '全部，如 1-2 或 1-5 8'
})

async function loadPPDOptions() {
  if (!selectedPrinter.value) return
  try {
    const res = await api.get<{ options: PPDOption[] }>(`/api/printers/${selectedPrinter.value}/ppd-options`)
    ppdOptions.value = res.options
    if (!supportsColor.value) form.value.color_mode = 'monochrome'
    if (!paperChoices.value.includes(form.value.paper_size)) form.value.paper_size = paperChoices.value[0] || 'A4'
  } catch {
    ppdOptions.value = []
  }
}

watch(selectedPrinter, loadPPDOptions)

async function loadPrinters() {
  try {
    const res = await api.get<{ printers: Printer[] }>('/api/printers')
    printers.value = res.printers
    if (!selectedPrinter.value && res.printers.length) {
      const def = res.printers.find((p) => p.is_default) || res.printers[0]
      selectedPrinter.value = def.id
    }
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function loadJobs() {
  try {
    const res = await api.get<{ jobs: Job[] }>('/api/jobs?limit=8')
    jobs.value = res.jobs
    syncPolling()
  } catch {
    /* silent */
  }
}

function syncPolling() {
  const active = jobs.value.some((j) =>
    ['pending', 'converting', 'submitted', 'queued', 'printing', 'held'].includes(j.status),
  )
  if (active && !pollTimer) pollTimer = window.setInterval(loadJobs, 2000)
  if (!active && pollTimer) {
    clearInterval(pollTimer)
    pollTimer = undefined
  }
}

let pageCountToken = 0

async function setFile(f: File | null) {
  if (fileUrl.value) URL.revokeObjectURL(fileUrl.value)
  if (f) nasPath.value = ''
  file.value = f
  if (f && isImage.value) form.value.page_range = ''
  fileUrl.value = f ? URL.createObjectURL(f) : ''
  pageCount.value = null
  const token = ++pageCountToken
  if (!f) return
  if (isImage.value) {
    pageCount.value = 1
    return
  }
  if (!f.name.toLowerCase().endsWith('.pdf')) return
  pageCountLoading.value = true
  try {
    const { PDFDocument } = await import('pdf-lib')
    const doc = await PDFDocument.load(await f.arrayBuffer(), { ignoreEncryption: true })
    if (token === pageCountToken) pageCount.value = doc.getPageCount()
  } catch {
    if (token === pageCountToken) pageCount.value = null
  } finally {
    if (token === pageCountToken) pageCountLoading.value = false
  }
}

function clearSource() {
  nasPath.value = ''
  void setFile(null)
}

async function pickNasFile() {
  nasPicking.value = true
  toast.info('正在打开 NAS 文件选择器…')
  try {
    const sdk = new TrimApp()
    console.log('[PrintPortal] TrimApp', { isWeb: sdk.isWeb, isStandaloneWeb: sdk.isStandaloneWeb })
    if (sdk.isStandaloneWeb) {
      toast.error('请从飞牛应用中心打开后选择 NAS 文件')
      return
    }
    const result = await sdk.pickUserFile({
      directory: false,
      multiple: false,
      title: '选择要打印的文件',
      okText: '选择',
      accept: ['.pdf', '.jpg', '.jpeg', '.png', '.webp', '.doc', '.docx', '.xls', '.xlsx', '.ppt', '.pptx', '.ps'],
      sidebarGroup: ['myFiles', 'otherShare', 'favorites'],
    })
    console.log('[PrintPortal] pickUserFile result', result)
    const path = result?.data?.[0]
    if (path) {
      nasPath.value = path
      file.value = null
      if (fileUrl.value) URL.revokeObjectURL(fileUrl.value)
      fileUrl.value = ''
      pageCount.value = null
      toast.success('已从 NAS 选择文件')
      if (isNasPdf.value) void loadNasPageCount()
      else if (isNasImage.value) {
        pageCount.value = 1
        form.value.page_range = ''
      }
    } else {
      toast.error(`未选择文件${result?.msg ? `：${result.msg}` : ''}`)
    }
  } catch (e) {
    console.error('[PrintPortal] pickUserFile failed', e)
    toast.error((e as Error).message || '选择 NAS 文件失败')
  } finally {
    nasPicking.value = false
  }
}

async function loadNasPageCount() {
  pageCountLoading.value = true
  const token = ++pageCountToken
  try {
    const res = await fetch(nasPreviewUrl.value)
    if (!res.ok) return
    const { PDFDocument } = await import('pdf-lib')
    const doc = await PDFDocument.load(await res.arrayBuffer(), { ignoreEncryption: true })
    if (token === pageCountToken) pageCount.value = doc.getPageCount()
  } catch {
    if (token === pageCountToken) pageCount.value = null
  } finally {
    if (token === pageCountToken) pageCountLoading.value = false
  }
}

async function loadAuthMode() {
  try {
    const cfg = await api.get<{ mode: 'token' | 'fnos' }>('/api/auth/config')
    authMode.value = cfg.mode
  } catch {
    /* default token */
  }
}

function onFileChange(e: Event) {
  const input = e.target as HTMLInputElement
  if (input.files?.length) void setFile(input.files[0])
  input.value = ''
}

function onDrop(e: DragEvent) {
  dragging.value = false
  const f = e.dataTransfer?.files?.[0]
  if (f) void setFile(f)
}

function validatePageRange(range: string, total: number): string {
  const text = range.trim()
  if (!text) return ''
  const tokens = text.split(/[\s,]+/).filter(Boolean)
  for (const token of tokens) {
    const m = token.match(/^(\d+)(?:-(\d+))?$/)
    if (!m) return '页码格式不正确，请使用 1-2 或 1-5 8'
    const start = Number(m[1])
    const end = m[2] ? Number(m[2]) : start
    if (start < 1 || end < start || end > total) {
      return `页码范围不能超过文件页数（共 ${total} 页）`
    }
  }
  return ''
}

async function submit() {
  if (!file.value && !nasPath.value) return toast.error('请先选择文件')
  if (!selectedPrinter.value) return toast.error('请先选择打印机')
  if (pageCount.value) {
    const rangeError = validatePageRange(form.value.page_range, pageCount.value)
    if (rangeError) return toast.error(rangeError)
  }
  submitting.value = true
  try {
    let res: { job_id: number; printer: string }
    if (nasPath.value) {
      res = await api.post<{ job_id: number; printer: string }>('/api/print/nas', {
        printer_id: selectedPrinter.value,
        path: nasPath.value,
        ...form.value,
      })
    } else {
      const fd = new FormData()
      fd.append('file', file.value!)
      fd.append('printer_id', String(selectedPrinter.value))
      for (const [k, v] of Object.entries(form.value)) fd.append(k, String(v))
      res = await api.upload<{ job_id: number; printer: string }>('/api/print', fd)
    }
    toast.success(`已提交打印任务 #${res.job_id}（${res.printer}）`)
    await setFile(null)
    nasPath.value = ''
    await loadJobs()
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    submitting.value = false
  }
}

function openJobFile(id: number) {
  window.open(apiUrl(`/api/jobs/${id}/file`), '_blank')
}

async function cancelJob(id: number) {
  try {
    await api.del(`/api/jobs/${id}`)
    toast.success(`任务 #${id} 已取消`)
    await loadJobs()
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function retryJob(id: number) {
  try {
    const res = await api.post<{ job_id: number }>(`/api/jobs/${id}/retry`)
    toast.success(`已重新提交为任务 #${res.job_id}`)
    await loadJobs()
  } catch (e) {
    toast.error((e as Error).message)
  }
}

function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

function formatTime(iso: string) {
  if (!iso) return ''
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

function openInNewWindow() {
  window.open(window.location.href, '_blank', 'noopener')
}

onMounted(async () => {
  await loadAuthMode()
  await loadPrinters()
  await loadPPDOptions()
  await loadJobs()
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
  if (fileUrl.value) URL.revokeObjectURL(fileUrl.value)
})
</script>

<template>
  <main class="page">
    <header class="topbar">
      <div class="brand">
        <div class="brand-mark">P</div>
        <div>
          <div class="brand-name">Print Portal</div>
          <div class="brand-sub">局域网打印门户</div>
        </div>
      </div>
      <nav class="nav">
        <router-link to="/" class="router-link-active">打印</router-link>
        <router-link to="/scan">扫描</router-link>
        <router-link to="/admin">管理</router-link>
        <button class="nav-link-btn" type="button" @click="openInNewWindow">新窗口</button>
      </nav>
    </header>

    <div v-if="!hasPrinters" class="card">
      <div class="empty">
        还没有配置打印机。请先进入
        <router-link to="/admin/printers">管理 → 打印机</router-link>
        添加设备。
      </div>
    </div>

    <div v-else class="print-layout" :class="{ 'has-preview': file || nasPath }">
      <section class="card">
        <div class="section-head">
          <div>
            <h2 class="card-title">{{ file || nasPath ? '文件预览' : '选择文件' }}</h2>
            <p class="card-desc">支持 PDF、图片、Word / Excel / PPT、PostScript，单次一个文件</p>
          </div>
          <button v-if="authMode === 'fnos'" class="btn btn-ghost btn-sm" type="button" :disabled="nasPicking" @click="pickNasFile">
            {{ nasPicking ? '打开选择器…' : '从 NAS 选择' }}
          </button>
        </div>

        <label
          v-if="!file && !nasPath"
          class="dropzone"
          :class="{ dragging }"
          @dragover.prevent="dragging = true"
          @dragleave.prevent="dragging = false"
          @drop.prevent="onDrop"
        >
          <input type="file" accept=".pdf,.jpg,.jpeg,.png,.webp,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.ps" @change="onFileChange" />
          <div class="dropzone-icon">＋</div>
          <div><strong>拖拽文件到这里</strong>，或点击选择</div>
          <div class="file-sub">最大 200MB</div>
        </label>

        <div v-else-if="nasPath" class="preview-shell">
          <button class="preview-remove" title="移除文件" aria-label="移除文件" @click="clearSource">×</button>
          <div class="preview-head">
            <span class="file-name">{{ nasName }}</span>
            <span class="file-sub">来自 NAS · 已授权<template v-if="pageCount"> · 共 {{ pageCount }} 页</template><template v-else-if="pageCountLoading"> · 读取页数…</template></span>
          </div>
          <iframe
            v-if="isNasPdf"
            :src="nasPreviewUrl"
            class="preview-frame"
            title="PDF 预览"
          ></iframe>
          <img v-else-if="isNasImage" :src="nasPreviewUrl" alt="文件预览" class="preview-image" />
          <div v-else class="file-card" style="width: 100%; margin-top: 14px">
            <div class="file-thumb"><span>NAS</span></div>
            <div class="file-meta">
              <div class="file-name">{{ nasName }}</div>
              <div class="file-sub">{{ nasPath }}</div>
            </div>
          </div>
        </div>

        <div v-else-if="file" class="preview-shell">
          <button class="preview-remove" title="移除文件" aria-label="移除文件" @click="setFile(null)">×</button>
          <div class="preview-head">
            <span class="file-name">{{ file.name }}</span>
            <span class="file-sub">{{ formatSize(file.size) }}<template v-if="pageCount"> · 共 {{ pageCount }} 页</template><template v-else-if="pageCountLoading"> · 读取页数…</template></span>
          </div>
          <iframe
            v-if="isPdf"
            :src="fileUrl"
            class="preview-frame"
            title="PDF 预览"
          ></iframe>
          <img v-else-if="isImage" :src="fileUrl" alt="文件预览" class="preview-image" />
          <div v-else class="file-card" style="width: 100%; margin-top: 14px">
            <div class="file-thumb"><span>{{ isPdf ? 'PDF' : 'DOC' }}</span></div>
            <div class="file-meta">
              <div class="file-name">{{ file.name }}</div>
              <div class="file-sub">{{ formatSize(file.size) }}</div>
            </div>
          </div>
        </div>
      </section>

      <section class="card">
        <div class="section-head">
          <div>
            <h2 class="card-title">打印设置</h2>
            <p class="card-desc">参数会根据所选打印机的 PPD 能力生效</p>
          </div>
        </div>

        <div class="params">
          <div class="field full">
            <label>打印机</label>
            <select v-model="selectedPrinter" class="select">
              <option v-for="p in printers" :key="p.id" :value="p.id">
                {{ p.name }}{{ p.is_default ? '（默认）' : '' }}
              </option>
            </select>
          </div>
          <div v-if="showPageRange" class="field">
            <label>页码范围<span v-if="pageCount" class="field-hint"> 共 {{ pageCount }} 页</span></label>
            <input v-model="form.page_range" class="input" :placeholder="pageRangePlaceholder" />
          </div>
          <div v-else class="field full">
            <label>页码范围</label>
            <div class="field-hint">图片固定为 1 页，无需设置页码范围</div>
          </div>
          <div class="field">
            <label>份数</label>
            <input v-model.number="form.copies" type="number" min="1" max="99" class="input" />
          </div>
          <div class="field">
            <label>双面</label>
            <select v-model="form.duplex" class="select">
              <option value="one-sided">单面</option>
              <option value="two-sided-long-edge">双面（长边翻页）</option>
              <option value="two-sided-short-edge">双面（短边翻页）</option>
            </select>
          </div>
          <div class="field">
            <label>纸张</label>
            <select v-model="form.paper_size" class="select">
              <option v-for="c in paperChoices" :key="c" :value="c">{{ c }}</option>
            </select>
          </div>
          <div class="field">
            <label>方向</label>
            <select v-model="form.orientation" class="select">
              <option value="portrait">纵向</option>
              <option value="landscape">横向</option>
            </select>
          </div>
          <div v-if="supportsColor" class="field">
            <label>颜色</label>
            <select v-model="form.color_mode" class="select">
              <option value="monochrome">黑白</option>
              <option value="color">彩色</option>
            </select>
          </div>
          <div class="field full">
            <label>缩放</label>
            <select v-model="form.scaling" class="select">
              <option value="auto">自动</option>
              <option value="fit">适应页面</option>
              <option value="fill">填满页面</option>
              <option value="none">原始大小</option>
            </select>
          </div>
        </div>

        <button class="btn btn-primary" style="width: 100%; margin-top: 18px" :disabled="submitting || (!file && !nasPath)" @click="submit">
          {{ submitting ? '提交中…' : '打印' }}
        </button>
        <p class="card-desc" style="text-align: center; margin-top: 10px">提交后即可离开，任务在后台排队处理</p>
      </section>
    </div>

    <section class="card" style="margin-top: 20px">
      <div class="section-head">
        <div>
          <h2 class="card-title">最近任务</h2>
          <p class="card-desc">任务进度会自动刷新</p>
        </div>
      </div>
      <div v-if="!jobs.length" class="empty">暂无任务</div>
      <div v-else class="job-list">
        <div v-for="j in jobs" :key="j.id" class="job-item">
          <div class="job-main">
            <div class="job-title">#{{ j.id }} · {{ j.filename }}</div>
            <div class="job-sub">
              {{ j.printer_name }} · {{ formatJobParams(j) }}
              <span v-if="j.error_message" style="color: var(--danger)"> · {{ j.error_message }}</span>
            </div>
            <div class="job-sub">{{ formatTime(j.created_at) }}</div>
          </div>
          <StatusBadge :status="j.status" />
          <button v-if="j.source === 'web' && ['submitted', 'queued', 'printing', 'completed', 'failed'].includes(j.status)" class="btn btn-ghost btn-sm" @click="openJobFile(j.id)">
            查看文件
          </button>
          <button v-if="['pending', 'converting', 'submitted', 'queued', 'printing', 'held'].includes(j.status)" class="btn btn-ghost btn-sm" @click="cancelJob(j.id)">
            取消
          </button>
          <button v-if="['failed', 'cancelled'].includes(j.status) && j.source === 'web'" class="btn btn-ghost btn-sm" @click="retryJob(j.id)">
            重试
          </button>
        </div>
      </div>
    </section>

    <footer class="footer" style="margin-top: 28px; text-align: center; color: var(--text-3); font-size: 12px">
      单用户 · 局域网 · 自带 CUPS · PPD + RAW
    </footer>
  </main>
</template>
