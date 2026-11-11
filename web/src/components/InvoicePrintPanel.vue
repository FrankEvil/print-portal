<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { api, apiUrl } from '../api'
import { toast } from '../toast'
import type { Printer } from '../types'
import CropEditor from './CropEditor.vue'
import ModePreview from './ModePreview.vue'

interface PageSize { width_mm: number; height_mm: number }
interface InvoiceItem {
  id: number
  file: File
  blob?: Blob
  name: string
  kind: 'pdf' | 'image'
  pages: PageSize[]
  preset: number
  previewUrl: string
}

const props = defineProps<{
  printers: Printer[]
  printerId: number | null
}>()
const emit = defineEmits<{
  (e: 'update:printerId', value: number): void
  (e: 'submitted'): void
}>()

const printer = computed({
  get: () => props.printerId ?? props.printers[0]?.id ?? null,
  set: (value: number) => emit('update:printerId', value),
})

const sizePresets = [
  { label: '电子发票 210×140', width: 210, height: 140 },
  { label: '专票/普票 241×140', width: 241, height: 140 },
  { label: 'A4 210×297', width: 210, height: 297 },
  { label: '卷票 76×177.8', width: 76, height: 177.8 },
  { label: '自定义 210×140', width: 210, height: 140 },
]

const items = ref<InvoiceItem[]>([])
const perSheet = ref<1 | 2>(2)
const trim = ref(true)
const cutLines = ref(true)
const copies = ref(1)
const previewId = ref('')
const previewLoading = ref(false)
const composeError = ref('')
const submitting = ref(false)
const editingId = ref<number | null>(null)
const editorSrc = ref('')
const fileInput = ref<HTMLInputElement | null>(null)
let nextId = 1
let composeTimer: number | undefined
let composeToken = 0

const previewSrc = computed(() => previewId.value ? `/api/printmode/preview/${previewId.value}` : '')
const canPrint = computed(() => previewId.value !== '' && !previewLoading.value && items.value.length > 0 && !submitting.value && items.value.every((item) => item.kind === 'pdf' || item.blob))
const editingItem = computed(() => items.value.find((item) => item.id === editingId.value) || null)

watch([items, perSheet, trim, cutLines], () => {
  window.clearTimeout(composeTimer)
  composeTimer = window.setTimeout(compose, 350)
}, { deep: true })

onUnmounted(() => {
  window.clearTimeout(composeTimer)
  for (const item of items.value) {
    if (item.previewUrl) URL.revokeObjectURL(item.previewUrl)
  }
})

function addFiles() {
  fileInput.value?.click()
}

function onFilesPicked(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  input.value = ''
  if (files.length) ingest(files)
}

function ingest(files: File[]) {
  const added: InvoiceItem[] = []
  for (const file of files) {
    const ext = file.name.toLowerCase().split('.').pop() || ''
    const kind: 'pdf' | 'image' = ext === 'pdf' ? 'pdf' : 'image'
    const item: InvoiceItem = {
      id: nextId++,
      file,
      name: file.name,
      kind,
      pages: [],
      preset: 0,
      previewUrl: '',
    }
    items.value.push(item)
    added.push(item)
  }
  void inspect(added)
  const firstImage = added.find((item) => item.kind === 'image')
  if (firstImage) openEditor(firstImage)
}

async function inspect(targets: InvoiceItem[]) {
  const form = new FormData()
  for (const item of targets) form.append('files', item.file, item.name)
  try {
    const result = await api.upload<{ items: { name: string; kind: string; pages: PageSize[] }[] }>('/api/printmode/inspect', form)
    targets.forEach((item, index) => {
      const info = result.items[index]
      if (info) item.pages = info.pages || []
    })
  } catch (e) {
    toast.error(`读取发票信息失败：${(e as Error).message}`)
  }
}

function presetOf(item: InvoiceItem) {
  return sizePresets[item.preset] || sizePresets[0]
}

function itemMeta(item: InvoiceItem) {
  if (item.kind === 'image') {
    const preset = presetOf(item)
    return `图片 · ${item.blob ? '已裁剪' : '未裁剪'} · ${preset.label.replace(' 自定义', '')}`
  }
  if (!item.pages.length) return '正在读取页面尺寸…'
  if (item.pages.length === 1) {
    const page = item.pages[0]
    return `${page.width_mm.toFixed(0)} × ${page.height_mm.toFixed(0)} mm · 1 页`
  }
  return `${item.pages.length} 页 · 首页 ${item.pages[0].width_mm.toFixed(0)} × ${item.pages[0].height_mm.toFixed(0)} mm`
}

function openEditor(item: InvoiceItem) {
  if (item.kind !== 'image') return
  if (item.previewUrl) URL.revokeObjectURL(item.previewUrl)
  item.previewUrl = URL.createObjectURL(item.file)
  editorSrc.value = item.previewUrl
  editingId.value = item.id
}

