<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, apiUrl } from '../api'
import { toast } from '../toast'
import type { ScanJob, Scanner } from '../types'
import StatusBadge from '../components/StatusBadge.vue'

const scanners = ref<Scanner[]>([])
const selected = ref('')
const loading = ref(false)
const scanning = ref(false)
const result = ref<ScanJob | null>(null)

const form = ref({
  format: 'pdf',
  resolution: 300,
  color_mode: 'color',
})

async function discover() {
  loading.value = true
  try {
    const res = await api.get<{ scanners: Scanner[] }>('/api/scanners')
    scanners.value = res.scanners || []
    if (!selected.value && scanners.value.length) selected.value = scanners.value[0].uri
    toast.success(`发现 ${scanners.value.length} 台扫描仪`)
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function scan() {
  if (!selected.value) return toast.error('请先发现并选择扫描仪')
  scanning.value = true
  result.value = null
  try {
    const job = await api.post<ScanJob>('/api/scan', {
      scanner: selected.value,
      format: form.value.format,
      resolution: form.value.resolution,
      color_mode: form.value.color_mode,
    })
    result.value = job
    toast.success('扫描完成')
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    scanning.value = false
  }
}

function downloadUrl(id: number) {
  return apiUrl(`/api/scans/${id}/download`)
}

function previewUrl(id: number) {
  return apiUrl(`/api/scans/${id}/download?inline=1`)
}

function openInNewWindow() {
  window.open(window.location.href, '_blank', 'noopener')
}

onMounted(discover)
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
        <router-link to="/">打印</router-link>
        <router-link to="/scan" class="router-link-active">扫描</router-link>
        <router-link to="/admin">管理</router-link>
        <button class="nav-link-btn" type="button" @click="openInNewWindow">新窗口</button>
      </nav>
    </header>

    <div class="print-layout">
      <section class="card">
        <div class="section-head">
          <div>
            <h2 class="card-title">扫描仪</h2>
            <p class="card-desc">USB 扫描依赖 Brother brscan4 驱动；网络扫描使用 sane-airscan</p>
          </div>
          <button class="btn btn-ghost btn-sm" :disabled="loading" @click="discover">
            {{ loading ? '检测中…' : '重新检测' }}
          </button>
        </div>

        <div v-if="!scanners.length" class="empty">未发现扫描仪。请确认 SANE 驱动已安装，或网络扫描仪已开启。</div>
        <div v-else class="job-list">
          <label v-for="s in scanners" :key="s.uri" class="job-item" style="cursor: pointer">
            <input v-model="selected" type="radio" :value="s.uri" />
            <div class="job-main">
              <div class="job-title">{{ s.name }}</div>
              <div class="job-sub">{{ s.uri }}</div>
            </div>
          </label>
        </div>
      </section>

      <section class="card">
        <div class="section-head">
          <div>
            <h2 class="card-title">扫描设置</h2>
            <p class="card-desc">扫描完成后可直接预览和下载</p>
          </div>
        </div>
        <div class="params">
          <div class="field full">
            <label>格式</label>
            <select v-model="form.format" class="select">
              <option value="pdf">PDF</option>
              <option value="jpeg">JPEG</option>
              <option value="png">PNG</option>
            </select>
          </div>
          <div class="field">
            <label>分辨率</label>
            <select v-model.number="form.resolution" class="select">
              <option :value="150">150 dpi</option>
              <option :value="300">300 dpi</option>
              <option :value="600">600 dpi</option>
            </select>
          </div>
          <div class="field">
            <label>颜色</label>
            <select v-model="form.color_mode" class="select">
              <option value="color">彩色</option>
              <option value="gray">灰度</option>
              <option value="lineart">黑白</option>
            </select>
          </div>
        </div>
        <button class="btn btn-primary" style="width: 100%; margin-top: 18px" :disabled="scanning || !scanners.length" @click="scan">
          {{ scanning ? '扫描中…' : '开始扫描' }}
        </button>
      </section>
    </div>

    <section v-if="result" class="card" style="margin-top: 20px">
      <div class="section-head">
        <div>
          <h2 class="card-title">扫描结果</h2>
          <p class="card-desc">{{ result.filename }} · {{ result.resolution }} dpi · {{ result.color_mode }}</p>
        </div>
        <StatusBadge :status="result.status" />
      </div>
      <div v-if="result.status === 'completed'" style="margin: 16px 0">
        <iframe v-if="result.format === 'pdf'" :src="previewUrl(result.id)" style="width: 100%; height: 520px; border: 1px solid var(--line); border-radius: 12px"></iframe>
        <img v-else :src="previewUrl(result.id)" alt="扫描预览" style="max-width: 100%; border: 1px solid var(--line); border-radius: 12px" />
      </div>
      <a :href="downloadUrl(result.id)" class="btn btn-primary" download>下载扫描文件</a>
    </section>
  </main>
</template>
