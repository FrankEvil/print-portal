<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import * as pdfjsLib from 'pdfjs-dist'
import pdfjsWorker from 'pdfjs-dist/build/pdf.worker.min.mjs?url'
import { TrimApp } from '@trimjs/web-app'
import { adminToken, api, apiBase, apiUrl } from '../api'
import { toast } from '../toast'
import type { Job, PaperArea, PPDOption, Printer } from '../types'
import { formatJobParams } from '../jobDisplay'
import StatusBadge from '../components/StatusBadge.vue'

pdfjsLib.GlobalWorkerOptions.workerSrc = `${apiBase}${pdfjsWorker}`

const printers = ref<Printer[]>([])
const selectedPrinter = ref<number | null>(null)
const ppdOptions = ref<PPDOption[]>([])
const paperAreas = ref<PaperArea[]>([])
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
const previewUrl = ref('')
const previewLoading = ref(false)
const previewError = ref('')
const previewProgress = ref({ done: 0, total: 0, stage: '' })
const pdfPages = ref<Array<{ page: number; url: string }>>([])
const pdfPageAspect = ref(1)
const pdfSheetPreviews = ref<Array<{ url: string }>>([])
let sheetPreviewToken = 0
const selectedPages = ref<number[]>([])
const extraPPDOptions = ref<Record<string, string>>({})
let previewSessionId = ''
let previewSessionPromise: Promise<string> | null = null
let pdfPreviewToken = 0

const form = ref({
  page_range: '',
  duplex: 'one-sided',
  copies: 1,
  paper_size: 'A4',
  orientation: 'portrait',
  color_mode: 'monochrome',
  scaling: 'auto',
})