function onCropConfirm(blob: Blob) {
  const item = editingItem.value
  if (!item) return
  item.blob = blob
  if (item.previewUrl) URL.revokeObjectURL(item.previewUrl)
  item.previewUrl = URL.createObjectURL(blob)
  editingId.value = null
}

function onCropCancel() {
  editingId.value = null
}

function removeItem(item: InvoiceItem) {
  if (item.previewUrl) URL.revokeObjectURL(item.previewUrl)
  items.value = items.value.filter((candidate) => candidate.id !== item.id)
}

async function compose() {
  if (!items.value.length) {
    previewId.value = ''
    previewLoading.value = false
    composeError.value = ''
    return
  }
  if (items.value.some((item) => item.kind === 'image' && !item.blob)) {
    previewId.value = ''
    previewLoading.value = false
    composeError.value = '请先裁剪所有图片发票'
    return
  }
  const request = ++composeToken
  previewLoading.value = true
  composeError.value = ''
  try {
    const form = new FormData()
    const sizes: PageSize[] = []
    for (const item of items.value) {
      const file = item.kind === 'image' ? (item.blob || item.file) : item.file
      form.append('files', file, item.name)
      if (item.kind === 'image') {
        const preset = presetOf(item)
        sizes.push({ width_mm: preset.width, height_mm: preset.height })
      } else {
        sizes.push({ width_mm: 0, height_mm: 0 })
      }
    }
    form.append('image_sizes', JSON.stringify(sizes))
    form.append('per_sheet', String(perSheet.value))
    form.append('trim', trim.value ? '1' : '0')
    form.append('cut_lines', cutLines.value ? '1' : '0')
    const result = await api.upload<{ preview_id: string }>('/api/printmode/invoice', form)
    if (request === composeToken) previewId.value = result.preview_id
  } catch (e) {
    if (request === composeToken) {
      previewId.value = ''
      composeError.value = (e as Error).message
    }
  } finally {
    if (request === composeToken) previewLoading.value = false
  }
}

