<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { api, apiUrl } from '../api'
import { toast } from '../toast'
import type { Job } from '../types'
import { formatJobParams } from '../jobDisplay'
import { mergeById, createPoller, type TaskOptions } from '../utils/refresh'
import StatusBadge from '../components/StatusBadge.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'

const jobs = ref<Job[]>([])
const status = ref('')
const source = ref('')
const query = ref('')
let searchTimer: number | undefined
const ACTIVE_STATUSES = ['pending', 'converting', 'submitted', 'queued', 'printing', 'held', 'paused']
const showClearConfirm = ref(false)
const clearing = ref(false)

function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => {
    refreshJobs()
  }, 300)
}

async function load(opts: TaskOptions = { quiet: false, force: true }) {
  try {
    const params = new URLSearchParams()
    if (status.value) params.set('status', status.value)
    if (source.value) params.set('source', source.value)
    if (query.value.trim()) params.set('q', query.value.trim())
    params.set('limit', '200')
    const res = await api.get<{ jobs: Job[] }>(`/api/admin/jobs?${params.toString()}`)
    jobs.value = mergeById(jobs.value, res.jobs || [], { force: opts.force })
    poller.sync()
  } catch (e) {
    if (!opts.quiet) toast.error((e as Error).message)
    throw e
  }
}

const poller = createPoller(load, {
  interval: 5000,
  activeWhen: () => jobs.value.some((j) => ACTIVE_STATUSES.includes(j.status)),
  pauseWhen: () => showClearConfirm.value,
})

function refreshJobs() {
  poller.refresh()
}

function canFile(j: Job) {
  return j.source === 'web' && ['submitted','queued','printing','completed','failed'].includes(j.status)
}
function canCancel(j: Job) {
  return ['pending','converting','submitted','queued','printing','held','paused'].includes(j.status)
}
function canRetry(j: Job) {
  return ['failed','cancelled'].includes(j.status) && j.source === 'web'
}
function canHold(j: Job) {
  return !!j.cups_job_id && ['queued','printing'].includes(j.status)
}
function canRelease(j: Job) {
  return !!j.cups_job_id && j.status === 'paused'
}
function canRestart(j: Job) {
  return !!j.cups_job_id && ['failed','completed'].includes(j.status)
}


function openJobFile(id: number) {
  window.open(apiUrl(`/api/jobs/${id}/file`), '_blank')
}

async function clearRecords() {
  if (clearing.value) return
  clearing.value = true
  try {
    const res = await api.del<{ cleared: number; cups_cleared?: boolean }>('/api/admin/jobs')
    showClearConfirm.value = false
    toast.success(`已清理 ${res.cleared} 条打印记录，CUPS 历史已同步`)
    refreshJobs()
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    clearing.value = false
  }
}

async function act(j: Job, action: string) {
  if (action === 'cancel' && !confirm(`取消任务 #${j.id}？`)) return
  try {
    if (action === 'cancel') await api.post(`/api/admin/jobs/${j.id}/cancel`)
    else if (action === 'retry') await api.post(`/api/jobs/${j.id}/retry`)
    else await api.post(`/api/admin/jobs/${j.id}/${action}`)
    toast.success('操作成功')
    refreshJobs()
  } catch (e) {
    toast.error((e as Error).message)
  }
}

onMounted(() => {
  poller.refresh()
  poller.start()
})
onUnmounted(() => {
  poller.stop()
  if (searchTimer) clearTimeout(searchTimer)
})
</script>

