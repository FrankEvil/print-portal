<script setup lang="ts">
import { computed, onUnmounted, ref, shallowRef, watch } from 'vue'
import * as pdfjsLib from 'pdfjs-dist'
import pdfjsWorker from 'pdfjs-dist/build/pdf.worker.min.mjs?url'
import { TrimApp } from '@trimjs/web-app'
import { adminToken, api, apiUrl } from '../api'
import { toast } from '../toast'
import type { PaperArea, Printer } from '../types'
import BookletPreview from './BookletPreview.vue'
import BookletReadingPreview from './BookletReadingPreview.vue'

pdfjsLib.GlobalWorkerOptions.workerSrc = new URL(pdfjsWorker, document.baseURI).href

interface BookletSheet { front: number[]; back: number[] }
interface BookletGeometry {
  padded_pages: number
  sheet_count: number
  side_count: number
  sheets: BookletSheet[]
}

const props = defineProps<{
  printers: Printer[]
  printerId: number | null
  paperChoices: string[]
  paperAreas: PaperArea[]
  duplexSupported?: boolean
  nasEnabled?: boolean
}>()
const emit = defineEmits<{
  (e: 'update:printerId', value: number): void
  (e: 'submitted'): void
}>()

const printer = computed({
  get: () => props.printerId ?? props.printers[0]?.id ?? null,
  set: (value: number) => emit('update:printerId', value),
})
const paperChoices = computed(() => props.paperChoices.length ? props.paperChoices : ['A4'])
const paperSize = ref(paperChoices.value.includes('A4') ? 'A4' : paperChoices.value[0])
const signaturePages = ref(0)
const pageRange = ref('')
const duplexMode = ref<'auto-long' | 'auto-short' | 'manual' | 'single'>(props.duplexSupported === false ? 'manual' : 'auto-long')
const copies = ref(1)
const file = ref<File | null>(null)
const nasPath = ref('')
const fileInput = ref<HTMLInputElement | null>(null)
const pdfDoc = shallowRef<pdfjsLib.PDFDocumentProxy | null>(null)
const pageCount = ref(0)
const sourceName = computed(() => file.value?.name || nasPath.value.split(/[\\/]/).pop() || '')
const geometry = ref<BookletGeometry | null>(null)
const geometryLoading = ref(false)
const geometryError = ref('')
const sheetIndex = ref(0)
const previewSide = ref<'front' | 'back'>('front')
const previewMode = ref<'sheet' | 'reading'>('sheet')
const readingPage = ref(1)
const submitting = ref(false)
const manualStage = ref<'idle' | 'back'>('idle')
const serverBackPreviewId = ref('')
let geometryTimer: number | undefined
let geometryToken = 0

const fallbackMM: Record<string, [number, number]> = { A4: [210, 297], A3: [297, 420], Letter: [216, 279], Legal: [216, 356], A5: [148, 210] }
const paperLabel = (value: string) => value === 'A4' ? 'A4 → A5' : value === 'A3' ? 'A3 → A4' : value
const paperDims = computed<[number, number]>(() => {
  const area = props.paperAreas.find((item) => item.page_size.toLowerCase() === paperSize.value.toLowerCase())
  const dims = area && area.width > 0 && area.height > 0
    ? [area.width / 72 * 25.4, area.height / 72 * 25.4] as [number, number]
    : (fallbackMM[paperSize.value] || fallbackMM.A4)
  return dims[0] > dims[1] ? dims : [dims[1], dims[0]]
})
const currentSheet = computed<BookletSheet | null>(() => geometry.value?.sheets[sheetIndex.value] || null)
const readingPages = computed(() => [readingPage.value, readingPage.value + 1])
const readingEnd = computed(() => Math.min(readingPage.value + 1, pageCount.value))
const signatureGroups = computed(() => {
  const padded = geometry.value?.padded_pages || 0
  if (!padded) return 0
  return Math.ceil(padded / (signaturePages.value || padded))
})
const thumbSheets = computed(() => Array.from({ length: geometry.value?.sheet_count || 0 }, (_, index) => ({ sheet: index })))
const canPrint = computed(() => !!(file.value || nasPath.value) && !!geometry.value && !geometryLoading.value && !submitting.value)

watch(() => props.duplexSupported, (supported) => {
  if (supported && duplexMode.value === 'manual') duplexMode.value = 'auto-long'
})

watch([file, nasPath, signaturePages, pageRange], () => {
  manualStage.value = 'idle'
  serverBackPreviewId.value = ''
  window.clearTimeout(geometryTimer)
  geometryTimer = window.setTimeout(refreshGeometry, 300)
})