const layout = ref({
  pages_per_sheet: 1,
  direction: 'lrtb',
  border: false,
  reverse: false,
  flip_horizontal: false,
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

const paperSizeMm: Record<string, [number, number]> = {
  A4: [210, 297],
  A5: [148, 210],
  A6: [105, 148],
  B5: [176, 250],
  B6: [125, 176],
  L: [89, 127],
  Postcard: [100, 148],
  TPostcard: [148, 100],
  ENV10: [105, 241],
  EnvDL: [110, 220],
  ENVC6: [114, 162],
  Letter: [216, 279],
  Legal: [216, 356],
}

function chooseSheetGrid(n: number, paperWidth: number, paperHeight: number, pageAspect: number) {
  if (n <= 1) return { cols: 1, rows: 1 }
  if (!Number.isFinite(pageAspect) || pageAspect <= 0) pageAspect = 0.7071
  const portrait = form.value.orientation !== 'landscape'
  let bestScore = Number.POSITIVE_INFINITY
  let best = { cols: 1, rows: n }
  for (let rows = 1; rows <= n; rows++) {
    const cols = Math.ceil(n / rows)
    const cellAspect = (paperWidth / cols) / (paperHeight / rows)
    const fill = Math.max(
      Math.min(pageAspect, cellAspect) / Math.max(pageAspect, cellAspect),
      Math.min(1 / pageAspect, cellAspect) / Math.max(1 / pageAspect, cellAspect),
    )
    const score = (1 - fill) * n + (cols * rows - n) * 0.75
    if (score < bestScore - 1e-9) {
      bestScore = score
      best = { cols, rows }
    } else if (Math.abs(score - bestScore) < 1e-9) {
      if ((portrait && rows > best.rows) || (!portrait && cols > best.cols)) best = { cols, rows }
    }
  }
  return best
}

const pdfSheetGrid = computed(() => {
  const n = Math.max(1, layout.value.pages_per_sheet)
  const [paperWidth, paperHeight] = printablePaperSize.value
  return chooseSheetGrid(n, paperWidth, paperHeight, pdfPageAspect.value)
})

const currentPaperArea = computed<PaperArea | null>(() => {
  const area = paperAreas.value.find((item) => item.page_size === form.value.paper_size)
  if (!area) return null
  if (form.value.orientation === 'landscape') {
    return {
      ...area,
      width: area.height,
      height: area.width,
      left: area.bottom,
      right: area.top,
      bottom: area.right,
      top: area.left,
    }
  }
  return area
})

const printablePaperSize = computed<[number, number]>(() => {
  const area = currentPaperArea.value
  if (!area) return paperSizeMm[form.value.paper_size] || paperSizeMm.A4
  return [
    Math.max(1, area.width - area.left - area.right),
    Math.max(1, area.height - area.bottom - area.top),
  ]
})

const previewAspect = computed(() => {
  let [width, height] = paperSizeMm[form.value.paper_size] || paperSizeMm.A4
  if (form.value.orientation === 'landscape') [width, height] = [height, width]
  return `${width} / ${height}`
})

const previewSheetAspect = computed(() => previewAspect.value)

const previewFilter = computed(() => form.value.color_mode === 'monochrome' ? 'grayscale(1)' : 'none')

const previewObjectFit = computed(() => {
  switch (form.value.scaling) {
    case 'fill': return 'cover'
    case 'none': return 'none'
    default: return 'contain'
  }
})

const previewLabel = computed(() => {
  const orientation = form.value.orientation === 'landscape' ? '横向' : '纵向'
  const color = form.value.color_mode === 'color' ? '彩色' : '黑白'
  const duplex = form.value.duplex === 'one-sided' ? '单面' : '双面'
  const scaling = { auto: '自动', fit: '适应页面', fill: '填满页面', none: '原始大小' }[form.value.scaling] || form.value.scaling
  return `${form.value.paper_size} · ${orientation} · ${color} · ${duplex} · ${scaling}`
})

const previewPage = computed(() => {
  const match = form.value.page_range.match(/\d+/)
  return match ? Number(match[0]) : 1
})

const hasSelectedPages = computed(() => {
  if (!isPdf.value && !isNasPdf.value) return true
  return selectedPages.value.length > 0
})

const pdfSheets = computed(() => {
  const size = Math.max(1, layout.value.pages_per_sheet)
  const selected = new Set(selectedPages.value)
  const pages = pdfPages.value.filter((item) => selected.has(item.page))
  const sheets: Array<Array<{ page: number; url: string }>> = []
  for (let i = 0; i < pages.length; i += size) {
    sheets.push(pages.slice(i, i + size))
  }
  return sheets
})

const pdfSheetColumns = computed(() => pdfSheetGrid.value.cols)

const previewAutoRotate = computed(() => {
  if (layout.value.pages_per_sheet <= 1) return 0
  const [width, height] = printablePaperSize.value
  const cols = pdfSheetGrid.value.cols
  const rows = pdfSheetGrid.value.rows
  const cellAspect = (width / cols) / (height / rows)
  const pageAspect = pdfPageAspect.value || 1
  if (cellAspect > 1.2 && pageAspect < 0.9) return 90
  if (cellAspect < 0.9 && pageAspect > 1.2) return 90
  return 0
})

const previewPageTransform = computed(() => {
  const transforms: string[] = []
  if (previewAutoRotate.value) transforms.push(`rotate(${previewAutoRotate.value}deg)`)
  if (layout.value.reverse) transforms.push('rotate(180deg)')
  if (layout.value.flip_horizontal) transforms.push('scaleX(-1)')
  return transforms.join(' ') || undefined
})

const layoutOptionsPayload = computed(() => ({
  ...layout.value,
  orientation: form.value.orientation,
}))

function clearPdfPages() {
  for (const item of pdfPages.value) {
    if (item.url) URL.revokeObjectURL(item.url)
  }
  pdfPages.value = []
  selectedPages.value = []
  for (const preview of pdfSheetPreviews.value) {
    if (preview.url.startsWith('blob:')) URL.revokeObjectURL(preview.url)
  }
  pdfSheetPreviews.value = []
}

function compactPages(pages: number[]): string {
  const sorted = [...new Set(pages)].sort((a, b) => a - b)
  const parts: string[] = []
  let start = sorted[0]
  let prev = sorted[0]
  for (let i = 1; i <= sorted.length; i++) {
    const current = sorted[i]
    if (current !== undefined && current === prev + 1) {
      prev = current
      continue
    }
    if (start !== undefined) {
      parts.push(prev === start ? `${start}` : `${start}-${prev}`)
    }
    start = current
    prev = current
  }
  return parts.join(',')
}

function syncPageRangeFromSelection() {
  const total = pageCount.value || pdfPages.value.length
  if (!selectedPages.value.length) {
    form.value.page_range = ''
    return
  }
  form.value.page_range = selectedPages.value.length >= total ? '' : compactPages(selectedPages.value)
}

function isPageSelected(page: number): boolean {
  return selectedPages.value.includes(page)
}

function togglePage(page: number) {
  const next = new Set(selectedPages.value)
  if (next.has(page)) next.delete(page)
  else next.add(page)
  selectedPages.value = Array.from(next).sort((a, b) => a - b)
  syncPageRangeFromSelection()
  if (isPdf.value || isNasPdf.value) void renderSheetPreviews()
}

function onPageCardClick(page: number) {
  if (layout.value.pages_per_sheet === 1) togglePage(page)
}

function onSheetClick(sheet: Array<{ page: number; url: string }>) {
  if (layout.value.pages_per_sheet === 1 && sheet[0]) togglePage(sheet[0].page)
}

function parsePageSelection(value: string, total: number): number[] | null {
  const text = value.trim()
  if (!text) return Array.from({ length: total }, (_, i) => i + 1)
  const pages = new Set<number>()
  for (const token of text.split(/[\s,]+/).filter(Boolean)) {
    const match = token.match(/^(\d+)(?:-(\d+))?$/)
    if (!match) return null
    const start = Number(match[1])
    const end = match[2] ? Number(match[2]) : start
    if (start < 1 || end < start || end > total) return null
    for (let page = start; page <= end; page++) pages.add(page)
  }
  return Array.from(pages).sort((a, b) => a - b)
}

function onPageRangeInput() {
  if (!isPdf.value && !isNasPdf.value) return
  const total = pageCount.value || pdfPages.value.length
  if (!total) return
  const parsed = parsePageSelection(form.value.page_range, total)
  if (parsed) {
    selectedPages.value = parsed
    if (isPdf.value || isNasPdf.value) void renderSheetPreviews()
  }
}

const colorOption = computed(() => ppdOptions.value.find((o) => {
  const keyword = o.keyword.toLowerCase()
  const text = o.text.toLowerCase()
  const hasColorChoice = o.choices.some((c) => /color|colour|rgb|cmyk/i.test(c))
  const hasMonoChoice = o.choices.some((c) => /mono|gray|grey|black/i.test(c))
  return keyword.includes('color') || keyword.includes('colour') || keyword === 'ink' ||
    text.includes('color') || text.includes('colour') || text.includes('grayscale') ||
    (hasColorChoice && hasMonoChoice)
}))

const supportsColor = computed(() => !!colorOption.value)

const duplexOption = computed(() => ppdOptions.value.find((o) => {
  const keyword = o.keyword.toLowerCase()
  const text = o.text.toLowerCase()
  return keyword.includes('duplex') || text.includes('duplex') || text.includes('2-sided') || text.includes('双面')
}))

const supportsDuplex = computed(() => !!duplexOption.value && duplexOption.value.choices.length > 1)

const duplexChoices = computed(() => {
  const choices = duplexOption.value?.choices.map((c) => c.toLowerCase()) || []
  const items = [{ value: 'one-sided', label: '单面' }]
  if (choices.some((c) => c.includes('duplex') && !c.includes('duplextumble'))) {
    items.push({ value: 'two-sided-long-edge', label: '双面（长边翻页）' })
  }
  if (choices.some((c) => c.includes('duplextumble'))) {
    items.push({ value: 'two-sided-short-edge', label: '双面（短边翻页）' })
  }
  if (items.length === 1 && supportsDuplex.value) items.push({ value: 'two-sided-long-edge', label: '双面（长边翻页）' })
  return items
})

const corePPDKeywords = computed(() => {
  const keys = new Set<string>()
  const pageSize = ppdOptions.value.find((o) => o.keyword === 'PageSize' || /page size/i.test(o.text))
  if (pageSize) keys.add(pageSize.keyword.toLowerCase())
  if (duplexOption.value) keys.add(duplexOption.value.keyword.toLowerCase())
  if (colorOption.value) keys.add(colorOption.value.keyword.toLowerCase())
  return keys
})

const extraPPDOptionList = computed(() =>
  ppdOptions.value.filter((o) => !corePPDKeywords.value.has(o.keyword.toLowerCase())),
)

function resetExtraPPDOptions() {
  const values: Record<string, string> = {}
  for (const option of extraPPDOptionList.value) {
    values[option.keyword] = option.default || option.choices[0] || ''
  }
  extraPPDOptions.value = values
}

const ppdOptionLabels: Record<string, string> = {
  resolution: '分辨率',
  inputslot: '纸张来源',
  mediatype: '介质类型',
  brlasereconomode: '省墨模式',
  brightness: '亮度',
  contrast: '对比度',
  saturation: '饱和度',
  color: '色彩',
  colour: '色彩',
  colormodel: '色彩模式',
  colormode: '色彩模式',
  orientation: '方向',
  outputbin: '输出纸盒',
  finisher: '装订器',
  staple: '装订',
  punch: '打孔',
  copies: '份数',
}

const ppdChoiceLabels: Record<string, Record<string, string>> = {
  resolution: { '600dpi': '600 dpi', '1200dpi': '1200 dpi' },
  inputslot: { auto: '自动', tray1: '纸盒 1', tray2: '纸盒 2', tray3: '纸盒 3', mptray: '手动进纸', manual: '手动', 'multi-purpose': '多用途纸盒' },
  mediatype: {
    plain: '普通纸',
    thin: '薄纸',
    thick: '厚纸',
    thicker: '更厚纸',
    bond: '证券纸',
    trans: '透明胶片',
    env: '信封',
    'env-thick': '厚信封',
    'env-thin': '薄信封',
    plain_normal: '普通纸（标准）',
    env_normal: '信封（标准）',
  },
  brlasereconomode: { false: '关闭', true: '开启' },
  outputbin: { auto: '自动', default: '默认' },
}

function ppdOptionLabel(option: PPDOption): string {
  return ppdOptionLabels[option.keyword.toLowerCase()] || option.text || option.keyword
}

function ppdChoiceLabel(keyword: string, choice: string): string {
  return ppdChoiceLabels[keyword.toLowerCase()]?.[choice.toLowerCase()] || choice
}

function defaultColorMode(): 'color' | 'monochrome' {
  const def = colorOption.value?.default?.toLowerCase() || ''
  return /mono|gray|grey|black/.test(def) ? 'monochrome' : 'color'
}

const pageRangePlaceholder = computed(() => {
  if (pageCount.value) return `全部（共 ${pageCount.value} 页），如 1-${Math.min(pageCount.value, 2)}`
  return '全部，如 1-2 或 1-5 8'
})

async function loadPPDOptions() {
  if (!selectedPrinter.value) return
  try {
    const [res, areaRes] = await Promise.all([
      api.get<{ options: PPDOption[] }>(`/api/printers/${selectedPrinter.value}/ppd-options`),
      api.get<{ areas: PaperArea[] }>(`/api/printers/${selectedPrinter.value}/paper-areas`).catch(() => ({ areas: [] })),
    ])
    ppdOptions.value = res.options
    paperAreas.value = areaRes.areas || []
    form.value.color_mode = colorOption.value ? defaultColorMode() : 'monochrome'
    if (!supportsDuplex.value) form.value.duplex = 'one-sided'
    if (!paperChoices.value.includes(form.value.paper_size)) form.value.paper_size = paperChoices.value[0] || 'A4'
    resetExtraPPDOptions()
  } catch {
    ppdOptions.value = []
    paperAreas.value = []
    extraPPDOptions.value = {}
  }
}

let previewAbort: AbortController | null = null
let previewTimer: number | undefined

function clearPreview() {
  if (previewUrl.value) URL.revokeObjectURL(previewUrl.value)
  previewUrl.value = ''
  clearPdfPages()
  previewError.value = ''
  previewProgress.value = { done: 0, total: 0, stage: '' }
}

async function renderPdfPreview(): Promise<void> {
  const token = ++pdfPreviewToken
  let data: ArrayBuffer
  if (file.value) {
    data = await file.value.arrayBuffer()
  } else if (nasPath.value) {
    const res = await fetch(nasPreviewUrl.value)
    if (!res.ok) throw new Error('PDF 预览读取失败')
    data = await res.arrayBuffer()
  } else {
    throw new Error('请先选择文件')
  }
  const doc = await pdfjsLib.getDocument({ data }).promise
  if (token !== pdfPreviewToken) return
  clearPdfPages()
  pageCount.value = doc.numPages
  pdfPages.value = Array.from({ length: doc.numPages }, (_, index) => ({ page: index + 1, url: '' }))
  selectedPages.value = Array.from({ length: doc.numPages }, (_, index) => index + 1)
  form.value.page_range = ''
  previewProgress.value = { done: 0, total: doc.numPages, stage: '正在渲染页面' }
  let active = 0
  const waiters: Array<() => void> = []
  const acquire = async () => {
    if (active < 4) {
      active++
      return
    }
    await new Promise<void>((resolve) => waiters.push(resolve))
    active++
  }
  const release = () => {
    active--
    const next = waiters.shift()
    if (next) next()
  }
  const renderPageAt = async (index: number) => {
    await acquire()
    try {
      if (token !== pdfPreviewToken) return
      const page = await doc.getPage(index + 1)
      const viewport = page.getViewport({ scale: 0.8 })
      if (page.pageNumber === 1) pdfPageAspect.value = viewport.width / viewport.height
      const canvas = document.createElement('canvas')
      canvas.width = Math.floor(viewport.width)
      canvas.height = Math.floor(viewport.height)
      const context = canvas.getContext('2d')
      if (!context) throw new Error('浏览器不支持 Canvas')
      await page.render({ canvasContext: context, viewport }).promise
      if (token !== pdfPreviewToken) return
      const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/png'))
      if (blob) {
        const item = pdfPages.value[index]
        if (item) item.url = URL.createObjectURL(blob)
      }
      previewProgress.value = {
        ...previewProgress.value,
        done: Math.min(previewProgress.value.total, previewProgress.value.done + 1),
      }
      void renderReadySheets(token)
    } finally {
      release()
    }
  }
  const pageTasks = Array.from({ length: doc.numPages }, (_, index) => renderPageAt(index))
  await Promise.all(pageTasks)
  if (token !== pdfPreviewToken) return
  await renderSheetPreviews()
}

async function renderReadySheets(fileToken: number): Promise<void> {
  const version = sheetPreviewToken
  for (let index = 0; index < pdfSheets.value.length; index++) {
    if (fileToken !== pdfPreviewToken || version !== sheetPreviewToken) return
    if (pdfSheetPreviews.value[index]) continue
    await renderSingleSheet(index, fileToken, version)
  }
}

async function loadImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () => reject(new Error('页面图片加载失败'))
    image.src = url
  })
}

