<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import type { Job, Overview } from '../types'
import StatusBadge from '../components/StatusBadge.vue'
import { formatJobParams } from '../jobDisplay'
import { toast } from '../toast'

const overview = ref<Overview | null>(null)
const jobs = ref<Job[]>([])

async function load() {
  try {
    overview.value = await api.get<Overview>('/api/admin/overview')
    const res = await api.get<{ jobs: Job[] }>('/api/admin/jobs?limit=8')
    jobs.value = res.jobs
  } catch (e) {
    toast.error((e as Error).message)
  }
}

onMounted(load)
</script>

<template>
  <div class="admin-header">
    <div>
      <h1 class="admin-title">概览</h1>
      <p class="admin-sub">CUPS 与打印任务状态一览</p>
    </div>
    <button class="btn btn-ghost btn-sm btn-refresh" @click="load">↻ <span class="refresh-label">刷新</span></button>
  </div>

  <div class="stat-grid" v-if="overview">
    <div class="stat">
      <div class="stat-label">CUPS</div>
      <div class="stat-value small">{{ overview.cups.state === 'running' ? '运行中' : '未运行' }}</div>
      <div class="admin-sub">版本 {{ overview.cups.version || '—' }}</div>
    </div>
    <div class="stat">
      <div class="stat-label">打印机</div>
      <div class="stat-value">{{ overview.printers }}</div>
      <div class="admin-sub">{{ overview.default_printer || '未设默认' }}</div>
    </div>
    <div class="stat">
      <div class="stat-label">活动任务</div>
      <div class="stat-value">{{ overview.active_jobs }}</div>
      <div class="admin-sub">排队 / 转换 / 打印中</div>
    </div>
    <div class="stat">
      <div class="stat-label">今日完成</div>
      <div class="stat-value">{{ overview.completed_jobs_today }}</div>
      <div class="admin-sub">已完成任务</div>
    </div>
  </div>

  <div class="quick-grid">
    <router-link to="/admin/printers" class="quick-card">
      <span class="quick-icon">⌁</span>
      <span><strong>打印机</strong><small>添加或管理设备</small></span>
      <span class="quick-arrow">→</span>
    </router-link>
    <router-link to="/admin/jobs" class="quick-card">
      <span class="quick-icon">▣</span>
      <span><strong>任务管理</strong><small>查看打印队列和记录</small></span>
      <span class="quick-arrow">→</span>
    </router-link>
    <router-link to="/admin/sharing" class="quick-card">
      <span class="quick-icon">⇄</span>
      <span><strong>共享设置</strong><small>局域网连接与权限</small></span>
      <span class="quick-arrow">→</span>
    </router-link>
  </div>

  <section class="card" style="margin-top: 20px">
    <div class="section-head">
      <h2 class="card-title">最近任务</h2>
      <router-link to="/admin/jobs" class="btn btn-ghost btn-sm">全部任务</router-link>
    </div>
    <div v-if="!jobs.length" class="empty">暂无任务</div>
    <div v-else class="table-wrap">
      <table class="table">
        <thead>
          <tr><th>ID</th><th>文件</th><th>打印机</th><th>来源</th><th>参数</th><th>状态</th><th>时间</th></tr>
        </thead>
        <tbody>
          <tr v-for="j in jobs" :key="j.id">
            <td data-label="ID">#{{ j.id }}</td>
            <td class="job-file-cell" data-label="文件">
              <div class="job-file-name" style="max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ j.filename }}</div>
              <div class="admin-sub">{{ j.source === 'web' ? '网页' : j.source === 'test' ? '测试页' : '本地' }}</div>
            </td>
            <td data-label="打印机">{{ j.printer_name }}</td>
            <td data-label="来源">{{ j.source === 'web' ? '网页' : j.source === 'test' ? '测试页' : '本地' }}</td>
            <td class="admin-sub" data-label="参数">{{ formatJobParams(j) }}</td>
            <td data-label="状态"><StatusBadge :status="j.status" /></td>
            <td class="admin-sub" data-label="时间">{{ new Date(j.created_at).toLocaleString('zh-CN', { hour12: false }) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
