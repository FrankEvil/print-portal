<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import * as pdfjsLib from 'pdfjs-dist'
import pdfjsWorker from 'pdfjs-dist/build/pdf.worker.min.mjs?url'
import { adminToken, apiUrl } from '../api'

pdfjsLib.GlobalWorkerOptions.workerSrc = new URL(pdfjsWorker, document.baseURI).href

const props = defineProps<{
  src: string
  label?: string
  maxWidth?: number
}>()

const pages = ref<{ index: number; url: string }[]>([])
const loading = ref(false)
const error = ref('')
let doc: pdfjsLib.PDFDocumentProxy | null = null
let token = 0

async function load() {
  const request = ++token
  loading.value = true
  error.value = ''
  // Keep the old pages on screen; `out` is only swapped in once fully rendered.
  try {
    if (doc) {
      await doc.destroy()
      doc = null
    }
    const url = props.src.startsWith('http') ? props.src : apiUrl(props.src)
    // PDF.js fetches a URL without our X-Admin-Token header. Load the bytes
    // with fetch first so token-mode deployments do not return 401 here.
    const response = await fetch(url, {
      headers: adminToken.value ? { 'X-Admin-Token': adminToken.value } : {},
    })
    if (!response.ok) {
      throw new Error(`加载预览失败：HTTP ${response.status}`)
    }
    const data = await response.arrayBuffer()
    const loaded = await pdfjsLib.getDocument({ data }).promise
    if (request !== token) {
      await loaded.destroy()
      return
    }
    doc = loaded
    const target = props.maxWidth || 820
    const out: { index: number; url: string }[] = []
    for (let i = 1; i <= loaded.numPages; i++) {
      const page = await loaded.getPage(i)
      const base = page.getViewport({ scale: 1 })
      const scale = Math.min(2, target / base.width)
      const viewport = page.getViewport({ scale })
      const canvas = document.createElement('canvas')
      canvas.width = Math.max(1, Math.round(viewport.width))
      canvas.height = Math.max(1, Math.round(viewport.height))
      const ctx = canvas.getContext('2d')
      if (!ctx) continue
      ctx.fillStyle = '#fff'
      ctx.fillRect(0, 0, canvas.width, canvas.height)
      await page.render({ canvasContext: ctx, viewport }).promise
      out.push({ index: i, url: canvas.toDataURL('image/jpeg', 0.88) })
      if (request !== token) return
    }
    if (request !== token) return
    pages.value = out
  } catch (e) {
    if (request === token) error.value = (e as Error).message
  } finally {
    if (request === token) loading.value = false
  }
}

watch(() => props.src, load, { immediate: true })

onUnmounted(() => {
  token++
  doc?.destroy()
})
</script>

<template>
  <div class="mode-preview">
    <div class="mode-preview-grid">
      <div v-for="page in pages" :key="page.index" class="mode-preview-page">
        <img :src="page.url" :alt="`第 ${page.index} 页预览`" />
        <div class="mode-preview-label">第 {{ page.index }} 页<template v-if="label"> · {{ label }}</template></div>
      </div>
    </div>
    <div v-if="loading" class="mode-preview-state">
      <span class="mode-preview-spinner" aria-hidden="true"></span>
      <span>正在生成预览…</span>
    </div>
    <div v-else-if="error" class="mode-preview-state error">{{ error }}</div>
  </div>
</template>

<style scoped>
.mode-preview { position: relative; min-height: 88px; }
.mode-preview-state { position: absolute; top: 16px; left: 50%; transform: translateX(-50%); z-index: 1; display: flex; align-items: center; gap: 8px; max-width: calc(100% - 32px); padding: 10px 16px; border-radius: 8px; background: rgba(255, 255, 255, .92); box-shadow: 0 2px 10px rgba(15, 23, 42, .14); text-align: center; color: var(--text-3); font-size: 13px; pointer-events: none; }
.mode-preview-state.error { color: var(--danger); }
.mode-preview-spinner { flex: none; width: 14px; height: 14px; border: 2px solid var(--line); border-top-color: var(--text-3); border-radius: 50%; animation: mode-preview-spin .7s linear infinite; }
@keyframes mode-preview-spin { to { transform: rotate(360deg); } }
.mode-preview-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 18px; }
.mode-preview-page { background: #fff; border-radius: 10px; box-shadow: 0 4px 16px rgba(15, 23, 42, .1); padding: 10px; }
.mode-preview-page img { width: 100%; display: block; border-radius: 3px; background: #fff; }
.mode-preview-label { margin-top: 9px; text-align: center; font-size: 11px; color: var(--text-3); }
</style>