function canvasToBlob(canvas: HTMLCanvasElement): Promise<Blob | null> {
  return new Promise((resolve) => canvas.toBlob(resolve, 'image/jpeg', 0.82))
}

async function renderSingleSheet(sheetIndex: number, fileToken: number, version = sheetPreviewToken): Promise<void> {
  if (fileToken !== pdfPreviewToken || version !== sheetPreviewToken) return
  const group = pdfSheets.value[sheetIndex]
  if (!group || !group.length || group.some((item) => !item.url)) return
  const grid = pdfSheetGrid.value
  const area = currentPaperArea.value
  let [paperWidth, paperHeight] = paperSizeMm[form.value.paper_size] || paperSizeMm.A4
  if (form.value.orientation === 'landscape') [paperWidth, paperHeight] = [paperHeight, paperWidth]
  if (area) {
    paperWidth = area.width
    paperHeight = area.height
  }
  const canvasWidth = 800
  const canvasHeight = Math.round(canvasWidth * paperHeight / paperWidth)
  const canvas = document.createElement('canvas')
  canvas.width = canvasWidth
  canvas.height = canvasHeight
  const context = canvas.getContext('2d')
  if (!context) return
  context.fillStyle = '#fff'
  context.fillRect(0, 0, canvas.width, canvas.height)
  const printLeft = area ? canvas.width * (area.left / area.width) : 0
  const printTop = area ? canvas.height * (area.top / area.height) : 0
  const printWidth = area ? canvas.width * ((area.width - area.left - area.right) / area.width) : canvas.width
  const printHeight = area ? canvas.height * ((area.height - area.bottom - area.top) / area.height) : canvas.height
  const cellWidth = printWidth / grid.cols
  const cellHeight = printHeight / grid.rows
  const images = await Promise.all(group.map((item) => loadImage(item.url).catch(() => null)))
  if (fileToken !== pdfPreviewToken || version !== sheetPreviewToken) return
  for (let index = 0; index < group.length; index++) {
    const image = images[index]
    if (!image) continue
    const pageAspect = image.naturalWidth / image.naturalHeight
    const cellAspect = cellWidth / cellHeight
    let angle = 0
    if (cellAspect > 1.2 && pageAspect < 0.9) angle = 90
    if (cellAspect < 0.9 && pageAspect > 1.2) angle = 90
    if (layout.value.reverse) angle += 180
    const rotated = angle % 180 !== 0
    const fitWidth = rotated ? image.naturalHeight : image.naturalWidth
    const fitHeight = rotated ? image.naturalWidth : image.naturalHeight
    const scale = Math.min((cellWidth - 4) / fitWidth, (cellHeight - 4) / fitHeight)
    const col = index % grid.cols
    const row = Math.floor(index / grid.cols)
    const centerX = printLeft + col * cellWidth + cellWidth / 2
    const centerY = printTop + row * cellHeight + cellHeight / 2
    context.save()
    context.translate(centerX, centerY)
    if (layout.value.flip_horizontal) context.scale(-1, 1)
    context.rotate(angle * Math.PI / 180)
    context.drawImage(
      image,
      -image.naturalWidth * scale / 2,
      -image.naturalHeight * scale / 2,
      image.naturalWidth * scale,
      image.naturalHeight * scale,
    )
    context.restore()
    if (layout.value.border) {
      context.strokeStyle = '#111827'
      context.lineWidth = 2
      context.strokeRect(printLeft + col * cellWidth + 1, printTop + row * cellHeight + 1, cellWidth - 2, cellHeight - 2)
    }
  }
  const blob = await canvasToBlob(canvas)
  if (!blob || fileToken !== pdfPreviewToken || version !== sheetPreviewToken) return
  const next = [...pdfSheetPreviews.value]
  const previous = next[sheetIndex]
  if (previous?.url.startsWith('blob:')) URL.revokeObjectURL(previous.url)
  next[sheetIndex] = { url: URL.createObjectURL(blob) }
  pdfSheetPreviews.value = next
}

