<script setup lang="ts">
import { computed, ref, watch } from 'vue'

export interface CollageImage { id: number; url: string; name: string }
interface CollageItem { uid: number; imageId: number; x: number; y: number; w: number; h: number; rotation: number; fit: 'cover' | 'contain' }
interface CollageSheet { items: CollageItem[] }

const props = defineProps<{
  images: CollageImage[]
  paperWidthMm: number
  paperHeightMm: number
}>()
const emit = defineEmits<{
  (e: 'update:layout', value: { sheets: { items: { index: number; x_mm: number; y_mm: number; w_mm: number; h_mm: number; rotation: number }[] }[] }): void
}>()

const sheets = ref<CollageSheet[]>([{ items: [] }])
const activeSheet = ref(0)
const selected = ref<CollageItem | null>(null)
const lockAspect = ref(true)
const fillMode = ref<'cover' | 'contain'>('cover')
const stage = ref<HTMLDivElement | null>(null)
const dragging = ref<{ mode: 'move' | 'resize'; startX: number; startY: number; item: CollageItem; ow: number; oh: number; vw: number; vh: number } | null>(null)
const rotating = ref<{ item: CollageItem; cx: number; cy: number; startAngle: number; startRotation: number } | null>(null)
const imageAspects = ref<Record<number, number>>({})
let nextUid = 1

const active = computed(() => sheets.value[activeSheet.value] || sheets.value[0])
const imageById = computed(() => new Map(props.images.map((img) => [img.id, img])))
const stageAspect = computed(() => `${props.paperWidthMm} / ${props.paperHeightMm}`)

function visualWidth(item: CollageItem): number {
  const rad = item.rotation * Math.PI / 180
  return Math.abs(item.w * Math.cos(rad)) + Math.abs(item.h * Math.sin(rad))
}

function visualHeight(item: CollageItem): number {
  const rad = item.rotation * Math.PI / 180
  return Math.abs(item.w * Math.sin(rad)) + Math.abs(item.h * Math.cos(rad))
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), Math.max(min, max))
}

watch(() => props.images, (images) => {
  const valid = new Set(images.map((img) => img.id))
  for (const sheet of sheets.value) {
    sheet.items = sheet.items.filter((item) => valid.has(item.imageId))
  }
  for (const image of images) {
    if (imageAspects.value[image.id]) continue
    const img = new Image()
    img.onload = () => { imageAspects.value[image.id] = img.naturalWidth / img.naturalHeight || 1 }
    img.src = image.url
  }
  emitLayout()
}, { deep: true })

watch(fillMode, (mode) => {
  for (const sheet of sheets.value) {
    for (const item of sheet.items) item.fit = mode
  }
  emitLayout()
})

function emitLayout() {
  emit('update:layout', {
    sheets: sheets.value.map((sheet) => ({
      items: sheet.items
        .map((item) => {
          const index = props.images.findIndex((image) => image.id === item.imageId)
          return { index, x_mm: item.x, y_mm: item.y, w_mm: item.w, h_mm: item.h, rotation: item.rotation, fit: item.fit }
        })
        .filter((item) => item.index >= 0),
    })),
  })
}

async function addImage(image: CollageImage) {
  if (!imageAspects.value[image.id]) {
    imageAspects.value[image.id] = await new Promise<number>((resolve) => {
      const img = new Image()
      img.onload = () => resolve(img.naturalWidth / img.naturalHeight || 1)
      img.onerror = () => resolve(1)
      img.src = image.url
    })
  }
  const aspect = imageAspects.value[image.id] || 1
  const maxW = props.paperWidthMm * 0.45
  const maxH = props.paperHeightMm * 0.45
  let w = maxW
  let h = w / aspect
  if (h > maxH) {
    h = maxH
    w = h * aspect
  }
  const count = active.value.items.length
  const offset = Math.min(count * 6, 30)
  const item: CollageItem = {
    uid: nextUid++,
    imageId: image.id,
    x: Math.max(0, (props.paperWidthMm - w) / 2 + offset - 15),
    y: Math.max(0, (props.paperHeightMm - h) / 2 + offset - 15),
    w,
    h,
    rotation: 0,
    fit: fillMode.value,
  }
  item.fit = fillMode.value
  active.value.items.push(item)
  selected.value = item
  emitLayout()
}

function addSheet() {
  sheets.value.push({ items: [] })
  activeSheet.value = sheets.value.length - 1
  selected.value = null
  emitLayout()
}

