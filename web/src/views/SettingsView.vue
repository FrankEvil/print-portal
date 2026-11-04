<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { toast } from '../toast'
import { applyTheme, getTheme, type ThemeMode } from '../theme'

const settings = ref<Record<string, string>>({})
const saving = ref(false)
const theme = ref<ThemeMode>(getTheme())

const fields: { key: string; label: string; type?: string; hint?: string }[] = [
  { key: 'max_upload_mb', label: '上传大小上限（MB）', type: 'number' },
  { key: 'temp_retention_hours', label: '临时文件保留（小时）', type: 'number' },
  { key: 'job_history_days', label: '任务历史保留（天）', type: 'number' },
  { key: 'language', label: '语言', hint: '当前 P1 仅中文' },
  { key: 'default_printer_id', label: '默认打印机 ID', type: 'number' },
]

async function load() {
  try {
    settings.value = await api.get('/api/admin/settings')
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function save() {
  saving.value = true
  try {
    await api.put('/api/admin/settings', settings.value)
    toast.success('设置已保存')
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    saving.value = false
  }
}

function changeTheme() {
  applyTheme(theme.value)
}

onMounted(load)
</script>

<template>
  <div class="admin-header">
    <div>
      <h1 class="admin-title">应用设置</h1>
      <p class="admin-sub">上传、保留策略与界面偏好</p>
    </div>
  </div>

  <section class="card">
    <div class="form-grid">
      <div v-for="f in fields" :key="f.key" class="field">
        <label>{{ f.label }}</label>
        <input v-model="settings[f.key]" :type="f.type || 'text'" class="input" :disabled="f.key === 'language'" />
        <span v-if="f.hint" class="admin-sub">{{ f.hint }}</span>
      </div>
      <div class="field">
        <label>主题</label>
        <select v-model="theme" class="select" @change="changeTheme">
          <option value="auto">跟随系统</option>
          <option value="light">浅色</option>
          <option value="dark">深色</option>
        </select>
      </div>
    </div>
    <button class="btn btn-primary" style="margin-top: 18px" :disabled="saving" @click="save">
      {{ saving ? '保存中…' : '保存设置' }}
    </button>
  </section>

  <section class="card" style="margin-top: 20px">
    <h2 class="card-title">关于备份</h2>
    <p class="card-desc">备份 / 恢复功能规划在 P1.1，包含配置、数据库、PPD 与 cupsd.conf。</p>
  </section>
</template>
