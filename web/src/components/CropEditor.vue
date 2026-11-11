<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

const props = defineProps<{
  src: string
  title?: string
  aspect?: number | null
}>()
const emit = defineEmits<{
  (e: 'confirm', blob: Blob): void
  (e: 'cancel'): void
}>()

const stage = ref<HTMLCanvasElement | null>(null)
const stageW = ref(720)
const stageH = ref(500)
const img = ref<HTMLImageElement | null>(null)
const workW = ref(0)
const workH = ref(0)
const rotation = ref(0)
const fine = ref(0)
const flipH = ref(false)
const flipV = ref(false)
const crop = ref({ x: 0, y: 0, w: 0, h: 0 })
const scale = ref(1)
const offsetX = ref(0)
const offsetY = ref(0)

let workCanvas: HTMLCanvasElement | null = null
let drag: {
  mode: string
  startX: number
  startY: number
  orig: { x: number; y: number; w: number; h: number }
} | null = null
let workTimer: number | undefined

const displayW = computed(() => workW.value * scale.value)
const displayH = computed(() => workH.value * scale.value)
const aspectLabel = computed(() => {
  if (!props.aspect) return ''
  return Math.abs(props.aspect - 85.6 / 54) < 0.01 ? '85.6 : 54' : props.aspect.toFixed(2)
})
const cropStyle = computed(() => ({
  left: `${crop.value.x}px`,
  top: `${crop.value.y}px`,
  width: `${crop.value.w}px`,
  height: `${crop.value.h}px`,
}))

onMounted(() => {
  stageW.value = Math.max(280, Math.min(720, window.innerWidth - 80))
  stageH.value = Math.max(240, Math.min(500, Math.round(window.innerHeight * 0.52)))
  const image = new Image()
  image.onload = () => {
    img.value = image
    buildWork()
  }
  image.onerror = () => emit('cancel')
  image.src = props.src
})

function scheduleWork() {
  window.clearTimeout(workTimer)
  workTimer = window.setTimeout(buildWork, 120)
}

function buildWork() {
  if (!img.value) return
  const image = img.value
  const maxEdge = 2400
  const baseScale = Math.min(1, maxEdge / Math.max(image.naturalWidth, image.naturalHeight))
  const w = image.naturalWidth * baseScale
  const h = image.naturalHeight * baseScale
  const angle = ((rotation.value + fine.value) * Math.PI) / 180
  const cos = Math.abs(Math.cos(angle))
  const sin = Math.abs(Math.sin(angle))
  const outW = Math.max(1, Math.round(w * cos + h * sin))
  const outH = Math.max(1, Math.round(w * sin + h * cos))

  const canvas = document.createElement('canvas')
  canvas.width = outW
  canvas.height = outH
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  ctx.fillStyle = '#fff'
  ctx.fillRect(0, 0, outW, outH)
  ctx.translate(outW / 2, outH / 2)
  ctx.rotate(angle)
  ctx.scale(flipH.value ? -1 : 1, flipV.value ? -1 : 1)
  ctx.drawImage(image, -w / 2, -h / 2, w, h)
  workCanvas = canvas
  workW.value = outW
  workH.value = outH
  fitStage(true)
}

function fitStage(resetCrop: boolean) {
  if (!workW.value || !workH.value) return
  scale.value = Math.min(stageW.value / workW.value, stageH.value / workH.value)
  offsetX.value = (stageW.value - workW.value * scale.value) / 2
  offsetY.value = (stageH.value - workH.value * scale.value) / 2
  if (resetCrop) {
    const bounds = {
      x: offsetX.value,
      y: offsetY.value,
      w: workW.value * scale.value,
      h: workH.value * scale.value,
    }
    if (props.aspect) {
      // Start at 60% of the largest usable aspect box. A full-width default
      // leaves almost no room to move the crop box, which feels broken on
      // phone photos where the card occupies only part of the frame.
      const fitW = Math.min(bounds.w, bounds.h * props.aspect)
      const w = fitW * 0.6
      const h = w / props.aspect
      crop.value = {
        x: bounds.x + (bounds.w - w) / 2,
        y: bounds.y + (bounds.h - h) / 2,
        w,
        h,
      }
    } else {
      crop.value = {
        x: bounds.x + bounds.w * 0.15,
        y: bounds.y + bounds.h * 0.15,
        w: bounds.w * 0.7,
        h: bounds.h * 0.7,
      }
    }
  }
  drawStage()
}