function removeSheet(index: number) {
  if (sheets.value.length <= 1) return
  sheets.value.splice(index, 1)
  activeSheet.value = Math.min(activeSheet.value, sheets.value.length - 1)
  selected.value = null
  emitLayout()
}

function removeItem(item: CollageItem) {
  active.value.items = active.value.items.filter((candidate) => candidate !== item)
  if (selected.value === item) selected.value = null
  emitLayout()
}

function point(event: PointerEvent) {
  const rect = stage.value?.getBoundingClientRect()
  if (!rect) return null
  return {
    x: (event.clientX - rect.left) * props.paperWidthMm / rect.width,
    y: (event.clientY - rect.top) * props.paperHeightMm / rect.height,
    scale: rect.width / props.paperWidthMm,
  }
}

function startMove(event: PointerEvent, item: CollageItem) {
  event.stopPropagation()
  const p = point(event)
  if (!p) return
  selected.value = item
  // Bring the item being dragged to the front of this sheet's paint order.
  const index = active.value.items.indexOf(item)
  if (index >= 0 && index !== active.value.items.length - 1) {
    active.value.items.splice(index, 1)
    active.value.items.push(item)
    emitLayout()
  }
  dragging.value = { mode: 'move', startX: p.x, startY: p.y, item, ow: item.w, oh: item.h, vw: visualWidth(item), vh: visualHeight(item) }
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', stopDrag, { once: true })
}

function startResize(event: PointerEvent, item: CollageItem) {
  event.stopPropagation()
  const p = point(event)
  if (!p) return
  selected.value = item
  dragging.value = { mode: 'resize', startX: p.x, startY: p.y, item, ow: item.w, oh: item.h, vw: visualWidth(item), vh: visualHeight(item) }
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', stopDrag, { once: true })
}

function onPointerMove(event: PointerEvent) {
  const drag = dragging.value
  const p = point(event)
  if (!drag || !p) return
  const dx = p.x - drag.startX
  const dy = p.y - drag.startY
  if (drag.mode === 'move') {
    drag.item.x = clamp(drag.item.x + dx, 0, props.paperWidthMm - visualWidth(drag.item))
    drag.item.y = clamp(drag.item.y + dy, 0, props.paperHeightMm - visualHeight(drag.item))
    // Use the pointer delta once; store the new baseline for the next move event.
    drag.startX = p.x
    drag.startY = p.y
  } else {
    const maxW = Math.max(10, props.paperWidthMm - drag.item.x)
    const maxH = Math.max(10, props.paperHeightMm - drag.item.y)
    let targetW = Math.max(10, drag.vw + dx)
    let targetH = lockAspect.value ? targetW * (drag.vh / drag.vw) : Math.max(10, drag.vh + dy)
    if (targetW > maxW) {
      targetW = maxW
      if (lockAspect.value) targetH = targetW * (drag.vh / drag.vw)
    }
    if (targetH > maxH) {
      targetH = maxH
      if (lockAspect.value) targetW = targetH * (drag.vw / drag.vh)
    }
    const sx = targetW / drag.vw
    const sy = targetH / drag.vh
    drag.item.w = Math.max(10, drag.ow * sx)
    drag.item.h = Math.max(10, drag.oh * sy)
    drag.startX = p.x
    drag.startY = p.y
    drag.vw = targetW
    drag.vh = targetH
    drag.ow = drag.item.w
    drag.oh = drag.item.h
  }
  emitLayout()
}

function stopDrag() {
  dragging.value = null
  window.removeEventListener('pointermove', onPointerMove)
}

function startRotate(event: PointerEvent, item: CollageItem) {
  event.stopPropagation()
  event.preventDefault()
  const p = point(event)
  if (!p) return
  selected.value = item
  const cx = item.x + visualWidth(item) / 2
  const cy = item.y + visualHeight(item) / 2
  rotating.value = {
    item,
    cx,
    cy,
    startAngle: Math.atan2(p.y - cy, p.x - cx),
    startRotation: item.rotation,
  }
  window.addEventListener('pointermove', onRotateMove)
  window.addEventListener('pointerup', stopRotate, { once: true })
}

