<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import type { PDFDocumentProxy } from 'pdfjs-dist'

const props = defineProps<{
  doc: PDFDocumentProxy | null
  pages: number[]
  paperWidthMm: number
  paperHeightMm: number
}>()

const canvas = ref<HTMLCanvasElement | null>(null)
let renderToken = 0

watch([() => props.doc, () => props.pages, () => props.paperWidthMm, () => props.paperHeightMm], () => { void render() }, { immediate: true })
onMounted(() => { void render() })
onUnmounted(() => { renderToken++ })

async function render() {
  const token = ++renderToken
  const element = canvas.value
  const doc = props.doc
  if (!element || !doc) return
  const width = 1200
  const height = Math.max(1, Math.round(width * props.paperHeightMm / props.paperWidthMm))
  element.width = width
  element.height = height
  const ctx = element.getContext('2d')
  if (!ctx) return
  ctx.fillStyle = '#f8fafc'
  ctx.fillRect(0, 0, width, height)
  const margin = 24
  const gap = 12
  const slotW = Math.floor((width - margin * 2 - gap) / 2)
  const slotH = height - margin * 2
  for (let index = 0; index < 2; index++) {
    const pageNo = props.pages[index] || 0
    const x = margin + index * (slotW + gap)
    ctx.fillStyle = '#ffffff'
    ctx.fillRect(x, margin, slotW, slotH)
    if (pageNo <= 0) {
      ctx.save()
      ctx.font = '18px -apple-system, BlinkMacSystemFont, "PingFang SC", sans-serif'
      ctx.fillStyle = '#94a3b8'
      ctx.textAlign = 'center'
      ctx.fillText('空白', x + slotW / 2, margin + slotH / 2)
      ctx.restore()
      continue
    }
    try {
      const page = await doc.getPage(pageNo)
      const viewport = page.getViewport({ scale: 1 })
      const offscreen = document.createElement('canvas')
      offscreen.width = Math.max(1, Math.floor(viewport.width))
      offscreen.height = Math.max(1, Math.floor(viewport.height))
      const offCtx = offscreen.getContext('2d')
      if (!offCtx) continue
      await page.render({ canvasContext: offCtx, viewport }).promise
      if (token !== renderToken) return
      const scale = Math.min(slotW / offscreen.width, slotH / offscreen.height)
      const drawW = Math.floor(offscreen.width * scale)
      const drawH = Math.floor(offscreen.height * scale)
      ctx.drawImage(offscreen, x + Math.floor((slotW - drawW) / 2), margin + Math.floor((slotH - drawH) / 2), drawW, drawH)
      ctx.save()
      ctx.font = '18px -apple-system, BlinkMacSystemFont, "PingFang SC", sans-serif'
      ctx.fillStyle = '#94a3b8'
      ctx.textAlign = 'center'
      ctx.fillText(`第 ${pageNo} 页`, x + slotW / 2, margin + slotH - 12)
      ctx.restore()
    } catch {
      /* blank */
    }
  }
  ctx.strokeStyle = '#cbd5e1'
  ctx.lineWidth = 1
  ctx.setLineDash([5, 5])
  ctx.beginPath()
  ctx.moveTo(width / 2, margin)
  ctx.lineTo(width / 2, height - margin)
  ctx.stroke()
}
</script>

<template>
  <div class="reading-preview"><canvas ref="canvas" /></div>
</template>

<style scoped>
.reading-preview { width:100%; background:#fff; border:1px solid var(--line); box-shadow:0 8px 24px rgba(15,23,42,.10); }
.reading-preview canvas { display:block; width:100%; height:auto; }
</style>
