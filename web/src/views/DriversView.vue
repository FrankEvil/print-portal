<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { toast } from '../toast'
import type { Driver, Model } from '../types'
import StatusBadge from '../components/StatusBadge.vue'

const drivers = ref<Driver[]>([])
const models = ref<Model[]>([])
const query = ref('')
const loading = ref(false)
const installing = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

async function load() {
  loading.value = true
  try {
    const res = await api.get<{ drivers: Driver[] }>('/api/admin/drivers')
    drivers.value = res.drivers
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function searchModels() {
  try {
    const res = await api.get<{ models: Model[] }>(`/api/admin/drivers/models?q=${encodeURIComponent(query.value)}`)
    models.value = res.models
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function install(e: Event) {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  if (!f) return
  installing.value = true
  try {
    const fd = new FormData()
    fd.append('file', f)
    await api.upload<Driver>('/api/admin/drivers/install', fd)
    toast.success(`驱动 ${f.name} 已安装`)
    await load()
  } catch (err) {
    toast.error((err as Error).message)
  } finally {
    installing.value = false
    input.value = ''
  }
}

async function uninstall(d: Driver) {
  if (!confirm(`确定卸载驱动 ${d.name}？`)) return
  try {
    await api.post(`/api/admin/drivers/${d.id}/uninstall`)
    toast.success('已卸载')
    await load()
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function refresh() {
  try {
    const res = await api.post<{ drivers: Driver[] }>('/api/admin/drivers/refresh')
    drivers.value = res.drivers
    toast.success('驱动状态已刷新')
  } catch (e) {
    toast.error((e as Error).message)
  }
}

onMounted(async () => {
  await load()
  await searchModels()
})
</script>

<template>
  <div class="admin-header">
    <div>
      <h1 class="admin-title">驱动管理</h1>
      <p class="admin-sub">内置驱动、驱动包与型号索引；状态区分可用 / 降级 / 缺失</p>
    </div>
    <div style="display: flex; gap: 8px">
      <button class="btn btn-ghost" @click="refresh">刷新状态</button>
      <button class="btn btn-primary" :disabled="installing" @click="fileInput?.click()">
        {{ installing ? '安装中…' : '上传驱动 / PPD' }}
      </button>
      <input ref="fileInput" type="file" accept=".ppd,.deb,.tar.gz,.tgz" style="display: none" @change="install" />
    </div>
  </div>

  <section class="card">
    <div class="section-head">
      <h2 class="card-title">已安装驱动</h2>
      <span class="admin-sub">{{ drivers.length }} 个</span>
    </div>
    <p class="card-desc" style="margin-bottom: 12px">
      状态由当前 CUPS 环境的 <code>lpinfo -m</code> 型号索引计算。「缺失」表示本机未安装对应驱动包（Docker 镜像内置 brlaser / foomatic；macOS 本机 CUPS 不包含）。
    </p>
    <div v-if="!drivers.length" class="empty">暂无驱动信息</div>
    <div v-else class="table-wrap">
      <table class="table">
        <thead>
          <tr><th>驱动</th><th>版本</th><th>类型</th><th>状态</th><th>型号数</th><th>许可证</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="d in drivers" :key="d.id">
            <td><strong>{{ d.name }}</strong></td>
            <td>{{ d.version || '—' }}</td>
            <td>{{ d.kind === 'builtin' ? '内置' : d.kind === 'package' ? '驱动包' : 'PPD' }}</td>
            <td><StatusBadge :status="d.state" /></td>
            <td>{{ d.models_count }}</td>
            <td>{{ d.license || '—' }}</td>
            <td>
              <button v-if="d.kind !== 'builtin'" class="btn btn-danger btn-sm" @click="uninstall(d)">卸载</button>
              <span v-else class="admin-sub">只读</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>

  <section class="card" style="margin-top: 20px">
    <div class="section-head">
      <h2 class="card-title">型号索引</h2>
      <div style="display: flex; gap: 8px">
        <input v-model="query" class="input" style="width: 240px" placeholder="搜索型号，如 DCP-7060" @keyup.enter="searchModels" />
        <button class="btn btn-ghost btn-sm" @click="searchModels">搜索</button>
      </div>
    </div>
    <p class="card-desc" style="margin-bottom: 12px">
      型号索引来自 <code>lpinfo -m</code>，数量多不代表驱动已安装；「已安装可用」请以上方驱动状态为准。
    </p>
    <div v-if="!models.length" class="empty">没有匹配型号</div>
    <div v-else class="table-wrap" style="max-height: 380px">
      <table class="table">
        <thead><tr><th>PPD 名称</th><th>型号描述</th><th>所属驱动</th></tr></thead>
        <tbody>
          <tr v-for="m in models" :key="m.name">
            <td style="word-break: break-all">{{ m.name }}</td>
            <td>{{ m.description }}</td>
            <td>{{ m.driver }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
