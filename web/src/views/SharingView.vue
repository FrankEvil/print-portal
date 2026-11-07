<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'
import { toast } from '../toast'
import type { SharingInfo, SharingPrinter } from '../types'
import StatusBadge from '../components/StatusBadge.vue'

const info = ref<SharingInfo | null>(null)
const authMode = ref<'token' | 'fnos'>('token')
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
    authMode.value = res.auth_mode || 'token'
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

async function saveSharing(p: SharingPrinter, access: boolean, discovery: boolean, airprint = p.airprint_enabled) {
  await api.put(`/api/admin/printers/${encodeURIComponent(p.name)}`, {
    name: p.name,
    model: p.model,
    ppd_queue: p.ppd_queue,
    raw_queue: p.raw_queue,
    device_uri: p.device_uri,
    is_default: p.is_default,
    is_shared: access,
    allow_discovery: discovery && access,
    airprint_enabled: airprint && access,
  })
  p.is_shared = access
  p.allow_discovery = discovery && access
}

async function toggleAccess(p: SharingPrinter, event: Event) {
  const checked = (event.target as HTMLInputElement).checked
  try {
    await saveSharing(p, checked, p.allow_discovery)
    toast.success(checked ? '已允许局域网访问' : '已禁止局域网访问')
  } catch (e) {
    toast.error((e as Error).message)
    ;(event.target as HTMLInputElement).checked = !checked
  }
}

async function toggleDiscovery(p: SharingPrinter, event: Event) {
  if (!p.is_shared) return
  const checked = (event.target as HTMLInputElement).checked
  try {
    await saveSharing(p, true, checked)
    toast.success(checked ? '已允许局域网发现' : '已关闭局域网发现')
  } catch (e) {
    toast.error((e as Error).message)
    ;(event.target as HTMLInputElement).checked = !checked
  }
}

