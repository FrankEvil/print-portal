<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import type { PDFDocumentProxy } from 'pdfjs-dist'

const props = defineProps<{
  doc: PDFDocumentProxy | null
  sheet: { front: number[]; back: number[] } | null
  side: 'front' | 'back'
  paperWidthMm: number
  paperHeightMm: number
  pageCount: number
  sheetIndex: number
  totalSheets: number
}>()

const canvas = ref<HTMLCanvasElement | null>(null)
let renderToken = 0

watch(
  [() => props.doc, () => props.sheet, () => props.side, () => props.paperWidthMm, () => props.paperHeightMm],
  () => { void render() },
  { immediate: true },
)
onMounted(() => { void render() })
onUnmounted(() => { renderToken++ })

function drawPageNumber(ctx: CanvasRenderingContext2D, pageNo: number, x: number, y: number, width: number, height: number) {
  if (pageNo <= 0) return
  ctx.save()
  ctx.font = '20px -apple-system, BlinkMacSystemFont, "PingFang SC", sans-serif'
  ctx.fillStyle = '#94a3b8'
  ctx.textAlign = 'left'
  ctx.fillText(`第 ${pageNo} 页`, x + 12, y + height - 12)
  ctx.restore()
}

function drawBadge(ctx: CanvasRenderingContext2D, text: string, x: number, y: number, width: number, background: string, color: string) {
  ctx.save()
  ctx.font = '600 18px -apple-system, BlinkMacSystemFont, "PingFang SC", sans-serif'
  const textWidth = ctx.measureText(text).width
  const badgeWidth = textWidth + 20
  ctx.fillStyle = background
  ctx.fillRect(x + width - badgeWidth - 10, y + 10, badgeWidth, 30)
  ctx.fillStyle = color
  ctx.textAlign = 'center'
  ctx.fillText(text, x + width - badgeWidth / 2 - 10, y + 32)
  ctx.restore()
}

async function render() {
  const token = ++renderToken
  const element = canvas.value
  const doc = props.doc
  const sheet = props.sheet
  if (!element || !doc || !sheet) return
  const width = 1200
  const height = Math.max(1, Math.round(width * props.paperHeightMm / props.paperWidthMm))
  // Draw the whole sheet on an offscreen canvas first; the visible canvas is
  // only touched once, at the very end, so its previous content never blanks.
  const offscreen = document.createElement('canvas')
  offscreen.width = width
  offscreen.height = height
  const ctx = offscreen.getContext('2d')
  if (!ctx) return
  ctx.fillStyle = '#ffffff'
  ctx.fillRect(0, 0, width, height)
  const margin = Math.round(width * 0.015)
  const gap = Math.round(width * 0.012)
  const slotW = Math.floor((width - margin * 2 - gap) / 2)
  const slotH = height - margin * 2
  const pages = props.side === 'front' ? sheet.front : sheet.back
  for (let index = 0; index < 2; index++) {
    const pageNo = pages[index] || 0
    const x = margin + index * (slotW + gap)
    if (pageNo <= 0) {
      ctx.fillStyle = '#f7f9fc'
      ctx.fillRect(x, margin, slotW, slotH)
      ctx.save()
      ctx.strokeStyle = '#dbe3ee'
      ctx.lineWidth = 1.5
      ctx.setLineDash([6, 5])
      ctx.strokeRect(x + 0.75, margin + 0.75, slotW - 1.5, slotH - 1.5)
      ctx.setLineDash([])
      ctx.font = '20px -apple-system, BlinkMacSystemFont, "PingFang SC", sans-serif'
      ctx.fillStyle = '#a8b3c2'
      ctx.textAlign = 'center'
      ctx.fillText('空白补页', x + slotW / 2, margin + slotH / 2)
      ctx.restore()
      drawPageNumber(ctx, pageNo, x, margin, slotW, slotH)
      continue
    }
    try {
      const page = await doc.getPage(pageNo)
      if (token !== renderToken) return
      const viewport = page.getViewport({ scale: 1 })
      const pageCanvas = document.createElement('canvas')
      pageCanvas.width = Math.max(1, Math.floor(viewport.width))
      pageCanvas.height = Math.max(1, Math.floor(viewport.height))
      const pageCtx = pageCanvas.getContext('2d')
      if (!pageCtx) continue
      await page.render({ canvasContext: pageCtx, viewport }).promise
      if (token !== renderToken) return
      const scale = Math.min(slotW / pageCanvas.width, slotH / pageCanvas.height)
      const drawW = Math.floor(pageCanvas.width * scale)
      const drawH = Math.floor(pageCanvas.height * scale)
      ctx.drawImage(pageCanvas, x + Math.floor((slotW - drawW) / 2), margin + Math.floor((slotH - drawH) / 2), drawW, drawH)
      if (props.sheetIndex === 0 && props.side === 'front' && index === 1) {
        drawBadge(ctx, '封面', x, margin, slotW, '#dcfce7', '#15803d')
      } else if (props.sheetIndex === props.totalSheets - 1 && props.side === 'back' && index === 0) {
        drawBadge(ctx, '封底', x, margin, slotW, '#dbeafe', '#2563eb')
      }
      drawPageNumber(ctx, pageNo, x, margin, slotW, slotH)
    } catch {
      /* keep the slot blank if a page cannot be rendered */
    }
  }
  ctx.strokeStyle = '#cbd5e1'
  ctx.lineWidth = 1
  ctx.setLineDash([5, 5])
  ctx.beginPath()
  ctx.moveTo(width / 2, margin)
  ctx.lineTo(width / 2, height - margin)
  ctx.stroke()
  if (token !== renderToken) return
  element.width = width
  element.height = height
  const displayCtx = element.getContext('2d')
  if (!displayCtx) return
  displayCtx.drawImage(offscreen, 0, 0)
}
</script>

<template>
  <div class="booklet-preview">
    <canvas ref="canvas" />
    <div v-if="!doc || !sheet" class="booklet-placeholder">预览准备中…</div>
  </div>
</template>

<style scoped>
.booklet-preview { position: relative; width: 100%; background: #fff; border: 1px solid var(--line); box-shadow: 0 8px 24px rgba(15,23,42,.10); }
.booklet-preview canvas { display: block; width: 100%; height: auto; }
.booklet-placeholder { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; color: var(--text-3); font-size: 13px; }
</style>