function onRotateMove(event: PointerEvent) {
  const state = rotating.value
  const p = point(event)
  if (!state || !p) return
  const angle = Math.atan2(p.y - state.cy, p.x - state.cx)
  let degrees = state.startRotation + (angle - state.startAngle) * 180 / Math.PI
  while (degrees > 180) degrees -= 360
  while (degrees < -180) degrees += 360
  state.item.rotation = Math.round(degrees)
  emitLayout()
}

function stopRotate() {
  rotating.value = null
  window.removeEventListener('pointermove', onRotateMove)
}

function rotateSelected(value: number) {
  if (!selected.value) return
  const item = selected.value
  const oldW = visualWidth(item)
  const oldH = visualHeight(item)
  const cx = item.x + oldW / 2
  const cy = item.y + oldH / 2
  item.rotation = value
  const newW = visualWidth(item)
  const newH = visualHeight(item)
  item.x = clamp(cx - newW / 2, 0, props.paperWidthMm - newW)
  item.y = clamp(cy - newH / 2, 0, props.paperHeightMm - newH)
  emitLayout()
}

function setQuickAngle(value: number) {
  rotateSelected(value)
}
</script>

<template>
  <div class="collage">
    <div class="collage-tray">
      <div class="collage-title">图片素材</div>
      <div class="collage-thumbs">
        <button v-for="image in images" :key="image.id" class="collage-thumb" type="button" @click="addImage(image)">
          <img :src="image.url" :alt="image.name" />
          <span>{{ image.name }}</span>
        </button>
        <div v-if="!images.length" class="collage-empty">先添加图片</div>
      </div>
    </div>
    <div class="collage-main">
      <div class="collage-toolbar">
        <div class="collage-tabs">
          <button v-for="(sheet, index) in sheets" :key="index" type="button" :class="{ on: activeSheet === index }" @click="activeSheet = index; selected = null">
            第 {{ index + 1 }} 张
            <span v-if="sheets.length > 1" @click.stop="removeSheet(index)">×</span>
          </button>
          <button class="collage-add-sheet" type="button" @click="addSheet">＋ 纸张</button>
        </div>
        <div class="collage-tools">
          <label class="collage-lock"><input v-model="lockAspect" type="checkbox" /> 等比缩放</label>
          <select v-model="fillMode" class="collage-fill"><option value="cover">填满（裁剪）</option><option value="contain">适应（留白）</option></select>
          <span class="collage-hint">拖动图片移动，拖右下角缩放</span>
        </div>
      </div>
      <div v-if="selected" class="collage-rotate">
        <label>旋转</label>
        <input :value="selected.rotation" type="range" min="-180" max="180" step="1" @input="rotateSelected(Number(($event.target as HTMLInputElement).value))" />
        <span>{{ Math.round(selected.rotation) }}°</span>
        <div class="quick-angles">
          <button v-for="angle in [0, 90, 180, 270]" :key="angle" type="button" @click="setQuickAngle(angle)">{{ angle }}°</button>
        </div>
      </div>
      <div ref="stage" class="collage-stage" :style="{ aspectRatio: stageAspect }" @click.self="selected = null">
        <div
          v-for="item in active.items"
          :key="item.uid"
          class="collage-item"
          :class="{ selected: selected === item }"
          :style="{ left: (item.x / paperWidthMm * 100) + '%', top: (item.y / paperHeightMm * 100) + '%', width: (visualWidth(item) / paperWidthMm * 100) + '%', height: (visualHeight(item) / paperHeightMm * 100) + '%' }"
          @pointerdown="startMove($event, item)"
          @click.stop="selected = item"
        >
          <img v-if="imageById.get(item.imageId)" class="collage-image" :src="imageById.get(item.imageId)!.url" :alt="imageById.get(item.imageId)!.name" :style="{ width: (item.w / visualWidth(item) * 100) + '%', height: (item.h / visualHeight(item) * 100) + '%', objectFit: item.fit, transform: `translate(-50%, -50%) rotate(${item.rotation}deg)` }" />
          <button class="collage-delete" type="button" @pointerdown.stop @click.stop="removeItem(item)">×</button>
          <span class="collage-resize" @pointerdown.stop="startResize($event, item)" />
          <span class="collage-rotate-handle" title="拖动旋转" @pointerdown.stop="startRotate($event, item)">↻</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.collage { display:grid; grid-template-columns:200px minmax(0,1fr); gap:16px; margin-top:16px; }