async function renderSheetPreviews(): Promise<void> {
  const token = ++sheetPreviewToken
  for (let index = 0; index < pdfSheets.value.length; index++) {
    if (token !== sheetPreviewToken) return
    await renderSingleSheet(index, pdfPreviewToken, token)
  }
}

async function ensurePreviewSession(): Promise<string> {
  if (previewSessionId) return previewSessionId
  if (!file.value) throw new Error('请先选择文件')
  if (!previewSessionPromise) {
    const fd = new FormData()
    fd.append('file', file.value)
    previewSessionPromise = api.upload<{ preview_id: string }>('/api/preview/session', fd)
      .then((res) => {
        previewSessionId = res.preview_id
        return res.preview_id
      })
      .finally(() => {
        previewSessionPromise = null
      })
  }
  return previewSessionPromise
}

async function refreshPreviewNow() {
  previewAbort?.abort()
  previewAbort = null
  if (!file.value && !nasPath.value) {
    clearPreview()
    return
  }
  // Images are rendered directly by the browser; converting them to PDF and
  // running Ghostscript for every parameter change makes the preview sluggish.
  if (isImage.value || isNasImage.value) {
    previewLoading.value = false
    clearPreview()
    return
  }
  if (isPdf.value || isNasPdf.value) {
    previewLoading.value = true
    previewError.value = ''
    try {
      await renderPdfPreview()
    } catch (e) {
      previewError.value = (e as Error).message || 'PDF 预览生成失败'
    } finally {
      previewLoading.value = false
    }
    return
  }
  const controller = new AbortController()
  previewAbort = controller
  previewLoading.value = true
  previewError.value = ''
  try {
    const fd = new FormData()
    if (file.value) {
      const sessionId = await ensurePreviewSession()
      if (previewAbort !== controller) return
      fd.append('preview_id', sessionId)
    } else {
      fd.append('nas_path', nasPath.value)
    }
    fd.append('paper_size', form.value.paper_size)
    fd.append('orientation', form.value.orientation)
    fd.append('color_mode', form.value.color_mode)
    fd.append('scaling', form.value.scaling)
    fd.append('page', String(previewPage.value))
    const headers: Record<string, string> = {}
    if (adminToken.value) headers['X-Admin-Token'] = adminToken.value
    const res = await fetch(apiUrl('/api/preview'), { method: 'POST', body: fd, signal: controller.signal, headers })
    if (!res.ok) {
      let message = '预览生成失败'
      try {
        const body = await res.json()
        message = body?.error?.message || message
      } catch {
        /* ignore */
      }
      throw new Error(message)
    }
    const blob = await res.blob()
    if (previewAbort !== controller) return
    if (previewUrl.value) URL.revokeObjectURL(previewUrl.value)
    previewUrl.value = URL.createObjectURL(blob)
  } catch (e) {
    if ((e as Error).name === 'AbortError') return
    previewError.value = (e as Error).message || '预览生成失败'
  } finally {
    if (previewAbort === controller) previewLoading.value = false
  }
}

