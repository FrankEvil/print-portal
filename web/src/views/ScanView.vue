<script setup lang="ts">
import { ref } from 'vue'
import { api } from '../api'
import { toast } from '../toast'
import type { Device } from '../types'

const scanners = ref<Device[]>([])
const loading = ref(false)

async function discover() {
  loading.value = true
  try {
    const res = await api.get<{ devices: Device[] }>('/api/admin/discovery/scanners')
    scanners.value = res.devices
    toast.success(`发现 ${res.devices.length} 台扫描仪`)
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <main class="page">
    <header class="topbar">
      <div class="brand">
        <div class="brand-mark">P</div>
        <div>
          <div class="brand-name">Print Portal</div>
          <div class="brand-sub">扫描</div>
        </div>
      </div>
      <nav class="nav">
        <a href="/">打印</a>
        <a href="/scan" class="router-link-active">扫描</a>
        <a href="/admin">管理</a>
      </nav>
    </header>

    <section class="card">
      <div class="section-head">
        <div>
          <h2 class="card-title">扫描仪</h2>
          <p class="card-desc">扫描功能是 P1 收尾项；USB 扫描依赖 Brother brscan4 驱动，未通过则顺延 P1.1</p>
        </div>
        <button class="btn btn-primary" :disabled="loading" @click="discover">{{ loading ? '扫描中…' : '发现扫描仪' }}</button>
      </div>
      <div v-if="!scanners.length" class="empty">点击「发现扫描仪」开始检测</div>
      <div v-else class="job-list">
        <div v-for="s in scanners" :key="s.id" class="job-item">
          <div class="job-main">
            <div class="job-title">{{ s.name }}</div>
            <div class="job-sub">{{ s.device_uri }}</div>
          </div>
          <span class="status-badge is-neutral">已发现</span>
        </div>
      </div>
    </section>
  </main>
</template>