watch([file, nasPath], async ([nextFile, nextPath]) => {
  if (nextFile || nextPath) return
  await pdfDoc.value?.destroy()
  pdfDoc.value = null
  pageCount.value = 0
  geometry.value = null
})

onUnmounted(() => {
  window.clearTimeout(geometryTimer)
  void pdfDoc.value?.destroy()
})

function chooseFile() { fileInput.value?.click() }

async function onFilePicked(event: Event) {
  const input = event.target as HTMLInputElement
  const selected = input.files?.[0]
  input.value = ''
  if (!selected) return
  if (!selected.name.toLowerCase().endsWith('.pdf')) {
    toast.error('书籍打印只支持 PDF')
    return
  }
  nasPath.value = ''
  await loadPdfFromFile(selected)
}

async function loadPdfFromFile(selected: File) {
  await pdfDoc.value?.destroy()
  pdfDoc.value = null
  file.value = selected
  pageCount.value = 0
  geometry.value = null
  sheetIndex.value = 0
  previewSide.value = 'front'
  try {
    await loadPdfData(await selected.arrayBuffer())
  } catch {
    toast.error('无法读取 PDF 页数')
  }
}

async function loadPdfData(data: ArrayBuffer) {
  const doc = await pdfjsLib.getDocument({ data }).promise
  pdfDoc.value = doc
  pageCount.value = doc.numPages
  await refreshGeometry()
}

async function pickNasPdf() {
  try {
    const sdk = new TrimApp()
    if (sdk.isStandaloneWeb) {
      toast.error('请从飞牛应用中心打开后选择 NAS 文件')
      return
    }
    const result = await sdk.pickUserFile({
      directory: false,
      multiple: false,
      title: '选择书籍 PDF',
      okText: '选择',
      accept: ['.pdf'],
      sidebarGroup: ['myFiles', 'otherShare', 'favorites'],
    })
    const path = result?.data?.[0]
    if (!path) return
    await pdfDoc.value?.destroy()
    pdfDoc.value = null
    file.value = null
    nasPath.value = path
    pageCount.value = 0
    geometry.value = null
    sheetIndex.value = 0
    previewSide.value = 'front'
    const headers: Record<string, string> = {}
    if (adminToken.value) headers['X-Admin-Token'] = adminToken.value
    const response = await fetch(apiUrl(`/api/nas/preview?path=${encodeURIComponent(path)}`), { headers })
    if (!response.ok) throw new Error('无法读取 NAS 文件')
    await loadPdfData(await response.arrayBuffer())
    toast.success('已从 NAS 选择')
  } catch (e) {
    toast.error((e as Error).message || '选择 NAS 文件失败')
  }
}

async function refreshGeometry() {
  if ((!file.value && !nasPath.value) || !pageCount.value) return
  const token = ++geometryToken
  geometryLoading.value = true
  geometryError.value = ''
  try {
    const result = await api.post<BookletGeometry>('/api/printmode/booklet/geometry', {
      page_count: pageCount.value,
      signature_pages: signaturePages.value,
      page_range: pageRange.value.trim(),
    })
    if (token !== geometryToken) return
    geometry.value = result
    sheetIndex.value = 0
    previewSide.value = 'front'
    previewMode.value = 'sheet'
    readingPage.value = 1
  } catch (e) {
    if (token !== geometryToken) return
    geometry.value = null
    geometryError.value = (e as Error).message
  } finally {
    if (token === geometryToken) geometryLoading.value = false
  }
}

function prevSheet() { if (sheetIndex.value > 0) sheetIndex.value-- }
function nextSheet() { if (geometry.value && sheetIndex.value < geometry.value.sheet_count - 1) sheetIndex.value++ }
function selectThumb(item: { sheet: number; side: 'front' | 'back' }) {
  previewMode.value = 'sheet'
  sheetIndex.value = item.sheet
  previewSide.value = item.side
}
function prevReading() { readingPage.value = Math.max(1, readingPage.value - 2) }
function nextReading() { if (readingPage.value + 2 <= pageCount.value) readingPage.value += 2 }

async function composeOnServer() {
  if (!file.value) throw new Error('请选择 PDF')
  const form = new FormData()
  if (file.value) form.append('file', file.value, file.value.name)
  if (nasPath.value) form.append('nas_path', nasPath.value)
  form.append('paper_width_mm', String(paperDims.value[0]))
  form.append('paper_height_mm', String(paperDims.value[1]))
  form.append('signature_pages', String(signaturePages.value))
  form.append('cut_lines', '1')
  form.append('page_range', pageRange.value.trim())
  return api.upload<{ preview_id: string; front_preview_id: string; back_preview_id: string; page_count: number; sheet_count: number }>('/api/printmode/booklet', form)
}

