<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { TrimApp } from '@trimjs/web-app'
import { api, apiUrl } from '../api'
import { toast } from '../toast'
import type { Printer } from '../types'
import CropEditor from './CropEditor.vue'
import ModePreview from './ModePreview.vue'

const props = defineProps<{
  printers: Printer[]
  printerId: number | null
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

type Side = 'front' | 'back'
const originals = ref<Record<Side, File | null>>({ front: null, back: null })
const cropped = ref<Record<Side, Blob | null>>({ front: null, back: null })
const sources = ref<Record<Side, string>>({ front: '', back: '' })
const nasSources = ref<Record<Side, string>>({ front: '', back: '' })
const previews = ref<Record<Side, string>>({ front: '', back: '' })
const editorSide = ref<Side | null>(null)
const editorSrc = ref('')
const fileInput = ref<HTMLInputElement | null>(null)
const pendingSide = ref<Side>('front')
const layout = ref<'vertical' | 'horizontal'>('vertical')
const border = ref(true)
const copies = ref(1)
const previewId = ref('')
const previewLoading = ref(false)
const composeError = ref('')
const submitting = ref(false)
let composeTimer: number | undefined
let composeToken = 0

const previewSrc = computed(() => previewId.value ? `/api/printmode/preview/${previewId.value}` : '')
const canPrint = computed(() => !!previewId.value && !previewLoading.value && !submitting.value)

watch([cropped, layout, border], () => {
  window.clearTimeout(composeTimer)
  composeTimer = window.setTimeout(compose, 250)
}, { deep: true })

onUnmounted(() => {
  for (const url of Object.values(previews.value)) {
    if (url.startsWith('blob:')) URL.revokeObjectURL(url)
  }
  for (const url of Object.values(sources.value)) {
    if (url.startsWith('blob:')) URL.revokeObjectURL(url)
  }
  window.clearTimeout(composeTimer)
})

function choose(side: Side) {
  pendingSide.value = side
  fileInput.value?.click()
}

function onFilePicked(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  if (!file.type.startsWith('image/')) {
    toast.error('请选择 JPG 或 PNG 图片')
    return
  }
  const side = pendingSide.value
  originals.value[side] = file
  nasSources.value[side] = ''
  cropped.value[side] = null
  if (sources.value[side].startsWith('blob:')) URL.revokeObjectURL(sources.value[side])
  sources.value[side] = URL.createObjectURL(file)
  previews.value[side] = sources.value[side]
  openEditor(side, sources.value[side])
}

async function pickNas(side: Side) {
  try {
    const sdk = new TrimApp()
    if (sdk.isStandaloneWeb) {
      toast.error('请从飞牛应用中心打开后选择 NAS 文件')
      return
    }
    const result = await sdk.pickUserFile({
      directory: false,
      multiple: false,
      title: side === 'front' ? '选择身份证正面' : '选择身份证反面',
      okText: '选择',
      accept: ['.jpg', '.jpeg', '.png'],
      sidebarGroup: ['myFiles', 'otherShare', 'favorites'],
    })
    const path = result?.data?.[0]
    if (!path) return
    if (sources.value[side].startsWith('blob:')) URL.revokeObjectURL(sources.value[side])
    originals.value[side] = null
    nasSources.value[side] = path
    cropped.value[side] = null
    sources.value[side] = apiUrl(`/api/nas/preview?path=${encodeURIComponent(path)}`)
    previews.value[side] = sources.value[side]
    openEditor(side, sources.value[side])
    toast.success('已从 NAS 选择')
  } catch (e) {
    toast.error((e as Error).message || '选择 NAS 文件失败')
  }
}

function openEditor(side: Side, src: string) {
  editorSrc.value = src
  editorSide.value = side
}

function reCrop(side: Side) {
  if (sources.value[side]) openEditor(side, sources.value[side])
}

function onCropConfirm(blob: Blob) {
  const side = editorSide.value
  if (!side) return
  const oldPreview = previews.value[side]
  cropped.value[side] = blob
  // Do not revoke sources[side]: it is the original image URL used when the
  // user clicks "重新裁剪". Only drop an earlier cropped preview blob.
  if (oldPreview.startsWith('blob:') && oldPreview !== sources.value[side]) {
    URL.revokeObjectURL(oldPreview)
  }
  previews.value[side] = URL.createObjectURL(blob)
  editorSide.value = null
}

function onCropCancel() {
  editorSide.value = null
}

async function compose() {
  if (!cropped.value.front || !cropped.value.back) {
    previewId.value = ''
    previewLoading.value = false
    composeError.value = ''
    return
  }
  const request = ++composeToken
  previewLoading.value = true
  composeError.value = ''
  try {
    const form = new FormData()
    form.append('front', cropped.value.front, 'front.jpg')
    form.append('back', cropped.value.back, 'back.jpg')
    form.append('layout', layout.value)
    form.append('border', border.value ? '1' : '0')
    const result = await api.upload<{ preview_id: string }>('/api/printmode/idcard', form)
    if (request === composeToken) previewId.value = result.preview_id
  } catch (e) {
    if (request === composeToken) {
      // Keep the previous preview: a failed recompose must not blank the panel.
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
    form.append('print_mode', 'idcard')
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
          <h2 class="card-title">身份证</h2>
          <p class="card-desc">上传正反面照片并裁剪，系统按 85.6 × 54 mm 实际尺寸排到一张 A4。</p>
        </div>
      </div>

      <div class="id-slots">
        <div v-for="side in (['front', 'back'] as const)" :key="side" class="id-slot" :class="{ done: !!cropped[side] }">
          <div class="id-slot-head">
            <span>身份证{{ side === 'front' ? '正面' : '反面' }}</span>
            <span v-if="cropped[side]" class="id-badge">已裁剪</span>
          </div>
          <template v-if="!cropped[side]">
            <button class="id-drop" type="button" @click="choose(side)">
              <span class="id-plus">＋</span>
              <span>点击上传{{ side === 'front' ? '正面' : '反面' }}照片</span>
              <span class="id-sub">支持 JPG / PNG，建议纯色背景</span>
            </button>
            <button v-if="nasEnabled" class="id-nas" type="button" @click="pickNas(side)">从 NAS 选择</button>
          </template>
          <div v-else class="id-thumb">
            <img :src="previews[side]" alt="身份证预览" />
            <div class="id-thumb-actions">
              <button class="btn btn-ghost btn-sm" type="button" @click="reCrop(side)">重新裁剪</button>
              <button class="btn btn-ghost btn-sm" type="button" @click="choose(side)">重新上传</button>
              <button v-if="nasEnabled" class="btn btn-ghost btn-sm" type="button" @click="pickNas(side)">从 NAS 选择</button>
            </div>
          </div>
        </div>
      </div>

      <div v-if="previewId || previewLoading || composeError" class="id-preview">
        <div class="id-preview-head">打印预览</div>
        <div class="preview-region">
          <ModePreview v-if="previewId" :src="previewSrc" label="身份证" :max-width="620" />
          <div v-else class="id-preview-state">正在合成 A4 版面…</div>
          <div v-if="composeError" class="preview-overlay error">{{ composeError }}</div>
        </div>
      </div>
    </section>

    <section class="card">
      <div class="section-head">
        <div>
          <h2 class="card-title">身份证设置</h2>
          <p class="card-desc">已按实际尺寸排版，不再需要页码、缩放等参数。</p>
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
          <label>排列方式</label>
          <div class="id-segment">
            <button type="button" :class="{ on: layout === 'vertical' }" @click="layout = 'vertical'">上下</button>
            <button type="button" :class="{ on: layout === 'horizontal' }" @click="layout = 'horizontal'">左右</button>
          </div>
        </div>
        <div class="field">
          <label>份数</label>
          <input v-model.number="copies" type="number" min="1" max="99" class="input" />
        </div>
        <div class="field">
          <label>纸张</label>
          <div class="input muted">A4</div>
        </div>
        <div class="field full">
          <label class="id-toggle-row"><span>显示裁剪虚线</span><span class="id-toggle" :class="{ off: !border }" @click="border = !border" /></label>
        </div>
      </div>
      <button class="btn btn-primary print-button" type="button" :disabled="!canPrint" @click="submit">
        {{ submitting ? '提交中…' : '打印' }}
      </button>
      <p class="card-desc" style="text-align: center; margin-top: 10px">提交后即可离开，任务在后台排队处理</p>
    </section>

    <input ref="fileInput" class="hidden-file" type="file" accept=".jpg,.jpeg,.png" @change="onFilePicked" />
    <CropEditor
      v-if="editorSide"
      :src="editorSrc"
      :title="editorSide === 'front' ? '裁剪身份证正面' : '裁剪身份证反面'"
      :aspect="85.6 / 54"
      @confirm="onCropConfirm"
      @cancel="onCropCancel"
    />
  </div>
</template>

<style scoped>
.id-slots { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.id-slot { border: 1px solid var(--line); border-radius: 14px; padding: 14px; background: var(--surface-2); }
.id-slot.done { border-color: #c7d2fe; background: #f8faff; }
.id-slot-head { display: flex; justify-content: space-between; align-items: center; font-size: 13px; font-weight: 700; color: var(--text-2); margin-bottom: 10px; }
.id-badge { font-size: 10px; color: #15803d; background: #dcfce7; border-radius: 999px; padding: 3px 8px; font-weight: 600; }
.id-drop { width: 100%; height: 180px; border: 1.5px dashed var(--line-strong); border-radius: 12px; background: #fff; color: var(--text-3); font-size: 13px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; cursor: pointer; }
.id-plus { width: 40px; height: 40px; border-radius: 10px; background: var(--accent-soft); color: var(--accent); font-size: 22px; line-height: 40px; }
.id-sub { font-size: 11px; color: var(--text-3); }
.id-thumb img { width: 100%; height: 180px; object-fit: contain; background: #fff; border: 1px solid var(--line); border-radius: 10px; }
.id-thumb-actions { display: flex; gap: 8px; margin-top: 9px; }
.id-preview { margin-top: 22px; border-top: 1px solid var(--line); padding-top: 18px; }
.id-preview-head { font-size: 14px; font-weight: 700; margin-bottom: 12px; }
.id-preview-state { padding: 30px; text-align: center; font-size: 13px; color: var(--text-3); }
.id-preview-state.error { color: var(--danger); }
.id-segment { display: flex; background: #f1f5f9; border-radius: 10px; padding: 3px; gap: 2px; }
.id-segment button { flex: 1; border: 0; background: transparent; color: var(--text-2); padding: 8px 0; border-radius: 8px; font-size: 13px; font-weight: 600; cursor: pointer; }
.id-segment button.on { background: #fff; color: var(--accent); box-shadow: 0 1px 3px rgba(15, 23, 42, .08); }
.muted { color: var(--text-3); }
.id-toggle-row { display: flex; align-items: center; justify-content: space-between; }
.id-toggle { width: 42px; height: 24px; border-radius: 999px; background: var(--accent); position: relative; display: inline-block; cursor: pointer; }
.id-toggle::after { content: ''; position: absolute; width: 20px; height: 20px; border-radius: 50%; background: #fff; right: 2px; top: 2px; box-shadow: 0 1px 3px rgba(0, 0, 0, .2); }
.id-toggle.off { background: #cbd5e1; }
.id-toggle.off::after { right: 20px; }
.print-button { width: 100%; margin-top: 18px; height: 46px; }
.hidden-file { display: none; }
.id-nas { width: 100%; margin-top: 8px; border: 1px solid var(--line); background: #fff; color: var(--text-2); border-radius: 9px; font-size: 12px; padding: 8px 0; cursor: pointer; }
@media (max-width: 760px) {
  .id-slots { grid-template-columns: 1fr; }
}
</style>
