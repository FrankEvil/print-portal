<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'
import { toast } from '../toast'
import type { SharingInfo, SharingPrinter } from '../types'
import StatusBadge from '../components/StatusBadge.vue'

const info = ref<SharingInfo | null>(null)
const settings = ref({ public_host: '', cups_port: '8631', web_port: '8088' })
const savingSettings = ref(false)
const loading = ref(false)

const webUrl = computed(() => {
  if (!info.value) return ''
  return `http://${info.value.public_host}:${info.value.web_port}/`
})

async function load() {
  loading.value = true
  try {
    const res = await api.get<SharingInfo>('/api/admin/sharing')
    info.value = res
    settings.value = { public_host: res.public_host, cups_port: res.cups_port, web_port: res.web_port }
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  savingSettings.value = true
  try {
    await api.put('/api/admin/settings', settings.value)
    toast.success('连接地址已保存')
    await load()
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    savingSettings.value = false
  }
}

function prettyModel(value: string) {
  if (!value) return '—'
  if (value === 'everywhere') return 'IPP Everywhere（免驱动）'
  const base = (value.split('/').pop() || value).replace(/\.(gz|ppd)$/i, '')
  return base.replace(/\s+CUPS$/i, '') || '—'
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.success('已复制')
  } catch {
    const textarea = document.createElement('textarea')
    textarea.value = text
    document.body.appendChild(textarea)
    textarea.select()
    document.execCommand('copy')
    textarea.remove()
    toast.success('已复制')
  }
}

async function toggleShare(p: SharingPrinter, event: Event) {
  const checked = (event.target as HTMLInputElement).checked
  try {
    await api.put(`/api/admin/printers/${encodeURIComponent(p.name)}`, {
      name: p.name,
      model: p.model,
      ppd_queue: p.ppd_queue,
      raw_queue: p.raw_queue,
      device_uri: p.device_uri,
      is_default: p.is_default,
      is_shared: checked,
    })
    p.is_shared = checked
    toast.success(checked ? '已开启 CUPS 共享标记' : '已关闭 CUPS 共享标记')
  } catch (e) {
    toast.error((e as Error).message)
    event.target && ((event.target as HTMLInputElement).checked = !checked)
  }
}

onMounted(load)
</script>

<template>
  <div class="admin-header">
    <div>
      <h1 class="admin-title">共享</h1>
      <p class="admin-sub">局域网客户端连接信息与访问地址配置</p>
    </div>
    <button class="btn btn-ghost btn-sm" :disabled="loading" @click="load">刷新</button>
  </div>

  <section class="card">
    <div class="section-head">
      <div>
        <h2 class="card-title">访问地址</h2>
        <p class="card-desc">局域网客户端使用这里的地址连接；保存后所有连接信息自动更新</p>
      </div>
    </div>
    <div class="form-grid">
      <div class="field"><label>主机 / IP 地址</label><input v-model="settings.public_host" class="input" placeholder="例如 192.168.2.30" /></div>
      <div class="field"><label>CUPS 端口</label><input v-model="settings.cups_port" class="input" placeholder="8631" /></div>
      <div class="field"><label>Web 端口</label><input v-model="settings.web_port" class="input" placeholder="8088" /></div>
    </div>
    <button class="btn btn-primary" style="margin-top: 16px" :disabled="savingSettings" @click="saveSettings">
      {{ savingSettings ? '保存中…' : '保存连接地址' }}
    </button>
    <div class="share-web share-web-global">
      网页打印地址：<a :href="webUrl" target="_blank" rel="noreferrer">{{ webUrl }}</a>
      <button class="btn btn-ghost btn-sm" @click="copy(webUrl)">复制</button>
    </div>
  </section>

  <div v-if="!info?.printers.length" class="card" style="margin-top: 20px">
    <div class="empty">还没有打印机，请先在「打印机」中添加</div>
  </div>

  <details v-for="p in info?.printers || []" :key="p.id" class="card share-printer-card">
    <summary class="share-printer-summary">
      <div class="share-printer-main">
        <div class="share-printer-title">
          <span class="share-printer-name">{{ p.name }}</span>
          <StatusBadge :status="p.state" />
          <span v-if="p.is_default" class="status-badge is-neutral">默认</span>
          <span v-if="p.is_shared" class="status-badge is-neutral">已共享</span>
        </div>
        <span class="share-printer-model">{{ prettyModel(p.model) }}</span>
      </div>
      <span class="share-expand">展开</span>
    </summary>

    <div class="share-printer-body">
      <div class="share-body-head">
        <span class="card-desc">连接地址</span>
        <label class="check-line" style="margin: 0" title="仅同步 CUPS printer-is-shared 标记，不改变 CUPS 监听或防火墙策略">
          <input type="checkbox" :checked="p.is_shared" @change="toggleShare(p, $event)" />
          CUPS 共享标记
        </label>
      </div>
      <p class="share-hint">共享标记只控制 CUPS 的打印机广播；局域网客户端使用上方 IPP 地址连接，实际访问由 CUPS 监听地址和防火墙决定。</p>

      <div class="share-grid">
        <div class="share-box is-ppd">
          <div class="share-box-head">
            <span class="queue-tag is-ppd">PPD</span>
            <strong>网页打印 / 本地 PPD</strong>
          </div>
          <code>{{ p.ppd_uri }}</code>
          <div class="share-box-actions">
            <button class="btn btn-ghost btn-sm" @click="copy(p.ppd_uri)">复制地址</button>
            <span>支持页码、双面、份数</span>
          </div>
        </div>

        <div class="share-box is-raw">
          <div class="share-box-head">
            <span class="queue-tag is-raw">RAW</span>
            <strong>本地 RAW 客户端</strong>
          </div>
          <code>{{ p.raw_uri }}</code>
          <div class="share-box-actions">
            <button class="btn btn-ghost btn-sm" @click="copy(p.raw_uri)">复制地址</button>
            <span>客户端自行渲染</span>
          </div>
        </div>
      </div>
    </div>
  </details>

  <section v-if="info?.printers.length" class="card" style="margin-top: 20px">
    <h2 class="card-title">客户端连接方法</h2>
    <div class="share-help">
      <div>
        <h3>macOS</h3>
        <p>系统设置 → 打印机与扫描仪 → 添加打印机 → IP，协议选择 IPP，地址填 PPD 或 RAW 连接地址。</p>
      </div>
      <div>
        <h3>Windows</h3>
        <p>设置 → 蓝牙和设备 → 打印机和扫描仪 → 添加设备 → 手动添加 → 使用 IP 地址，协议选 IPP。</p>
      </div>
      <div>
        <h3>Linux / CUPS</h3>
        <p><code>lpadmin -p &lt;队列名&gt; -E -v ipp://主机:端口/printers/&lt;队列名&gt; -m everywhere</code></p>
      </div>
    </div>
  </section>
</template>