async function submit() {
  if (!previewId.value || !printer.value) return
  submitting.value = true
  try {
    const form = new FormData()
    form.append('preview_id', previewId.value)
    form.append('print_mode', 'invoice')
    form.append('printer_id', String(printer.value))
    form.append('copies', String(copies.value))
    form.append('paper_size', 'A4')
    form.append('orientation', 'portrait')
    form.append('duplex', 'one-sided')
    form.append('color_mode', 'color')
    form.append('scaling', 'auto')
    await api.upload('/api/print', form)
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
      <div class="section-head">
        <div>
          <h2 class="card-title">发票</h2>
          <p class="card-desc">支持 PDF 电子发票和 JPG / PNG 图片，按原始尺寸排版，不缩放。</p>
        </div>
      </div>

      <div class="invoice-list">
        <div v-for="item in items" :key="item.id" class="invoice-row" :class="{ image: item.kind === 'image' }">
          <div class="invoice-icon" :class="item.kind">{{ item.kind === 'pdf' ? 'PDF' : 'IMG' }}</div>
          <div class="invoice-main">
            <div class="invoice-name">{{ item.name }}</div>
            <div class="invoice-meta">{{ itemMeta(item) }}</div>
          </div>
          <select v-if="item.kind === 'image'" v-model.number="item.preset" class="invoice-size">
            <option v-for="(preset, index) in sizePresets" :key="preset.label" :value="index">{{ preset.label }}</option>
          </select>
          <button v-if="item.kind === 'image'" class="btn btn-ghost btn-sm" type="button" @click="openEditor(item)">重新裁剪</button>
          <button class="invoice-remove" type="button" title="移除" @click="removeItem(item)">×</button>
        </div>
        <button class="invoice-add" type="button" @click="addFiles">＋ 添加发票（PDF / JPG / PNG，可多选）</button>
      </div>

      <div v-if="previewId || previewLoading || composeError" class="invoice-preview">
        <div class="invoice-preview-head">打印预览</div>
        <div v-if="previewLoading" class="invoice-preview-state">正在合成 A4 版面…</div>
        <div v-else-if="composeError" class="invoice-preview-state error">{{ composeError }}</div>
        <ModePreview v-else :src="previewSrc" label="发票" :max-width="520" />
      </div>
    </section>

    <section class="card">
      <div class="section-head">
        <div>
          <h2 class="card-title">发票设置</h2>
          <p class="card-desc">自动选择能 1:1 放下的纸张方向。</p>
        </div>
      </div>
      <div class="params">
        <div class="field full">
          <label>打印机</label>
          <select v-model.number="printer" class="select">
            <option v-for="item in printers" :key="item.id" :value="item.id">{{ item.name }}{{ item.is_default ? '（默认）' : '' }}</option>
          </select>
        </div>
        <div class="field full">
          <label>每张 A4</label>
          <div class="invoice-segment">
            <button type="button" :class="{ on: perSheet === 1 }" @click="perSheet = 1">1 张</button>
            <button type="button" :class="{ on: perSheet === 2 }" @click="perSheet = 2">2 张</button>
          </div>
        </div>
        <div class="field">
          <label>份数</label>
          <input v-model.number="copies" type="number" min="1" max="99" class="input" />
        </div>
        <div class="field">
          <label>纸张</label>
          <div class="input muted">A4（自动横竖排）</div>
        </div>
        <div class="field full">
          <label class="invoice-toggle-row"><span>自动裁掉白边</span><span class="invoice-toggle" :class="{ off: !trim }" @click="trim = !trim" /></label>
        </div>
        <div class="field full">
          <label class="invoice-toggle-row"><span>显示裁剪线</span><span class="invoice-toggle" :class="{ off: !cutLines }" @click="cutLines = !cutLines" /></label>
        </div>
      </div>
      <button class="btn btn-primary invoice-print" type="button" :disabled="!canPrint" @click="submit">
        {{ submitting ? '提交中…' : `打印 ${items.length} 张发票` }}
      </button>
      <p class="card-desc" style="text-align: center; margin-top: 10px">提交后即可离开，任务在后台排队处理</p>
    </section>

    <input ref="fileInput" class="hidden-file" type="file" multiple accept=".pdf,.jpg,.jpeg,.png" @change="onFilesPicked" />
    <CropEditor
      v-if="editingItem"
      :src="editorSrc"
      title="裁剪发票"
      :aspect="null"
      @confirm="onCropConfirm"
      @cancel="onCropCancel"
    />
  </div>
</template>

<style scoped>
.invoice-list { display: flex; flex-direction: column; gap: 10px; }
.invoice-row { display: flex; align-items: center; gap: 12px; border: 1px solid var(--line); border-radius: 12px; padding: 11px 13px; background: #fff; }
.invoice-row.image { border-color: #fde68a; background: #fffdf5; }
.invoice-icon { width: 40px; height: 40px; border-radius: 9px; background: #eef2ff; color: #4f46e5; display: flex; align-items: center; justify-content: center; font-size: 11px; font-weight: 700; flex: none; }
.invoice-icon.image { background: #fef3c7; color: #b45309; }
.invoice-main { flex: 1; min-width: 0; }
.invoice-name { font-size: 13px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.invoice-meta { font-size: 11px; color: var(--text-3); margin-top: 3px; }
.invoice-size { max-width: 180px; height: 34px; border: 1px solid var(--line); border-radius: 8px; background: #fff; font-size: 12px; padding: 0 8px; }
.invoice-remove { width: 30px; height: 30px; border: 0; border-radius: 8px; background: var(--surface-2); color: var(--text-3); font-size: 18px; cursor: pointer; flex: none; }
.invoice-add { height: 46px; border: 1.5px dashed var(--line-strong); border-radius: 12px; background: #fafbfe; color: var(--text-3); font-size: 13px; cursor: pointer; }
.invoice-preview { margin-top: 20px; border-top: 1px solid var(--line); padding-top: 18px; }
.invoice-preview-head { font-size: 14px; font-weight: 700; margin-bottom: 12px; }
.invoice-preview-state { padding: 30px; text-align: center; font-size: 13px; color: var(--text-3); }
.invoice-preview-state.error { color: var(--danger); }
.invoice-segment { display: flex; background: #f1f5f9; border-radius: 10px; padding: 3px; gap: 2px; }
.invoice-segment button { flex: 1; border: 0; background: transparent; color: var(--text-2); padding: 8px 0; border-radius: 8px; font-size: 13px; font-weight: 600; cursor: pointer; }
.invoice-segment button.on { background: #fff; color: var(--accent); box-shadow: 0 1px 3px rgba(15, 23, 42, .08); }
.muted { color: var(--text-3); }
.invoice-toggle-row { display: flex; align-items: center; justify-content: space-between; }
.invoice-toggle { width: 42px; height: 24px; border-radius: 999px; background: var(--accent); position: relative; display: inline-block; cursor: pointer; }
.invoice-toggle::after { content: ''; position: absolute; width: 20px; height: 20px; border-radius: 50%; background: #fff; right: 2px; top: 2px; box-shadow: 0 1px 3px rgba(0, 0, 0, .2); }
.invoice-toggle.off { background: #cbd5e1; }
.invoice-toggle.off::after { right: 20px; }
.invoice-print { width: 100%; margin-top: 18px; height: 46px; }
.hidden-file { display: none; }
@media (max-width: 760px) {
  .invoice-row { flex-wrap: wrap; }
  .invoice-main { flex-basis: calc(100% - 60px); }
}
</style>