.collage-tray { border:1px solid var(--line); border-radius:12px; padding:10px; background:var(--surface-2); min-width:0; }
.collage-title { font-size:12px; font-weight:700; color:var(--text-2); margin-bottom:8px; }
.collage-thumbs { display:grid; grid-template-columns:1fr 1fr; gap:8px; }
.collage-thumb { border:1px solid var(--line); border-radius:8px; background:#fff; padding:4px; text-align:left; cursor:pointer; min-width:0; }
.collage-thumb img { width:100%; height:62px; object-fit:cover; border-radius:5px; display:block; }
.collage-thumb span { display:block; font-size:9px; color:var(--text-3); overflow:hidden; text-overflow:ellipsis; white-space:nowrap; margin-top:3px; }
.collage-empty { grid-column:1/-1; padding:20px 0; text-align:center; color:var(--text-3); font-size:11px; }
.collage-main { min-width:0; }
.collage-toolbar { display:flex; justify-content:space-between; align-items:flex-start; margin-bottom:8px; gap:8px; flex-wrap:wrap; }
.collage-tabs { display:flex; gap:4px; flex:1 1 auto; min-width:0; flex-wrap:nowrap; overflow-x:auto; padding-bottom:2px; }
.collage-tabs button { flex:0 0 auto; white-space:nowrap; border:1px solid var(--line); background:#fff; border-radius:7px; padding:5px 9px; font-size:11px; color:var(--text-2); cursor:pointer; }
.collage-tabs button.on { color:var(--accent); border-color:#c7d2fe; background:#eef2ff; }
.collage-tabs button span { margin-left:5px; color:var(--text-3); }
.collage-add-sheet { border-style:dashed !important; }
.collage-tools { display:flex; align-items:center; gap:10px; flex:0 0 auto; flex-wrap:wrap; }
.collage-lock { display:flex; align-items:center; gap:4px; font-size:10px; color:var(--text-2); white-space:nowrap; cursor:pointer; }
.collage-lock input { accent-color:var(--accent); width:13px; height:13px; }
.collage-hint { font-size:10px; color:var(--text-3); white-space:nowrap; }
.collage-fill { border:1px solid var(--line); border-radius:6px; background:#fff; color:var(--text-2); font-size:10px; padding:3px 5px; }
.collage-stage { position:relative; width:100%; background:#fff; box-shadow:0 6px 20px rgba(15,23,42,.12); overflow:hidden; border:1px solid var(--line); }
.collage-item { position:absolute; z-index:1; border:1px solid transparent; box-sizing:border-box; cursor:move; user-select:none; touch-action:none; }
.collage-item.selected { z-index:5; border-color:var(--accent); }
.collage-image { position:absolute; left:50%; top:50%; display:block; pointer-events:none; background:#fff; transform-origin:center; }
.collage-delete { position:absolute; right:-9px; top:-9px; width:18px; height:18px; border:0; border-radius:50%; background:#ef4444; color:#fff; font-size:12px; line-height:18px; text-align:center; cursor:pointer; }
.collage-resize { position:absolute; right:-6px; bottom:-6px; width:12px; height:12px; border:2px solid var(--accent); background:#fff; border-radius:3px; cursor:nwse-resize; touch-action:none; }
.collage-rotate-handle { position:absolute; left:50%; top:-30px; width:24px; height:24px; margin-left:-12px; border:2px solid var(--accent); background:#fff; color:var(--accent); border-radius:50%; font-size:16px; line-height:20px; text-align:center; cursor:grab; touch-action:none; user-select:none; box-shadow:0 2px 6px rgba(15,23,42,.12); }
.collage-rotate { display:flex; align-items:center; gap:10px; margin-bottom:8px; padding:7px 9px; border:1px solid var(--line); border-radius:8px; background:#fafbfe; font-size:11px; color:var(--text-3); }
.collage-rotate input { flex:1; accent-color:var(--accent); }
.quick-angles { display:flex; gap:4px; }
.quick-angles button { border:1px solid var(--line); background:#fff; border-radius:6px; padding:3px 6px; font-size:10px; color:var(--text-2); cursor:pointer; }
@media (max-width:760px) {
  .collage { grid-template-columns:1fr; }
  .collage-toolbar { flex-direction:column; align-items:stretch; }
  .collage-tools { justify-content:space-between; }
  .collage-thumbs { grid-template-columns:repeat(4,1fr); }
  .collage-thumb img { height:44px; }
  .collage-hint { display:none; }
}
</style>
