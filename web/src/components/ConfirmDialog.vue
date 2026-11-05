<script setup lang="ts">
withDefaults(
  defineProps<{
    open: boolean
    title: string
    message: string
    confirmText?: string
    danger?: boolean
    busy?: boolean
  }>(),
  {
    confirmText: '确定',
    danger: false,
    busy: false,
  },
)

defineEmits<{
  (e: 'close'): void
  (e: 'confirm'): void
}>()
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="modal-overlay" @click.self="$emit('close')">
      <div class="modal confirm-modal">
        <div class="modal-head">
          <div>
            <h2 class="card-title">{{ title }}</h2>
            <p class="card-desc confirm-message">{{ message }}</p>
          </div>
          <button class="modal-close" @click="$emit('close')">×</button>
        </div>
        <div class="modal-foot">
          <span style="flex: 1"></span>
          <button class="btn btn-ghost" @click="$emit('close')">取消</button>
          <button
            class="btn"
            :class="danger ? 'btn-danger' : 'btn-primary'"
            :disabled="busy"
            @click="$emit('confirm')"
          >
            {{ busy ? '处理中…' : confirmText }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