async function submitPreview(preview: string, duplex: string) {
  if (!printer.value) throw new Error('请选择打印机')
  const form = new FormData()
  form.append('preview_id', preview)
  form.append('print_mode', 'booklet')
  form.append('printer_id', String(printer.value))
  form.append('copies', String(copies.value))
  form.append('paper_size', paperSize.value)
  form.append('orientation', 'landscape')
  form.append('duplex', duplex)
  form.append('color_mode', 'monochrome')
  form.append('scaling', 'auto')
  await api.upload('/api/print', form)
}

async function submit() {
  if (!canPrint.value) return
  submitting.value = true
  try {
    if (duplexMode.value === 'manual') {
      if (manualStage.value === 'idle') {
        const result = await composeOnServer()
        await submitPreview(result.front_preview_id, 'one-sided')
        serverBackPreviewId.value = result.back_preview_id
        manualStage.value = 'back'
        toast.success('正面已提交，请将纸张翻面后打印反面')
      } else {
        await submitPreview(serverBackPreviewId.value, 'one-sided')
        manualStage.value = 'idle'
        toast.success('反面已提交')
        emit('submitted')
      }
      return
    }
    const result = await composeOnServer()
    const duplex = duplexMode.value === 'single' ? 'one-sided' : duplexMode.value === 'auto-short' ? 'two-sided-short-edge' : 'two-sided-long-edge'
    await submitPreview(result.preview_id, duplex)
    toast.success('已提交打印')
    emit('submitted')
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="print-layout has-preview">
    <section class="card">
      <div class="section-head"><div><h2 class="card-title">书籍打印</h2><p class="card-desc">PDF 按 A4 对折成 A5 骑马钉排版，自动补空白页。</p></div><button v-if="nasEnabled" class="btn btn-ghost btn-sm" type="button" @click="pickNasPdf">从 NAS 选择</button></div>
      <button v-if="!file && !nasPath" class="book-drop" type="button" @click="chooseFile">选择 PDF 文件<br><span>点击选择 · 只支持 PDF，本地预览不上传</span></button>
      <div v-else class="book-file">
        <div class="book-icon">PDF</div>
        <div style="flex:1"><div class="book-name">{{ sourceName }}</div><div class="book-meta">原文档 {{ pageCount || '…' }} 页 · 将生成 {{ geometry?.sheet_count || '…' }} 张 A4</div></div>
        <button class="btn btn-ghost btn-sm" type="button" @click="chooseFile">更换</button>
        <button class="book-remove" type="button" @click="file = null; nasPath = ''">×</button>
      </div>
      <input ref="fileInput" class="hidden-file" type="file" accept=".pdf,application/pdf" @change="onFilePicked" />

      <div v-if="file || nasPath" class="book-preview">
        <div class="book-preview-head">
          <span>打印预览</span>
          <div class="book-view-tabs">
            <button type="button" :class="{ on: previewMode === 'sheet' }" @click="previewMode = 'sheet'">按纸张</button>
            <button type="button" :class="{ on: previewMode === 'reading' }" @click="previewMode = 'reading'">按阅读顺序</button>
          </div>
        </div>
        <div v-if="geometryLoading" class="book-state">正在计算版面…</div>
        <div v-else-if="geometryError" class="book-state error">{{ geometryError }}</div>
        <div v-else-if="geometry">
          <template v-if="previewMode === 'sheet' && currentSheet">
            <div class="preview-controls">
              <div class="book-pager">
                <button type="button" :disabled="sheetIndex <= 0" @click="prevSheet">← 上一张</button>
                <span>第 {{ sheetIndex + 1 }} 张 / 共 {{ geometry.sheet_count }} 张</span>
                <button type="button" :disabled="sheetIndex >= geometry.sheet_count - 1" @click="nextSheet">下一张 →</button>
              </div>
              <div class="book-side-tabs">
                <button type="button" :class="{ on: previewSide === 'front' }" @click="previewSide = 'front'">正面</button>
                <button type="button" :class="{ on: previewSide === 'back' }" @click="previewSide = 'back'">反面</button>
              </div>
            </div>
            <div class="book-page-wrap">
              <BookletPreview :doc="pdfDoc" :sheet="currentSheet" :side="previewSide" :paper-width-mm="paperDims[0]" :paper-height-mm="paperDims[1]" :page-count="pageCount" :sheet-index="sheetIndex" :total-sheets="geometry.sheet_count" />
              <div class="book-preview-meta">A4 纸{{ previewSide === 'front' ? '正面' : '反面' }} · 沿中间折线对折</div>
            </div>
            <div class="book-thumbs">
              <div v-for="thumb in thumbSheets" :key="thumb.sheet" class="book-thumb-group" :class="{ on: thumb.sheet === sheetIndex }">
                <div class="book-thumb-title">第 {{ thumb.sheet + 1 }} 张</div>
                <div class="book-thumb-btns">
                  <button type="button" :class="{ on: thumb.sheet === sheetIndex && previewSide === 'front' }" @click="selectThumb({ sheet: thumb.sheet, side: 'front' })">正面</button>
                  <button type="button" :class="{ on: thumb.sheet === sheetIndex && previewSide === 'back' }" @click="selectThumb({ sheet: thumb.sheet, side: 'back' })">反面</button>
                </div>
              </div>
            </div>
            <div class="book-thumb-scroll">← 左右滑动查看更多 →</div>
          </template>
          <template v-else>
            <div class="preview-controls">
              <div class="book-pager">
                <button type="button" :disabled="readingPage <= 1" @click="prevReading">← 上一页</button>
                <span>第 {{ readingPage }} - {{ readingEnd }} 页 / 共 {{ pageCount }} 页</span>
                <button type="button" :disabled="readingPage + 2 > pageCount" @click="nextReading">下一页 →</button>
              </div>
            </div>
            <BookletReadingPreview :doc="pdfDoc" :pages="readingPages" :paper-width-mm="paperDims[0]" :paper-height-mm="paperDims[1]" />
            <div class="book-preview-meta">按阅读顺序预览，仅用于检查内容顺序；实际打印按 A4 纸张排版</div>
          </template>
        </div>
      </div>
    </section>

    <section class="card">
      <div class="section-head"><div><h2 class="card-title">书籍设置</h2><p class="card-desc">默认骑马钉，所有选项都可以改。</p></div></div>
      <div class="params">
        <div class="field full"><label>打印机</label><select v-model.number="printer" class="select"><option v-for="item in printers" :key="item.id" :value="item.id">{{ item.name }}{{ item.is_default ? '（默认）' : '' }}</option></select></div>
        <div class="field full"><label>纸张</label><select v-model="paperSize" class="select"><option v-for="choice in paperChoices" :key="choice" :value="choice">{{ paperLabel(choice) }}</option></select></div>
        <div class="field"><label>装订方式</label><select class="select"><option>骑马钉</option><option>仅折页</option></select></div>
        <div class="field"><label>份数</label><input v-model.number="copies" type="number" min="1" max="99" class="input" /></div>
        <div class="field full"><label>双面</label><select v-model="duplexMode" class="select"><option value="auto-long" :disabled="duplexSupported === false">自动双面（长边）</option><option value="auto-short" :disabled="duplexSupported === false">自动双面（短边）</option><option value="manual">手动双面</option><option value="single">单面</option></select><div v-if="duplexSupported === false" class="field-hint">当前打印机不支持自动双面，可选手动双面</div></div>
        <details class="book-advanced">
          <summary>高级设置</summary>
          <div class="advanced-grid">
            <div class="field full"><label>分册设置</label><select v-model.number="signaturePages" class="select"><option :value="0">整本装订（推荐）</option><option :value="4">每 4 页一叠</option><option :value="8">每 8 页一叠</option><option :value="16">每 16 页一叠</option><option :value="32">每 32 页一叠</option></select><div class="field-hint">书太厚时可分成多叠折叠，再合并装订；一般选整本即可。</div></div>
            <div class="field full"><label>页码范围</label><input v-model="pageRange" class="input" placeholder="全部，如 1-16" /></div>
          </div>
        </details>
        <div class="field full"><div class="book-info">原文档 {{ pageCount }} 页 → 补空白后 {{ geometry?.padded_pages || '—' }} 页<br>{{ geometry?.sheet_count || '—' }} 张 A4 · 分册：{{ signaturePages ? `${signatureGroups} 叠，每叠 ${signaturePages} 页` : '整本（1 叠）' }}<template v-if="duplexMode === 'manual'"> · 手动双面会分两次打印</template></div></div>
      </div>
      <button class="btn btn-primary print-button" type="button" :disabled="!canPrint" @click="submit">
        {{ submitting ? '正在上传并排版…' : manualStage === 'back' ? '打印反面（已翻面）' : duplexMode === 'manual' ? '打印正面' : '打印书籍' }}
      </button>
      <p v-if="manualStage === 'back'" class="manual-hint">正面已提交。请把纸翻面放回纸盒，然后点「打印反面」。</p>
      <p v-else class="card-desc" style="text-align:center;margin-top:8px">预览在本地完成，只有打印时才会上传 PDF</p>
    </section>
  </div>
</template>

<style scoped>
.book-drop { width:100%; height:220px; border:1.5px dashed var(--line-strong); border-radius:14px; background:#fafbfe; color:var(--text-2); font-size:15px; line-height:1.8; cursor:pointer; }
.book-drop span { font-size:12px; color:var(--text-3); }
.book-nas { width:100%; height:40px; margin-top:8px; border:1px solid var(--line); border-radius:10px; background:#fff; color:var(--text-2); font-size:12px; cursor:pointer; }
.book-file { display:flex; align-items:center; gap:12px; border:1px solid var(--line); border-radius:12px; padding:12px; background:#fff; }
.book-icon { width:44px; height:44px; border-radius:10px; background:#eef2ff; color:#4f46e5; display:flex; align-items:center; justify-content:center; font-size:11px; font-weight:700; }
.book-name { font-size:14px; font-weight:600; }
.book-meta { font-size:11px; color:var(--text-3); margin-top:4px; }
.book-remove { width:30px; height:30px; border:0; border-radius:8px; background:var(--surface-2); color:var(--text-3); font-size:18px; cursor:pointer; }
.book-preview { margin-top:20px; border-top:1px solid var(--line); padding-top:18px; }
.book-preview-head { display:flex; align-items:center; justify-content:space-between; font-size:14px; font-weight:700; margin-bottom:12px; }
.book-state { padding:30px; text-align:center; font-size:13px; color:var(--text-3); }
.book-state.error { color:var(--danger); }
.preview-controls { display:flex; align-items:center; justify-content:space-between; gap:10px; margin-bottom:12px; flex-wrap:wrap; }
.book-pager { display:flex; align-items:center; gap:8px; font-size:11px; color:var(--text-2); }
.book-pager button, .book-side-tabs button { border:1px solid var(--line); background:#fff; color:var(--text-2); border-radius:8px; padding:5px 9px; font-size:11px; cursor:pointer; }
.book-pager button:disabled { opacity:.35; cursor:default; }
.book-view-tabs { display:flex; gap:4px; }
.book-view-tabs button { border:1px solid var(--line); background:#fff; color:var(--text-2); border-radius:8px; padding:6px 12px; font-size:12px; cursor:pointer; }
.book-view-tabs button.on { background:#eef2ff; color:var(--accent); border-color:#c7d2fe; }
.book-side-tabs { display:flex; gap:4px; }
.book-side-tabs button.on { background:#eef2ff; color:var(--accent); border-color:#c7d2fe; }
.book-page-wrap { max-width:680px; margin:0 auto; }
.book-preview-meta { text-align:center; font-size:11px; color:var(--text-3); margin-top:8px; }
.book-thumbs { display:flex; gap:8px; overflow-x:auto; margin-top:16px; padding-bottom:4px; }
.book-thumb-group { flex:0 0 auto; border:1px solid var(--line); border-radius:8px; padding:5px 7px; background:#fff; min-width:84px; }
.book-thumb-group.on { border-color:var(--accent); background:var(--accent-soft); }
.book-thumb-title { font-size:9px; color:var(--text-3); font-weight:600; margin-bottom:4px; }
.book-thumb-group.on .book-thumb-title { color:var(--accent); }
.book-thumb-btns { display:flex; gap:4px; }
.book-thumb-btns button { border:1px solid var(--line); background:#fff; color:var(--text-3); border-radius:4px; padding:3px 5px; font-size:9px; cursor:pointer; }
.book-thumb-btns button.on { background:var(--accent); border-color:var(--accent); color:#fff; }
.book-thumb-scroll { text-align:right; font-size:10px; color:var(--text-3); margin-top:4px; }
.book-advanced { border-top:1px solid var(--line); padding-top:12px; margin-top:4px; grid-column:1/-1; }
.book-advanced summary { cursor:pointer; color:var(--text-2); font-size:13px; font-weight:600; list-style:none; }
.book-advanced summary::-webkit-details-marker { display:none; }
.book-advanced summary::before { content:'▸'; display:inline-block; width:16px; color:var(--text-3); }
.book-advanced[open] summary::before { transform:rotate(90deg); }
.advanced-grid { display:grid; grid-template-columns:1fr; gap:0; margin-top:12px; }
.book-info { font-size:12px; color:var(--text-3); background:#f8fafc; border-radius:10px; padding:11px 12px; line-height:1.6; }
.manual-hint { text-align:center; font-size:12px; color:var(--warning); margin-top:8px; }
.print-button { width:100%; margin-top:18px; height:46px; }
.hidden-file { display:none; }
</style>