function refreshPreview() {
  if (previewTimer) clearTimeout(previewTimer)
  previewTimer = window.setTimeout(() => { void refreshPreviewNow() }, 200)
}

watch([file, nasPath, selectedPrinter], () => refreshPreview())

watch([() => form.value.paper_size, () => form.value.orientation], () => {
  if (isPdf.value || isNasPdf.value) {
    void renderSheetPreviews()
    return
  }
  refreshPreview()
})

watch(paperAreas, () => {
  if (isPdf.value || isNasPdf.value) void renderSheetPreviews()
})

watch([() => form.value.color_mode, () => form.value.scaling], () => {
  // PDF pages are already rendered; color/scaling only affects the preview layer.
  if (isPdf.value || isNasPdf.value) return
  refreshPreview()
})

watch(() => form.value.page_range, () => {
  if (!isPdf.value && !isNasPdf.value) refreshPreview()
})

watch(selectedPrinter, loadPPDOptions)
watch(
  [() => layout.value.pages_per_sheet, () => layout.value.reverse, () => layout.value.flip_horizontal,
    () => layout.value.border, () => form.value.orientation, () => form.value.paper_size],
  () => { void renderSheetPreviews() },
)

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
    ['pending', 'converting', 'submitted', 'queued', 'printing', 'held', 'paused'].includes(j.status),
  )
  if (active && !pollTimer) pollTimer = window.setInterval(loadJobs, 2000)
  if (!active && pollTimer) {
    clearInterval(pollTimer)
    pollTimer = undefined
  }
}