function drawStage() {
  const canvas = stage.value
  if (!canvas || !workCanvas) return
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  ctx.clearRect(0, 0, stageW.value, stageH.value)
  ctx.drawImage(workCanvas, offsetX.value, offsetY.value, displayW.value, displayH.value)
}

function startDrag(event: PointerEvent, mode: string) {
  event.preventDefault()
  event.stopPropagation()
  const target = event.currentTarget as HTMLElement
  target.setPointerCapture?.(event.pointerId)
  drag = { mode, startX: event.clientX, startY: event.clientY, orig: { ...crop.value } }
  window.addEventListener('pointermove', onMove)
  window.addEventListener('pointerup', stopDrag, { once: true })
}

function onMove(event: PointerEvent) {
  if (!drag) return
  const dx = event.clientX - drag.startX
  const dy = event.clientY - drag.startY
  const orig = drag.orig
  const leftLimit = offsetX.value
  const topLimit = offsetY.value
  const rightLimit = offsetX.value + displayW.value
  const bottomLimit = offsetY.value + displayH.value
  let left = orig.x
  let top = orig.y
  let right = orig.x + orig.w
  let bottom = orig.y + orig.h

  if (drag.mode === 'move') {
    left = clamp(orig.x + dx, leftLimit, rightLimit - orig.w)
    top = clamp(orig.y + dy, topLimit, bottomLimit - orig.h)
    crop.value = { x: left, y: top, w: orig.w, h: orig.h }
    return
  }

  if (drag.mode.includes('w')) left = Math.min(orig.x + dx, right - 40)
  if (drag.mode.includes('e')) right = Math.max(orig.x + orig.w + dx, left + 40)
  if (drag.mode.includes('n')) top = Math.min(orig.y + dy, bottom - 40)
  if (drag.mode.includes('s')) bottom = Math.max(orig.y + orig.h + dy, top + 40)

  left = clamp(left, leftLimit, rightLimit - 40)
  right = clamp(right, left + 40, rightLimit)
  top = clamp(top, topLimit, bottomLimit - 40)
  bottom = clamp(bottom, top + 40, bottomLimit)

  if (props.aspect) {
    // Keep the requested aspect. Corner handles use the dominant axis; edge
    // handles adjust the other dimension around the fixed opposite edge.
    const aspect = props.aspect
    const isCorner = drag.mode.length === 2
    if (isCorner) {
      let w = right - left
      let h = bottom - top
      if (w / h > aspect) {
        w = h * aspect
        if (drag.mode.includes('w')) left = right - w
        else right = left + w
      } else {
        h = w / aspect
        if (drag.mode.includes('n')) top = bottom - h
        else bottom = top + h
      }
    } else if (drag.mode === 'e' || drag.mode === 'w') {
      const w = right - left
      const h = w / aspect
      if (drag.mode === 'w') {
        top = clamp(bottom - h, topLimit, bottomLimit - 40)
        bottom = top + h
      } else {
        bottom = clamp(top + h, top + 40, bottomLimit)
        top = bottom - h
      }
    } else {
      const h = bottom - top
      const w = h * aspect
      if (drag.mode === 'n') {
        left = clamp(right - w, leftLimit, rightLimit - 40)
        right = left + w
      } else {
        right = clamp(left + w, left + 40, rightLimit)
        left = right - w
      }
    }
  }

  crop.value = { x: left, y: top, w: right - left, h: bottom - top }
}

function stopDrag() {
  drag = null
  window.removeEventListener('pointermove', onMove)
}

function clamp(v: number, min: number, max: number) {
  return Math.min(Math.max(v, min), max)
}

