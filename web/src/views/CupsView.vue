<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { toast } from '../toast'
import type { Printer } from '../types'

const status = ref<{ version: string; state: string; server_name: string; printers: Printer[] } | null>(null)
const settings = ref<Record<string, string>>({})
const configPath = ref('')
const configContent = ref('')
const savingSettings = ref(false)
const savingConfig = ref(false)

const settingLabels: Record<string, string> = {
  web_interface: 'Web 界面',
  share_printers: '共享打印机',
  browsing: '浏览（Browsing）',
  log_level: '日志级别',
  max_jobs: '最大任务数',
  preserve_job_history: '保留任务历史',
  preserve_job_files: '保留任务文件',
}

async function loadStatus() {
  try {
    status.value = await api.get('/api/admin/cups/status')
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function loadSettings() {
  try {
    settings.value = await api.get('/api/admin/cups/settings')
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function loadConfig() {
  try {
    const res = await api.get<{ path: string; content: string }>('/api/admin/cups/config')
    configPath.value = res.path
    configContent.value = res.content
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function saveSettings() {
  savingSettings.value = true
  try {
    await api.put('/api/admin/cups/settings', settings.value)
    toast.success('CUPS 设置已保存')
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    savingSettings.value = false
  }
}

async function saveConfig() {
  savingConfig.value = true
  try {
    await api.put('/api/admin/cups/config', { content: configContent.value })
    toast.success('cupsd.conf 已校验、备份并保存')
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    savingConfig.value = false
  }
}

onMounted(async () => {
  await loadStatus()
  await loadSettings()
  await loadConfig()
})
</script>

<template>
  <div class="admin-header">
    <div>
      <h1 class="admin-title">CUPS 控制台</h1>
      <p class="admin-sub">版本、状态、常用参数与 cupsd.conf 高级编辑</p>
    </div>
    <button class="btn btn-ghost btn-sm btn-refresh" @click="loadStatus">↻ <span class="refresh-label">刷新</span></button>
  </div>

  <div class="stat-grid" v-if="status">
    <div class="stat">
      <div class="stat-label">CUPS 版本</div>
      <div class="stat-value small">{{ status.version || '—' }}</div>
    </div>
    <div class="stat">
      <div class="stat-label">运行状态</div>
      <div class="stat-value small">{{ status.state === 'running' ? '运行中' : '未运行' }}</div>
    </div>
    <div class="stat">
      <div class="stat-label">ServerName</div>
      <div class="stat-value small">{{ status.server_name }}</div>
    </div>
    <div class="stat">
      <div class="stat-label">打印队列</div>
      <div class="stat-value">{{ status.printers?.length || 0 }}</div>
    </div>
  </div>

  <section class="card cups-settings-card" style="margin-top: 20px">
    <div class="section-head"><h2 class="card-title">常用参数</h2></div>
    <div class="form-grid cups-settings-grid">
      <div v-for="(label, key) in settingLabels" :key="key" class="field">
        <label>{{ label }}</label>
        <select v-if="['true', 'false'].includes(settings[key])" v-model="settings[key]" class="select">
          <option value="true">开启</option>
          <option value="false">关闭</option>
        </select>
        <input v-else v-model="settings[key]" class="input" />
      </div>
    </div>
    <button class="btn btn-primary cups-save-btn" style="margin-top: 16px" :disabled="savingSettings" @click="saveSettings">保存参数</button>
  </section>

  <section class="card cups-config-card" style="margin-top: 20px">
    <div class="section-head">
      <div>
        <h2 class="card-title">cupsd.conf 高级编辑</h2>
        <p class="card-desc">保存前会执行 <code>cupsd -t</code> 校验，旧文件自动备份，失败不覆盖</p>
      </div>
      <button class="btn btn-ghost btn-sm" @click="loadConfig">重新加载</button>
    </div>
    <p class="admin-sub" style="margin-bottom: 8px">{{ configPath }}</p>
    <textarea v-model="configContent" class="textarea" spellcheck="false" style="min-height: 320px"></textarea>
    <button class="btn btn-primary" style="margin-top: 12px" :disabled="savingConfig" @click="saveConfig">
      {{ savingConfig ? '校验并保存…' : '校验并保存' }}
    </button>
  </section>
</template>