let pageCountToken = 0

async function setFile(f: File | null) {
  previewSessionId = ''
  previewSessionPromise = null
  if (fileUrl.value) URL.revokeObjectURL(fileUrl.value)
  if (f) nasPath.value = ''
  file.value = f
  if (f) form.value.page_range = ''
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
      previewSessionId = ''
      previewSessionPromise = null
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
  if (!hasSelectedPages.value) return toast.error('请至少选择一页')
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
        ppd_options: JSON.stringify(extraPPDOptions.value),
        layout_options: JSON.stringify(layoutOptionsPayload.value),
      })
    } else {
      const fd = new FormData()
      fd.append('file', file.value!)
      fd.append('printer_id', String(selectedPrinter.value))
      for (const [k, v] of Object.entries(form.value)) fd.append(k, String(v))
      fd.append('ppd_options', JSON.stringify(extraPPDOptions.value))
      fd.append('layout_options', JSON.stringify(layoutOptionsPayload.value))
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
  if (previewTimer) clearTimeout(previewTimer)
  previewAbort?.abort()
  if (fileUrl.value) URL.revokeObjectURL(fileUrl.value)
  if (previewUrl.value) URL.revokeObjectURL(previewUrl.value)
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
            <span class="file-sub">来自 NAS · 已授权<template v-if="pageCount"> · 原文档 {{ pageCount }} 页</template><template v-else-if="pageCountLoading"> · 读取页数…</template><template v-if="isNasPdf"> · 已选 {{ selectedPages.length }} 页<template v-if="layout.pages_per_sheet > 1"> · 每张 {{ layout.pages_per_sheet }} 页 → 预计 {{ pdfSheets.length }} 张纸</template></template></span>
          </div>
          <div v-if="isNasPdf" class="pdf-page-grid">
            <div v-for="(sheet, sheetIndex) in pdfSheets" :key="sheetIndex" class="pdf-sheet-card" :class="{ 'single-page': layout.pages_per_sheet === 1 }" :style="{ aspectRatio: previewSheetAspect }">
              <img v-if="pdfSheetPreviews[sheetIndex]" :src="pdfSheetPreviews[sheetIndex].url" alt="打印版面预览" class="pdf-sheet-image" :style="{ filter: previewFilter }" @click="onSheetClick(sheet)" />
              <div v-else class="pdf-page-loading">正在生成版面预览…</div>
              <label v-if="layout.pages_per_sheet === 1 && sheet[0]" class="pdf-page-select" @click.stop>
                <input type="checkbox" :checked="isPageSelected(sheet[0].page)" @change="togglePage(sheet[0].page)" />
                第 {{ sheet[0].page }} 页
              </label>
              <div v-if="layout.pages_per_sheet > 1" class="pdf-sheet-label">第 {{ sheetIndex + 1 }} 张（共 {{ pdfSheets.length }} 张）</div>
            </div>
            <div v-if="previewLoading && !pdfPages.length" class="preview-loading">{{ previewProgress.stage || '正在加载 PDF…' }}<template v-if="previewProgress.total"> · {{ previewProgress.done }}/{{ previewProgress.total }} 页</template></div>
          </div>
          <div v-else class="print-paper" :style="{ aspectRatio: previewAspect }">
            <img v-if="previewUrl" :src="previewUrl" alt="打印效果预览" class="preview-image print-preview-image" :style="{ filter: previewFilter }" />
            <img v-else-if="isNasImage" :src="nasPreviewUrl" alt="文件预览" class="preview-image" :style="{ filter: previewFilter, objectFit: previewObjectFit }" />
            <div v-else class="file-card" style="width: 100%; margin-top: 14px">
              <div class="file-thumb"><span>NAS</span></div>
              <div class="file-meta">
                <div class="file-name">{{ nasName }}</div>
                <div class="file-sub">{{ nasPath }}</div>
              </div>
            </div>
            <div v-if="previewLoading" class="preview-loading">正在生成打印预览…</div>
          </div>
          <div class="print-preview-meta">{{ previewLabel }}</div>
          <p v-if="previewError" class="preview-error">{{ previewError }}</p>
        </div>

        <div v-else-if="file" class="preview-shell">
          <button class="preview-remove" title="移除文件" aria-label="移除文件" @click="setFile(null)">×</button>
          <div class="preview-head">
            <span class="file-name">{{ file.name }}</span>
            <span class="file-sub">{{ formatSize(file.size) }}<template v-if="pageCount"> · 原文档 {{ pageCount }} 页</template><template v-else-if="pageCountLoading"> · 读取页数…</template><template v-if="isPdf"> · 已选 {{ selectedPages.length }} 页<template v-if="layout.pages_per_sheet > 1"> · 每张 {{ layout.pages_per_sheet }} 页 → 预计 {{ pdfSheets.length }} 张纸</template></template></span>
          </div>
          <div v-if="isPdf" class="pdf-page-grid">
            <div v-for="(sheet, sheetIndex) in pdfSheets" :key="sheetIndex" class="pdf-sheet-card" :class="{ 'single-page': layout.pages_per_sheet === 1 }" :style="{ aspectRatio: previewSheetAspect }">
              <img v-if="pdfSheetPreviews[sheetIndex]" :src="pdfSheetPreviews[sheetIndex].url" alt="打印版面预览" class="pdf-sheet-image" :style="{ filter: previewFilter }" @click="onSheetClick(sheet)" />
              <div v-else class="pdf-page-loading">正在生成版面预览…</div>
              <label v-if="layout.pages_per_sheet === 1 && sheet[0]" class="pdf-page-select" @click.stop>
                <input type="checkbox" :checked="isPageSelected(sheet[0].page)" @change="togglePage(sheet[0].page)" />
                第 {{ sheet[0].page }} 页
              </label>
              <div v-if="layout.pages_per_sheet > 1" class="pdf-sheet-label">第 {{ sheetIndex + 1 }} 张（共 {{ pdfSheets.length }} 张）</div>
            </div>
            <div v-if="previewLoading && !pdfPages.length" class="preview-loading">{{ previewProgress.stage || '正在加载 PDF…' }}<template v-if="previewProgress.total"> · {{ previewProgress.done }}/{{ previewProgress.total }} 页</template></div>
          </div>
          <div v-else class="print-paper" :style="{ aspectRatio: previewAspect }">
            <img v-if="previewUrl" :src="previewUrl" alt="打印效果预览" class="preview-image print-preview-image" :style="{ filter: previewFilter }" />
            <img v-else-if="isImage" :src="fileUrl" alt="文件预览" class="preview-image" :style="{ filter: previewFilter, objectFit: previewObjectFit }" />
            <div v-else class="file-card" style="width: 100%; margin-top: 14px">
              <div class="file-thumb"><span>{{ isPdf ? 'PDF' : 'DOC' }}</span></div>
              <div class="file-meta">
                <div class="file-name">{{ file.name }}</div>
                <div class="file-sub">{{ formatSize(file.size) }}</div>
              </div>
            </div>
            <div v-if="previewLoading" class="preview-loading">正在生成打印预览…</div>
          </div>
          <div class="print-preview-meta">{{ previewLabel }}</div>
          <p v-if="previewError" class="preview-error">{{ previewError }}</p>
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
            <input v-model="form.page_range" class="input" :placeholder="pageRangePlaceholder" @input="onPageRangeInput" />
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
            <select v-model="form.duplex" class="select" :disabled="!supportsDuplex">
              <option v-for="choice in duplexChoices" :key="choice.value" :value="choice.value">{{ choice.label }}</option>
            </select>
            <div v-if="!supportsDuplex" class="field-hint">当前打印机不支持双面</div>
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
          <div class="field">
            <label>缩放</label>
            <select v-model="form.scaling" class="select">
              <option value="auto">自动</option>
              <option value="fit">适应页面</option>
              <option value="fill">填满页面</option>
              <option value="none">原始大小</option>
            </select>
          </div>
          <details class="layout-section">
            <summary>布局</summary>
            <div class="layout-grid">
              <div class="field">
                <label>每张页数</label>
                <select v-model.number="layout.pages_per_sheet" class="select">
                  <option :value="1">1</option>
                  <option :value="2">2</option>
                  <option :value="4">4</option>
                  <option :value="6">6</option>
                  <option :value="9">9</option>
                  <option :value="16">16</option>
                </select>
              </div>
              <div class="field">
                <label>布局方向</label>
                <select v-model="layout.direction" class="select">
                  <option value="lrtb">从左到右，从上到下</option>
                  <option value="rltb">从右到左，从上到下</option>
                  <option value="tblr">从上到下，从左到右</option>
                  <option value="btlr">从下到上，从左到右</option>
                </select>
                <div v-if="layout.pages_per_sheet === 1" class="field-hint">仅每张页数大于 1 时生效</div>
              </div>
              <div class="field">
                <label>边框</label>
                <select v-model="layout.border" class="select">
                  <option :value="false">无</option>
                  <option :value="true">细线</option>
                </select>
              </div>
              <div class="field">
                <label>颠倒页面方向</label>
                <select v-model="layout.reverse" class="select">
                  <option :value="false">关闭</option>
                  <option :value="true">开启</option>
                </select>
              </div>
              <div class="field">
                <label>水平翻转</label>
                <select v-model="layout.flip_horizontal" class="select">
                  <option :value="false">关闭</option>
                  <option :value="true">开启</option>
                </select>
              </div>
            </div>
          </details>
          <details v-if="extraPPDOptionList.length" class="extra-ppd-section">
            <summary>更多打印机选项（{{ extraPPDOptionList.length }} 项）</summary>
            <div class="extra-ppd-grid">
              <div v-for="option in extraPPDOptionList" :key="option.keyword" class="field">
                <label>{{ ppdOptionLabel(option) }}</label>
                <select v-model="extraPPDOptions[option.keyword]" class="select">
                  <option v-for="choice in option.choices" :key="choice" :value="choice">{{ ppdChoiceLabel(option.keyword, choice) }}</option>
                </select>
              </div>
            </div>
          </details>
        </div>

        <button class="btn btn-primary" style="width: 100%; margin-top: 18px" :disabled="submitting || (!file && !nasPath) || !hasSelectedPages" @click="submit">
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
          <button v-if="['pending', 'converting', 'submitted', 'queued', 'printing', 'held', 'paused'].includes(j.status)" class="btn btn-ghost btn-sm" @click="cancelJob(j.id)">
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