async function toggleAirprint(p: SharingPrinter, event: Event) {
  if (!p.is_shared) return
  const checked = (event.target as HTMLInputElement).checked
  try {
    await saveSharing(p, true, p.allow_discovery, checked)
    p.airprint_enabled = checked
    toast.success(checked ? 'AirPrint 已开启' : 'AirPrint 已关闭')
  } catch (e) {
    toast.error((e as Error).message)
    ;(event.target as HTMLInputElement).checked = !checked
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
    <button class="btn btn-ghost btn-sm btn-refresh" :disabled="loading" @click="load">↻ <span class="refresh-label">刷新</span></button>
  </div>

  <section v-if="authMode !== 'fnos'" class="card">
    <div class="section-head">
      <div>
        <h2 class="card-title">访问地址</h2>
        <p class="card-desc">局域网客户端使用这里的地址连接；fnOS 环境会自动使用 NAS 的局域网 IP，也可手动覆盖</p>
      </div>
    </div>
    <div class="form-grid">
      <div class="field"><label>主机 / IP 地址（自动检测）</label><input v-model="settings.public_host" class="input" placeholder="例如 192.168.2.30" /></div>
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
        <div class="share-printer-identity">
          <span class="share-printer-icon">P</span>
          <div class="share-printer-text">
            <span class="share-printer-name">{{ p.name }}</span>
            <span class="share-printer-model">{{ prettyModel(p.model) }}</span>
          </div>
        </div>
        <div class="share-printer-tags">
          <StatusBadge :status="p.state" />
          <span v-if="p.is_default" class="status-badge is-neutral">默认</span>
          <span v-if="p.is_shared" class="status-badge is-neutral">允许访问</span>
          <span v-if="p.allow_discovery" class="status-badge is-neutral">允许发现</span>
          <span v-if="p.airprint_enabled" class="status-badge is-neutral">AirPrint</span>
        </div>
      </div>
      <span class="share-expand">展开</span>
    </summary>

    <div class="share-printer-body">
      <div class="share-body-head">
        <span class="card-desc">连接设置</span>
        <div class="share-switches">
          <label class="check-line" style="margin: 0">
            <input type="checkbox" :checked="p.is_shared" @change="toggleAccess(p, $event)" />
            允许访问
          </label>
          <label class="check-line" :style="{ margin: 0, opacity: p.is_shared ? 1 : 0.45 }">
            <input type="checkbox" :checked="p.allow_discovery" :disabled="!p.is_shared" @change="toggleDiscovery(p, $event)" />
            允许发现
          </label>
          <label class="check-line" :style="{ margin: 0, opacity: p.is_shared ? 1 : 0.45 }">
            <input type="checkbox" :checked="p.airprint_enabled" :disabled="!p.is_shared" @change="toggleAirprint(p, $event)" />
            AirPrint
          </label>
        </div>
      </div>
      <p class="share-hint">允许访问：局域网客户端可通过 IPP 网关连接；允许发现：通过 mDNS/Bonjour 广播。关闭访问时发现自动关闭。</p>

      <div class="share-grid">
        <div class="share-box is-ppd">
          <div class="share-box-head">
            <span class="queue-tag is-ppd">PPD</span>
            <strong>网页打印 / 本地 PPD</strong>
          </div>
          <div class="share-url-field">
            <code>{{ p.ppd_uri }}</code>
            <button class="share-url-copy" type="button" title="复制地址" aria-label="复制地址" @click="copy(p.ppd_uri)"><svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2"></rect><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"></path></svg></button>
          </div>
          <div class="share-box-actions">
            <span>服务器 PPD 转换 · 支持页码/双面</span>
          </div>
        </div>

        <div class="share-box is-raw">
          <div class="share-box-head">
            <span class="queue-tag is-raw">RAW</span>
            <strong>本地 RAW 客户端</strong>
          </div>
          <div class="share-url-field">
            <code>{{ p.raw_uri }}</code>
            <button class="share-url-copy" type="button" title="复制地址" aria-label="复制地址" @click="copy(p.raw_uri)"><svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2"></rect><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"></path></svg></button>
          </div>
          <div class="share-box-actions">
            <span>需本地驱动 · 原样透传</span>
          </div>
        </div>
      </div>
    </div>
  </details>

  <section v-if="info?.printers.length" class="card share-guide" style="margin-top: 20px">
    <h2 class="card-title">PPD 与 RAW 怎么选</h2>
    <div class="share-guide-grid">
      <div class="share-guide-card is-ppd">
        <div class="share-guide-head">
          <span class="queue-tag is-ppd">PPD</span>
          <strong>推荐大多数客户端</strong>
        </div>
        <p>服务器上的 CUPS 使用 PPD 驱动完成格式转换，客户端一般不需要安装厂商驱动。支持服务器端设置页码、双面、份数、纸张和颜色，网页打印固定使用这个队列。</p>
        <h3>连接过程</h3>
        <ol>
          <li>在 Mac / Windows 添加 IPP 打印机，地址填上面的 PPD 连接地址。</li>
          <li>驱动选择 IPP Everywhere、系统自动匹配或服务器上已安装的 PPD 型号。</li>
          <li>客户端直接打印；页码、双面等参数由 CUPS 服务器处理。</li>
        </ol>
      </div>

      <div class="share-guide-card is-raw">
        <div class="share-guide-head">
          <span class="queue-tag is-raw">RAW</span>
          <strong>适合已有本地驱动的客户端</strong>
        </div>
        <p>RAW 队列没有 PPD，CUPS 原样透传客户端发来的数据。<strong>本地必须先安装对应打印机厂商驱动</strong>，由本地驱动完成渲染和页面设置；服务器端不支持页码、双面、份数等参数转换。</p>
        <h3>连接过程</h3>
        <ol>
          <li>先在 Mac / Windows 安装对应型号的厂商驱动。</li>
          <li>添加 IPP 打印机，地址填上面的 RAW 连接地址，并选择刚安装的本地驱动。</li>
          <li>在客户端驱动的打印对话框中设置双面、纸张、份数等，再提交打印。</li>
          <li>客户端渲染后的数据通过 RAW 队列原样发送给打印机。</li>
        </ol>
      </div>
    </div>
    <p class="share-guide-note">简单记：<strong>PPD 由服务器转换，客户端可以不装驱动；RAW 由本地驱动渲染，本地必须装驱动。</strong> 网页打印始终走 PPD，不使用本地 RAW。</p>
  </section>

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
