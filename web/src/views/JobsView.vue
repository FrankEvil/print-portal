<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api'
import { toast } from '../toast'
import type { Job } from '../types'
import { formatJobParams } from '../jobDisplay'
import StatusBadge from '../components/StatusBadge.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'

const jobs = ref<Job[]>([])
const status = ref('')
const source = ref('')
let timer: number | undefined
const showClearConfirm = ref(false)
const clearing = ref(false)

async function load() {
  try {
    const params = new URLSearchParams()
    if (status.value) params.set('status', status.value)
    if (source.value) params.set('source', source.value)
    params.set('limit', '200')
    const res = await api.get<{ jobs: Job[] }>(`/api/admin/jobs?${params.toString()}`)
    jobs.value = res.jobs
  } catch (e) {
    toast.error((e as Error).message)
  }
}

function openJobFile(id: number) {
  window.open(`/api/jobs/${id}/file`, '_blank')
}

async function clearRecords() {
  if (clearing.value) return
  clearing.value = true
  try {
    const res = await api.del<{ cleared: number; cups_cleared?: boolean }>('/api/admin/jobs')
    showClearConfirm.value = false
    toast.success(`已清理 ${res.cleared} 条打印记录，CUPS 历史已同步`)
    await load()
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
    await load()
  } catch (e) {
    toast.error((e as Error).message)
  }
}

onMounted(() => {
  load()
  timer = window.setInterval(load, 3000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="admin-header">
    <div>
      <h1 class="admin-title">任务管理</h1>
      <p class="admin-sub">网页任务与本地直连任务统一展示；本地任务仅含 CUPS 元数据</p>
    </div>
    <div style="display: flex; gap: 8px">
      <select v-model="status" class="select" style="width: 140px" @change="load">
        <option value="">全部状态</option>
        <option value="pending">待处理</option>
        <option value="converting">转换中</option>
        <option value="submitted">已提交</option>
        <option value="queued">排队中</option>
        <option value="printing">打印中</option>
        <option value="completed">已完成</option>
        <option value="failed">失败</option>
        <option value="cancelled">已取消</option>
      </select>
      <select v-model="source" class="select" style="width: 120px" @change="load">
        <option value="">全部来源</option>
        <option value="web">网页</option>
        <option value="local">本地</option>
      </select>
      <button class="btn btn-ghost btn-sm" @click="showClearConfirm = true">清空打印记录</button>
      <button class="btn btn-ghost btn-sm" @click="load">刷新</button>
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
            <td>#{{ j.id }}</td>
            <td>
              <div style="max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ j.filename }}</div>
              <div v-if="j.cups_user" class="admin-sub">{{ j.cups_user }}</div>
            </td>
            <td>{{ j.printer_name }}</td>
            <td>{{ j.source === 'web' ? '网页' : '本地' }}</td>
            <td class="admin-sub">{{ formatJobParams(j) }}</td>
            <td>
              <StatusBadge :status="j.status" />
              <div v-if="j.error_message" class="admin-sub" style="color: var(--danger); max-width: 220px">{{ j.error_message }}</div>
            </td>
            <td class="admin-sub">{{ new Date(j.created_at).toLocaleString('zh-CN', { hour12: false }) }}</td>
            <td>
              <div style="display: flex; gap: 6px; flex-wrap: wrap">
                <button v-if="j.source === 'web' && ['submitted','queued','printing','completed','failed'].includes(j.status)" class="btn btn-ghost btn-sm" @click="openJobFile(j.id)">查看文件</button>
                <button v-if="['pending','converting','submitted','queued','printing'].includes(j.status)" class="btn btn-ghost btn-sm" @click="act(j, 'cancel')">取消</button>
                <button v-if="['failed','cancelled'].includes(j.status) && j.source === 'web'" class="btn btn-ghost btn-sm" @click="act(j, 'retry')">重试</button>
                <button v-if="j.cups_job_id && ['queued','printing'].includes(j.status)" class="btn btn-ghost btn-sm" @click="act(j, 'hold')">暂停</button>
                <button v-if="j.cups_job_id && ['queued','printing'].includes(j.status)" class="btn btn-ghost btn-sm" @click="act(j, 'release')">恢复</button>
                <button v-if="j.cups_job_id && ['failed','completed'].includes(j.status)" class="btn btn-ghost btn-sm" @click="act(j, 'restart')">重新打印</button>
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
