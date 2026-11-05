<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'

export interface SearchableOption {
  value: string
  label: string
  hint?: string
}

const props = defineProps<{
  modelValue: string
  options: SearchableOption[]
  placeholder?: string
  disabled?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

const open = ref(false)
const query = ref('')
const root = ref<HTMLElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)

const selectedLabel = computed(() => {
  const match = props.options.find((o) => o.value === props.modelValue)
  return match?.label || (props.modelValue ? props.modelValue : '')
})

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return props.options.slice(0, 300)
  return props.options
    .filter((o) => `${o.label} ${o.hint || ''} ${o.value}`.toLowerCase().includes(q))
    .slice(0, 300)
})

async function toggle() {
  if (props.disabled) return
  open.value = !open.value
  if (open.value) {
    query.value = ''
    await nextTick()
    searchInput.value?.focus()
  }
}

function choose(option: SearchableOption) {
  emit('update:modelValue', option.value)
  open.value = false
}

function onDocumentClick(event: MouseEvent) {
  if (root.value && !root.value.contains(event.target as Node)) open.value = false
}

onMounted(() => document.addEventListener('click', onDocumentClick))
onUnmounted(() => document.removeEventListener('click', onDocumentClick))
</script>

<template>
  <div ref="root" class="searchable-select" :class="{ disabled: props.disabled }">
    <button type="button" class="searchable-trigger" :disabled="props.disabled" @click="toggle">
      <span :class="{ placeholder: !selectedLabel }">{{ selectedLabel || props.placeholder || '请选择' }}</span>
      <span class="searchable-caret">▾</span>
    </button>

    <div v-if="open" class="searchable-panel">
      <input
        ref="searchInput"
        v-model="query"
        class="input searchable-input"
        placeholder="输入品牌、型号或文件名搜索"
        @keydown.esc="open = false"
      />
      <div class="searchable-list">
        <button
          v-for="o in filtered"
          :key="o.value"
          type="button"
          class="searchable-option"
          :class="{ selected: o.value === modelValue }"
          @click="choose(o)"
        >
          <span>{{ o.label }}</span>
          <small v-if="o.hint">{{ o.hint }}</small>
        </button>
        <div v-if="!filtered.length" class="searchable-empty">没有匹配型号</div>
      </div>
    </div>
  </div>
</template>
