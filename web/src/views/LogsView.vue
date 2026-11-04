<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { toast } from '../toast'

const kind = ref<'app' | 'cups'>('app')
const lines = ref<string[]>([])
const path = ref('')
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const res = await api.get<{ path: string; lines: string[] }>(`/api/admin/logs?kind=${kind.value}&lines=300`)
    path.value = res.path
    lines.value = res.lines
  } catch (e) {
    toast.error((e as Error).message)
    lines.value = []
  } finally {
    loading.value = false
  }
}

function switchKind(k: 'app' | 'cups') {
  kind.value = k
  load()
}

onMounted(load)
</script>

<template>
  <div class="admin-header">
    <div>
      <h1 class="admin-title">日志</h1>
      <p class="admin-sub">应用日志与 CUPS error_log</p>
    </div>
    <button class="btn btn-ghost btn-sm" :disabled="loading" @click="load">刷新</button>
  </div>

  <section class="card">
    <div class="section-head">
      <div style="display: flex; gap: 8px">
        <button class="btn btn-sm" :class="kind === 'app' ? 'btn-primary' : 'btn-ghost'" @click="switchKind('app')">应用日志</button>
        <button class="btn btn-sm" :class="kind === 'cups' ? 'btn-primary' : 'btn-ghost'" @click="switchKind('cups')">CUPS 日志</button>
      </div>
      <span class="admin-sub">{{ path }}</span>
    </div>
    <pre style="margin: 0; padding: 16px; background: var(--surface-2); border: 1px solid var(--line); border-radius: 12px; max-height: 520px; overflow: auto; font-size: 12px; line-height: 1.6">{{ lines.join('\n') || '暂无日志' }}</pre>
  </section>
</template>