function rotate(step: number) {
  rotation.value = (rotation.value + step + 360) % 360
  fine.value = 0
  buildWork()
}

function flipHorizontal() {
  flipH.value = !flipH.value
  buildWork()
}

function flipVertical() {
  flipV.value = !flipV.value
  buildWork()
}

function reset() {
  rotation.value = 0
  fine.value = 0
  flipH.value = false
  flipV.value = false
  buildWork()
}

function confirm() {
  if (!workCanvas) return
  const sx = (crop.value.x - offsetX.value) / scale.value
  const sy = (crop.value.y - offsetY.value) / scale.value
  const sw = crop.value.w / scale.value
  const sh = crop.value.h / scale.value
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(1, Math.round(sw))
  canvas.height = Math.max(1, Math.round(sh))
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  ctx.fillStyle = '#fff'
  ctx.fillRect(0, 0, canvas.width, canvas.height)
  ctx.drawImage(workCanvas, sx, sy, sw, sh, 0, 0, canvas.width, canvas.height)
  canvas.toBlob((blob) => {
    if (blob) emit('confirm', blob)
  }, 'image/jpeg', 0.92)
}

const handles = ['nw', 'ne', 'sw', 'se', 'n', 's', 'w', 'e']
</script>

<template>
  <div class="crop-overlay" @click.self="emit('cancel')">
    <div class="crop-modal">
      <header class="crop-head">
        <div>
          <div class="crop-title">{{ title || '裁剪图片' }}</div>
          <div class="crop-sub">拖动框内移动，拖动四角/四边缩放；可使用旋转、翻转和比例锁定</div>
        </div>
        <button class="crop-close" type="button" @click="emit('cancel')">×</button>
      </header>
      <div class="crop-body">
        <div class="crop-stage">
          <canvas ref="stage" :width="stageW" :height="stageH" />
          <div class="crop-dim" :style="cropStyle">
            <div class="crop-box" @pointerdown="startDrag($event, 'move')">
              <div
                v-for="handle in handles"
                :key="handle"
                class="crop-handle"
                :class="'h-' + handle"
                @pointerdown.stop="startDrag($event, handle)"
              />
              <div class="crop-grid g1" /><div class="crop-grid g2" /><div class="crop-grid g3" /><div class="crop-grid g4" />
            </div>
          </div>
        </div>
        <aside class="crop-tools">
          <div class="tool-title">旋转</div>
          <div class="tool-grid">
            <button class="tool-btn" type="button" @click="rotate(-90)">↺ 左转 90°</button>
            <button class="tool-btn" type="button" @click="rotate(90)">↻ 右转 90°</button>
          </div>
          <div class="tool-title">微调角度</div>
          <input v-model.number="fine" class="angle-range" type="range" min="-15" max="15" step="1" @input="scheduleWork" />
          <div class="range-labels"><span>-15°</span><span>{{ fine }}°</span><span>+15°</span></div>
          <div class="tool-title">翻转</div>
          <div class="tool-grid">
            <button class="tool-btn" type="button" @click="flipHorizontal">⇋ 水平翻转</button>
            <button class="tool-btn" type="button" @click="flipVertical">⇅ 垂直翻转</button>
            <button class="tool-btn wide" type="button" @click="reset">↺ 重置</button>
          </div>
          <div v-if="aspect" class="ratio-row"><span class="ratio-dot" />锁定比例 {{ aspectLabel }}</div>
          <div class="crop-hint">裁剪结果按原图分辨率保存，不会因为屏幕缩放变模糊。</div>
        </aside>
      </div>
      <footer class="crop-foot">
        <button class="btn btn-ghost" type="button" @click="emit('cancel')">取消</button>
        <button class="btn btn-primary" type="button" @click="confirm">确认裁剪</button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.crop-overlay {
  position: fixed; inset: 0; z-index: 200; background: rgba(15, 23, 42, .42);
  display: flex; align-items: center; justify-content: center; padding: 24px;
}
.crop-modal { width: min(1080px, 96vw); height: min(720px, 92vh); background: #fff; border-radius: 20px; box-shadow: 0 30px 80px rgba(15, 23, 42, .35); display: flex; flex-direction: column; overflow: hidden; }
.crop-head { padding: 22px 28px 16px; border-bottom: 1px solid var(--line); display: flex; justify-content: space-between; }
.crop-title { font-size: 19px; font-weight: 700; }
.crop-sub { font-size: 12px; color: var(--text-2); margin-top: 5px; }
.crop-close { width: 34px; height: 34px; border: 0; border-radius: 9px; background: var(--surface-2); color: var(--text-2); font-size: 18px; cursor: pointer; }
.crop-body { flex: 1; display: flex; min-height: 0; }
.crop-stage { flex: 1; background: #dfe3ec; display: flex; align-items: center; justify-content: center; position: relative; overflow: hidden; }
.crop-stage canvas { display: block; }
.crop-dim { position: absolute; box-shadow: 0 0 0 9999px rgba(15, 23, 42, .42); }
.crop-box { position: absolute; inset: 0; border: 2px solid var(--accent); cursor: move; touch-action: none; }
.crop-handle { position: absolute; width: 12px; height: 12px; background: #fff; border: 2px solid var(--accent); border-radius: 3px; z-index: 2; }
.h-nw { left: -7px; top: -7px; cursor: nwse-resize; }
.h-ne { right: -7px; top: -7px; cursor: nesw-resize; }
.h-sw { left: -7px; bottom: -7px; cursor: nesw-resize; }
.h-se { right: -7px; bottom: -7px; cursor: nwse-resize; }
.h-n { left: 50%; top: -7px; margin-left: -6px; cursor: ns-resize; }
.h-s { left: 50%; bottom: -7px; margin-left: -6px; cursor: ns-resize; }
.h-w { left: -7px; top: 50%; margin-top: -6px; cursor: ew-resize; }
.h-e { right: -7px; top: 50%; margin-top: -6px; cursor: ew-resize; }
.crop-grid { position: absolute; background: rgba(255,255,255,.5); pointer-events: none; }
.g1 { left: 33.3%; top: 0; width: 1px; height: 100%; }
.g2 { left: 66.6%; top: 0; width: 1px; height: 100%; }
.g3 { top: 33.3%; left: 0; height: 1px; width: 100%; }
.g4 { top: 66.6%; left: 0; height: 1px; width: 100%; }
.crop-tools { width: 280px; border-left: 1px solid var(--line); padding: 20px; overflow-y: auto; }
.tool-title { font-size: 12px; color: var(--text-3); font-weight: 700; margin: 4px 0 9px; }
.tool-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-bottom: 16px; }
.tool-btn { height: 38px; border: 1px solid var(--line); border-radius: 9px; background: #fff; font-size: 12px; color: var(--text-2); cursor: pointer; }
.tool-btn.wide { grid-column: span 2; }
.angle-range { width: 100%; accent-color: var(--accent); }
.range-labels { display: flex; justify-content: space-between; font-size: 11px; color: var(--text-3); margin: 6px 0 16px; }
.ratio-row { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-2); background: var(--surface-2); border-radius: 9px; padding: 10px 12px; margin-bottom: 14px; }
.ratio-dot { width: 14px; height: 14px; border: 2px solid var(--accent); border-radius: 4px; background: var(--accent-soft); }
.crop-hint { font-size: 11px; color: var(--text-3); line-height: 1.7; background: var(--surface-2); border-radius: 10px; padding: 11px 12px; }
.crop-foot { padding: 14px 26px; border-top: 1px solid var(--line); display: flex; justify-content: flex-end; gap: 10px; }
@media (max-width: 760px) {
  .crop-overlay { padding: 10px; }
  .crop-modal { height: 94vh; }
  .crop-body { flex-direction: column; }
  .crop-stage { min-height: 280px; }
  .crop-tools { width: 100%; border-left: 0; border-top: 1px solid var(--line); }
}
</style>