<template>
  <div class="admin-header">
    <div>
      <h1 class="admin-title">任务管理</h1>
      <p class="admin-sub">网页任务与本地直连任务统一展示；本地任务仅含 CUPS 元数据</p>
    </div>
    <div class="admin-actions">
      <select v-model="status" class="select" style="width: 140px" @change="refreshJobs">
        <option value="">全部状态</option>
        <option value="pending">待处理</option>
        <option value="converting">转换中</option>
        <option value="submitted">已提交</option>
        <option value="queued">排队中</option>
        <option value="paused">已暂停</option>
        <option value="printing">打印中</option>
        <option value="completed">已完成</option>
        <option value="failed">失败</option>
        <option value="cancelled">已取消</option>
      </select>
      <select v-model="source" class="select" style="width: 120px" @change="refreshJobs">
        <option value="">全部来源</option>
        <option value="web">网页</option>
        <option value="local">本地</option>
        <option value="test">测试页</option>
      </select>
      <input
        v-model="query"
        class="input"
        style="width: 220px"
        placeholder="搜索文件 / 用户 / 状态"
        @input="onSearchInput"
        @keyup.enter="refreshJobs"
      />
      <button class="btn btn-ghost btn-sm" @click="showClearConfirm = true">清空打印记录</button>
      <button class="btn btn-ghost btn-sm" @click="refreshJobs">刷新</button>
    </div>
  </div>

  <section class="card">
    <div v-if="!jobs.length" class="empty">暂无任务</div>
    <div v-else class="table-wrap">
      <table class="table">
        <thead>
          <tr><th>ID</th><th>文件 / 标题</th><th>打印机</th><th>来源</th><th>参数</th><th>状态</th><th>时间</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="j in jobs" :key="j.id">
            <td data-label="ID">#{{ j.id }}</td>
            <td class="job-file-cell" data-label="文件">
              <div class="job-file-name" style="max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ j.filename }}</div>
              <div v-if="j.cups_user" class="admin-sub">{{ j.cups_user }}</div>
            </td>
            <td data-label="打印机">{{ j.printer_name }}</td>
            <td data-label="来源">{{ j.source === 'web' ? '网页' : j.source === 'test' ? '测试页' : '本地' }}</td>
            <td class="admin-sub" data-label="参数">{{ formatJobParams(j) }}</td>
            <td data-label="状态">
              <StatusBadge :status="j.status" />
              <div v-if="j.error_message" class="admin-sub" style="color: var(--danger); max-width: 220px">{{ j.error_message }}</div>
            </td>
            <td class="admin-sub" data-label="时间">{{ new Date(j.created_at).toLocaleString('zh-CN', { hour12: false }) }}</td>
            <td data-label="操作">
              <div style="display: flex; gap: 6px; flex-wrap: wrap">
                <button class="btn btn-ghost btn-sm" :class="{ 'is-hidden': !canFile(j) }" :disabled="!canFile(j)" @click="openJobFile(j.id)">查看文件</button>
                <button class="btn btn-ghost btn-sm" :class="{ 'is-hidden': !canCancel(j) }" :disabled="!canCancel(j)" @click="act(j, 'cancel')">取消</button>
                <button class="btn btn-ghost btn-sm" :class="{ 'is-hidden': !canRetry(j) }" :disabled="!canRetry(j)" @click="act(j, 'retry')">重试</button>
                <button class="btn btn-ghost btn-sm" :class="{ 'is-hidden': !canHold(j) }" :disabled="!canHold(j)" @click="act(j, 'hold')">暂停</button>
                <button class="btn btn-ghost btn-sm" :class="{ 'is-hidden': !canRelease(j) }" :disabled="!canRelease(j)" @click="act(j, 'release')">恢复</button>
                <button class="btn btn-ghost btn-sm" :class="{ 'is-hidden': !canRestart(j) }" :disabled="!canRestart(j)" @click="act(j, 'restart')">重新打印</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>

  <ConfirmDialog
    :open="showClearConfirm"
    title="清空打印记录"
    message="将删除所有已完成、失败和已取消的打印记录，并同步清空 CUPS 历史；正在打印的任务不受影响。"
    confirm-text="清空记录"
    danger
    :busy="clearing"
    @close="showClearConfirm = false"
    @confirm="clearRecords"
  />
</template>
