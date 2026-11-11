<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { TrimApp } from '@trimjs/web-app'
import { api, apiUrl } from '../api'
import { toast } from '../toast'
import type { PaperArea, Printer } from '../types'
import CropEditor from './CropEditor.vue'
import ModePreview from './ModePreview.vue'
import PhotoCollageEditor from './PhotoCollageEditor.vue'

interface PhotoItem { id: number; file?: File; blob?: Blob; nasPath?: string; name: string; sourceUrl: string; previewUrl: string }

const props = defineProps<{
  printers: Printer[]
  printerId: number | null
  paperChoices: string[]
  paperAreas: PaperArea[]
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
const paperSize = ref(paperChoices.value[0] || 'A4')
const layoutMode = ref<'auto' | 'manual'>('auto')
const manualLayout = ref<{ sheets: { items: { index: number; x_mm: number; y_mm: number; w_mm: number; h_mm: number; rotation: number }[] }[] }>({ sheets: [] })
const perSheet = ref(4)
const arrangement = ref<'auto' | 'horizontal' | 'vertical'>('auto')
const scaling = ref<'fit' | 'fill'>('fit')
const borderless = ref(true)
const cutLines = ref(true)
const copies = ref(1)
const items = ref<PhotoItem[]>([])
const editingId = ref<number | null>(null)
const editorSrc = ref('')
const fileInput = ref<HTMLInputElement | null>(null)
const previewId = ref('')
const previewLoading = ref(false)
const outputPages = ref(0)
const composeError = ref('')
const submitting = ref(false)
let nextId = 1
let composeTimer: number | undefined
let composeToken = 0

const previewSrc = computed(() => previewId.value ? `/api/printmode/preview/${previewId.value}` : '')
const editingItem = computed(() => items.value.find((item) => item.id === editingId.value) || null)
const collageImages = computed(() => items.value.map((item) => ({ id: item.id, url: item.previewUrl, name: item.name })))
const canPrint = computed(() => {
  const ready = items.value.every((item) => item.nasPath || item.file || item.blob)
  if (!ready) return false
  if (layoutMode.value === 'manual') return items.value.length > 0 && manualLayout.value.sheets.some((sheet) => sheet.items.length > 0) && !submitting.value
  return previewId.value !== '' && !previewLoading.value && items.value.length > 0 && !submitting.value
})

const fallbackMM: Record<string, [number, number]> = {
  A4: [210, 297], A5: [148, 210], A6: [105, 148], Letter: [216, 279], Legal: [216, 356],
  '4x6': [102, 152], '6x4': [152, 102], '4R': [102, 152], '3R': [89, 127], '5x3': [127, 89],
  '102x152mm': [102, 152], '152x102mm': [152, 102], '89x127mm': [89, 127], '127x89mm': [127, 89],
}
const paperAliases: Record<string, string> = {
  '4x6': '6寸（4R，102×152mm）', '6x4': '6寸（4R，102×152mm）', '4R': '6寸（4R，102×152mm）',
  '102x152mm': '6寸（4R，102×152mm）', '152x102mm': '6寸（4R，102×152mm）',
  '3R': '5寸（3R，89×127mm）', '5x3': '5寸（3R，89×127mm）', '89x127mm': '5寸（3R，89×127mm）',
  '127x89mm': '5寸（3R，89×127mm）',
}
const paperDims = computed<[number, number]>(() => {
  const area = props.paperAreas.find((item) => item.page_size.toLowerCase() === paperSize.value.toLowerCase())
  if (area && area.width > 0 && area.height > 0) {
    return [area.width / 72 * 25.4, area.height / 72 * 25.4]
  }
  return fallbackMM[paperSize.value] || fallbackMM.A4
})
const paperLabel = (value: string) => paperAliases[value] || value
const paperOrientation = computed(() => paperDims.value[0] > paperDims.value[1] ? 'landscape' : 'portrait')

watch([items, perSheet, arrangement, scaling, borderless, cutLines, layoutMode], () => {
  window.clearTimeout(composeTimer)
  composeTimer = window.setTimeout(compose, 350)
}, { deep: true })

onUnmounted(() => {
  window.clearTimeout(composeTimer)
  for (const item of items.value) {
    if (item.previewUrl.startsWith('blob:')) URL.revokeObjectURL(item.previewUrl)
    if (item.sourceUrl.startsWith('blob:')) URL.revokeObjectURL(item.sourceUrl)
  }
})

function addFiles() { fileInput.value?.click() }
function onFilesPicked(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  input.value = ''
  if (files.length) ingest(files)
}
function ingest(files: File[]) {
  const added: PhotoItem[] = []
  for (const file of files) {
    if (!/^image\//.test(file.type)) {
      toast.error(`${file.name} 不是图片`)
      continue
    }
    const url = URL.createObjectURL(file)
    const item: PhotoItem = { id: nextId++, file, name: file.name, sourceUrl: url, previewUrl: url }
    items.value.push(item)
    added.push(item)
  }
  // Cropping is optional: uploaded images can be printed as-is.
}
function openEditor(item: PhotoItem) {
  if (item.previewUrl.startsWith('blob:') && item.previewUrl !== item.sourceUrl) URL.revokeObjectURL(item.previewUrl)
  item.previewUrl = item.sourceUrl
  editorSrc.value = item.sourceUrl
  editingId.value = item.id
}
function onCropConfirm(blob: Blob) {
  const item = editingItem.value
  if (!item) return
  item.blob = blob
  if (item.previewUrl.startsWith('blob:') && item.previewUrl !== item.sourceUrl) URL.revokeObjectURL(item.previewUrl)
  item.previewUrl = URL.createObjectURL(blob)
  editingId.value = null
}
function onCropCancel() { editingId.value = null }
function removeItem(item: PhotoItem) {
  if (item.previewUrl.startsWith('blob:')) URL.revokeObjectURL(item.previewUrl)
  if (item.sourceUrl.startsWith('blob:') && item.sourceUrl !== item.previewUrl) URL.revokeObjectURL(item.sourceUrl)
  items.value = items.value.filter((candidate) => candidate.id !== item.id)
}

async function pickNasImages() {
  try {
    const sdk = new TrimApp()
    if (sdk.isStandaloneWeb) {
      toast.error('请从飞牛应用中心打开后选择 NAS 文件')
      return
    }
    const result = await sdk.pickUserFile({
      directory: false,
      multiple: true,
      title: '选择图片',
      okText: '选择',
      accept: ['.jpg', '.jpeg', '.png', '.webp'],
      sidebarGroup: ['myFiles', 'otherShare', 'favorites'],
    })
    const paths: string[] = result?.data || []
    if (!paths.length) return
    for (const path of paths) {
      const name = path.split(/[\\/]/).pop() || path
      const url = apiUrl(`/api/nas/preview?path=${encodeURIComponent(path)}`)
      items.value.push({ id: nextId++, name, nasPath: path, sourceUrl: url, previewUrl: url })
    }
    toast.success(`已从 NAS 选择 ${paths.length} 张图片`)
  } catch (e) {
    toast.error((e as Error).message || '选择 NAS 文件失败')
  }
}

function appendPhotoForm(form: FormData) {
  for (const item of items.value) {
    if (!item.nasPath) form.append('files', item.blob || item.file!, item.name)
  }
  form.append('item_sources', JSON.stringify(items.value.map((item) => item.nasPath ? 'nas' : 'upload')))
  form.append('nas_paths', JSON.stringify(items.value.map((item) => item.nasPath || '')))
}

async function compose() {
  if (layoutMode.value === 'manual') {
    previewId.value = ''
    outputPages.value = manualLayout.value.sheets.length
    previewLoading.value = false
    composeError.value = ''
    return
  }
  if (!items.value.length) {
    previewId.value = ''
    outputPages.value = 0
    previewLoading.value = false
    composeError.value = ''
    return
  }
  const request = ++composeToken
  previewLoading.value = true
  composeError.value = ''
  try {
    const form = new FormData()
    appendPhotoForm(form)
    form.append('paper_width_mm', String(paperDims.value[0]))
    form.append('paper_height_mm', String(paperDims.value[1]))
    form.append('per_sheet', String(perSheet.value))
    form.append('arrangement', arrangement.value)
    form.append('scaling', scaling.value)
    form.append('borderless', borderless.value ? '1' : '0')
    form.append('margin_mm', '4')
    form.append('cut_lines', cutLines.value ? '1' : '0')
    const result = await api.upload<{ preview_id: string; page_count: number }>('/api/printmode/photos', form)
    if (request === composeToken) {
      previewId.value = result.preview_id
      outputPages.value = result.page_count
    }
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
  if (!printer.value || !items.value.length) return
  submitting.value = true
  try {
    let preview = previewId.value
    if (layoutMode.value === 'manual') {
      const composeForm = new FormData()
      appendPhotoForm(composeForm)
      composeForm.append('paper_width_mm', String(paperDims.value[0]))
      composeForm.append('paper_height_mm', String(paperDims.value[1]))
      composeForm.append('layout', JSON.stringify(manualLayout.value))
      const result = await api.upload<{ preview_id: string }>('/api/printmode/photos/manual', composeForm)
      preview = result.preview_id
    }
    if (!preview) throw new Error('版面还没有准备好')
    const form = new FormData()
    form.append('preview_id', preview)
    form.append('print_mode', 'photos')
    form.append('printer_id', String(printer.value))
    form.append('copies', String(copies.value))
    form.append('paper_size', paperSize.value)
    form.append('orientation', paperOrientation.value)
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
          <h2 class="card-title">图片打印</h2>
          <p class="card-desc">{{ layoutMode === 'manual' ? '一次选择多张图片，自由拖拽拼贴到纸张。' : '一次选择多张图片，按纸张和每张张数自动排版。' }}</p>
        </div>
      </div>
      <div class="photo-grid">
        <div v-for="item in items" :key="item.id" class="photo-card">
          <img :src="item.previewUrl" :alt="item.name" />
          <button class="photo-remove" type="button" @click="removeItem(item)">×</button>
          <div class="photo-name">{{ item.name }}</div>
          <button class="btn btn-ghost btn-sm" type="button" @click="openEditor(item)">{{ item.blob ? '重新裁剪' : '裁剪' }}</button>
        </div>
      </div>
      <button class="photo-add" type="button" @click="addFiles">＋ 添加图片（JPG / PNG / WebP，可多选）</button>
      <button v-if="nasEnabled" class="photo-add" type="button" @click="pickNasImages">从 NAS 选择图片</button>
      <input ref="fileInput" class="hidden-file" type="file" multiple accept=".jpg,.jpeg,.png,.webp" @change="onFilesPicked" />
      <PhotoCollageEditor v-if="layoutMode === 'manual'" :images="collageImages" :paper-width-mm="paperDims[0]" :paper-height-mm="paperDims[1]" @update:layout="manualLayout = $event" />

      <div v-if="layoutMode === 'auto' && (previewId || previewLoading || composeError)" class="photo-preview">
        <div class="photo-preview-head">打印预览</div>
        <div v-if="previewLoading" class="photo-preview-state">正在合成版面…</div>
        <div v-else-if="composeError" class="photo-preview-state error">{{ composeError }}</div>
        <ModePreview v-else :key="previewId" :src="previewSrc" label="图片打印" :max-width="620" />
      </div>
    </section>

    <section class="card">
      <div class="section-head"><div><h2 class="card-title">图片设置</h2><p class="card-desc">纸张和排版参数由打印机 PPD 能力驱动。</p></div></div>
      <div class="params">
        <div class="field full"><label>打印机</label><select v-model.number="printer" class="select"><option v-for="item in printers" :key="item.id" :value="item.id">{{ item.name }}{{ item.is_default ? '（默认）' : '' }}</option></select></div>
        <div class="field full"><label>纸张</label><select v-model="paperSize" class="select"><option v-for="choice in paperChoices" :key="choice" :value="choice">{{ paperLabel(choice) }}</option></select></div>
        <div class="field full"><label>排版方式</label><div class="segment"><button type="button" :class="{ on: layoutMode === 'auto' }" @click="layoutMode = 'auto'">自动排版</button><button type="button" :class="{ on: layoutMode === 'manual' }" @click="layoutMode = 'manual'">手动拼贴</button></div></div>
        <div v-if="layoutMode === 'auto'" class="field"><label>每张纸</label><select v-model.number="perSheet" class="select"><option :value="1">1 张</option><option :value="2">2 张</option><option :value="4">4 张</option><option :value="6">6 张</option><option :value="8">8 张</option></select></div>
        <div v-if="layoutMode === 'auto'" class="field"><label>排列方式</label><select v-model="arrangement" class="select"><option value="auto">自动网格</option><option value="horizontal">横向优先</option><option value="vertical">纵向优先</option></select></div>
        <div v-if="layoutMode === 'auto'" class="field"><label>图片缩放</label><select v-model="scaling" class="select"><option value="fit">适应</option><option value="fill">填满</option></select></div>
        <div class="field"><label>份数</label><input v-model.number="copies" type="number" min="1" max="99" class="input" /></div>
        <div class="field full"><label>边距</label><select v-model="borderless" class="select"><option :value="true">无边框</option><option :value="false">白边（4mm）</option></select></div>
        <div class="field full"><label class="toggle-label"><span>显示裁剪线</span><span class="toggle" :class="{ off: !cutLines }" @click="cutLines = !cutLines" /></label></div>
      </div>
      <button class="btn btn-primary print-button" type="button" :disabled="!canPrint" @click="submit">{{ submitting ? '提交中…' : `打印 ${items.length} 张图片` }}</button>
      <p class="card-desc" style="text-align:center;margin-top:8px"><template v-if="layoutMode === 'manual'">{{ manualLayout.sheets.length }} 张纸 · </template><template v-else-if="outputPages">将输出 {{ outputPages }} 张纸 · </template>提交后即可离开</p>
    </section>
    <CropEditor v-if="editingItem" :src="editorSrc" title="裁剪图片" :aspect="null" @confirm="onCropConfirm" @cancel="onCropCancel" />
  </div>
</template>

<style scoped>
.photo-grid { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:12px; }
.photo-card { min-width:0; border:1px solid var(--line); border-radius:12px; padding:8px; background:var(--surface-2); position:relative; }
.photo-card img { width:100%; height:110px; object-fit:cover; border-radius:8px; background:#fff; display:block; }
.photo-name { font-size:11px; color:var(--text-3); margin-top:7px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.photo-remove { position:absolute; right:10px; top:10px; width:24px; height:24px; border:0; border-radius:7px; background:rgba(255,255,255,.85); color:#64748b; font-size:16px; cursor:pointer; }
.photo-add { width:100%; height:44px; margin-top:12px; border:1.5px dashed var(--line-strong); border-radius:11px; background:#fafbfe; color:var(--text-3); font-size:13px; cursor:pointer; }
.photo-preview { margin-top:20px; border-top:1px solid var(--line); padding-top:18px; }
.photo-preview-head { font-size:14px; font-weight:700; margin-bottom:12px; }
.photo-preview-state { padding:30px; text-align:center; font-size:13px; color:var(--text-3); }
.photo-preview-state.error { color:var(--danger); }
.hidden-file { display:none; }
.segment { display:flex; background:#f1f5f9; border-radius:10px; padding:3px; gap:2px; }
.segment button { flex:1; border:0; background:transparent; color:var(--text-2); padding:8px 0; border-radius:8px; font-size:13px; font-weight:600; cursor:pointer; }
.segment button.on { background:#fff; color:var(--accent); box-shadow:0 1px 3px rgba(15,23,42,.08); }
.toggle-label { display:flex; align-items:center; justify-content:space-between; }
.toggle { width:42px; height:24px; border-radius:999px; background:var(--accent); position:relative; display:inline-block; cursor:pointer; }
.toggle::after { content:''; position:absolute; width:20px; height:20px; border-radius:50%; background:#fff; right:2px; top:2px; box-shadow:0 1px 3px rgba(0,0,0,.2); }
.toggle.off { background:#cbd5e1; }
.toggle.off::after { right:20px; }
.print-button { width:100%; margin-top:18px; height:46px; }
@media (max-width:760px){ .photo-grid{grid-template-columns:repeat(2,1fr);} }
</style>
