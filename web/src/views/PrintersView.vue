<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { toast } from '../toast'
import type { AdminPrinter, Model, PPDOption } from '../types'
import StatusBadge from '../components/StatusBadge.vue'

const printers = ref<AdminPrinter[]>([])
const showForm = ref(false)
const editing = ref<AdminPrinter | null>(null)
const saving = ref(false)
const form = ref({
  name: '',
  model: '',
  ppd_queue: '',
  raw_queue: '',
  device_uri: '',
  is_default: false,
  is_shared: true,
})
const modelOptions = ref<Model[]>([])

const optionsFor = ref('')
const ppdOptions = ref<PPDOption[]>([])
const optionValues = ref<Record<string, string>>({})

async function load() {
  try {
    const res = await api.get<{ printers: AdminPrinter[] }>('/api/admin/printers')
    printers.value = res.printers
  } catch (e) {
    toast.error((e as Error).message)
  }
}

function openCreate() {
  editing.value = null
  form.value = { name: '', model: '', ppd_queue: '', raw_queue: '', device_uri: '', is_default: false, is_shared: true }
  showForm.value = true
  loadModelOptions()
}

async function loadModelOptions() {
  try {
    const res = await api.get<{ models: Model[] }>('/api/admin/drivers/models?q=')
    modelOptions.value = res.models.slice(0, 300)
  } catch {
    /* ignore */
  }
}

function openEdit(p: AdminPrinter) {
  editing.value = p
  form.value = {
    name: p.name,
    model: p.model,
    ppd_queue: p.ppd_queue,
    raw_queue: p.raw_queue,
    device_uri: p.device_uri,
    is_default: p.is_default,
    is_shared: p.is_shared,
  }
  showForm.value = true
}

async function save() {
  saving.value = true
  try {
    if (editing.value) {
      await api.put(`/api/admin/printers/${encodeURIComponent(editing.value.name)}`, form.value)
      toast.success('打印机已更新')
    } else {
      const slug = form.value.name
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-|-$/g, '') || 'printer'
      if (!form.value.ppd_queue) form.value.ppd_queue = `${slug}-ppd`
      if (!form.value.raw_queue) form.value.raw_queue = `${slug}-raw`
      await api.post('/api/admin/printers', form.value)
      toast.success('打印机已创建')
    }
    showForm.value = false
    await load()
  } catch (e) {
    toast.error((e as Error).message)
  } finally {
    saving.value = false
  }
}

async function remove(p: AdminPrinter) {
  if (!confirm(`确定删除打印机 ${p.name} 及其队列？`)) return
  try {
    await api.del(`/api/admin/printers/${encodeURIComponent(p.name)}`)
    toast.success('已删除')
    await load()
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function action(p: AdminPrinter, name: string) {
  if (name === 'purge' && !confirm('确定清空该打印机的所有任务？')) return
  try {
    const res = await api.post<{ cups_job_id?: string }>(`/api/admin/printers/${encodeURIComponent(p.name)}/${name}`)
    toast.success(name === 'test' ? `测试页已提交 ${res.cups_job_id || ''}` : '操作成功')
    await load()
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function loadOptions(p: AdminPrinter) {
  optionsFor.value = p.name
  try {
    const res = await api.get<{ options: PPDOption[] }>(`/api/admin/printers/${encodeURIComponent(p.name)}/ppd-options`)
    ppdOptions.value = res.options
    optionValues.value = {}
    for (const o of res.options) optionValues.value[o.keyword] = o.default
  } catch (e) {
    toast.error((e as Error).message)
  }
}

async function saveOptions() {
  if (!optionsFor.value) return
  try {
    await api.put(`/api/admin/printers/${encodeURIComponent(optionsFor.value)}/ppd-options`, { options: optionValues.value })
    toast.success('PPD 默认选项已保存')
  } catch (e) {
    toast.error((e as Error).message)
  }
}

onMounted(load)
</script>

<template>
  <div class="admin-header">
    <div>
      <h1 class="admin-title">打印机</h1>
      <p class="admin-sub">管理物理打印机、PPD / RAW 双队列与默认选项</p>
    </div>
    <button class="btn btn-primary" @click="openCreate">添加打印机</button>
  </div>

  <section v-if="showForm" class="card" style="margin-bottom: 20px">
    <div class="section-head">
      <h2 class="card-title">{{ editing ? '编辑打印机' : '添加打印机' }}</h2>
      <button class="btn btn-ghost btn-sm" @click="showForm = false">关闭</button>
    </div>
    <div class="form-grid">
      <div class="field"><label>名称</label><input v-model="form.name" class="input" placeholder="Brother DCP-7060D" /></div>
      <div class="field">
        <label>驱动 / 型号</label>
        <input v-model="form.model" class="input" list="model-options" placeholder="drv:///brlaser.drv/br7060d.ppd 或 everywhere" />
        <datalist id="model-options">
          <option v-for="m in modelOptions" :key="m.name" :value="m.name">{{ m.description }}</option>
        </datalist>
      </div>
      <div class="field"><label>PPD 队列名</label><input v-model="form.ppd_queue" class="input" :disabled="!!editing" placeholder="dcp7060d-ppd" /></div>
      <div class="field"><label>RAW 队列名</label><input v-model="form.raw_queue" class="input" :disabled="!!editing" placeholder="dcp7060d-raw" /></div>
      <div class="field full"><label>设备地址 device-uri</label><input v-model="form.device_uri" class="input" placeholder="usb://Brother/DCP-7060D?serial=... 或 ipp://..." /></div>
      <div class="field"><label><input v-model="form.is_default" type="checkbox" /> 设为默认打印机</label></div>
      <div class="field"><label><input v-model="form.is_shared" type="checkbox" /> 局域网共享</label></div>
    </div>
    <button class="btn btn-primary" style="margin-top: 16px" :disabled="saving" @click="save">
      {{ saving ? '保存中…' : '保存' }}
    </button>
  </section>

  <section class="card">
    <div v-if="!printers.length" class="empty">还没有打印机，点击右上角添加</div>
    <div v-else class="table-wrap">
      <table class="table">
        <thead>
          <tr><th>名称</th><th>队列</th><th>设备地址</th><th>状态</th><th>队列长度</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="p in printers" :key="p.id">
            <td>
              <strong>{{ p.name }}</strong>
              <div class="admin-sub">{{ p.model || '—' }}{{ p.is_default ? ' · 默认' : '' }}</div>
            </td>
            <td>
              <div>{{ p.ppd_queue }} <span class="admin-sub">(PPD)</span></div>
              <div>{{ p.raw_queue }} <span class="admin-sub">(RAW)</span></div>
            </td>
            <td style="max-width: 260px; word-break: break-all">{{ p.device_uri }}</td>
            <td><StatusBadge :status="p.state" /></td>
            <td>{{ p.queue_length }}</td>
            <td>
              <div style="display: flex; flex-wrap: wrap; gap: 6px">
                <button class="btn btn-ghost btn-sm" @click="openEdit(p)">编辑</button>
                <button class="btn btn-ghost btn-sm" @click="action(p, 'test')">测试页</button>
                <button class="btn btn-ghost btn-sm" @click="action(p, 'pause')">暂停</button>
                <button class="btn btn-ghost btn-sm" @click="action(p, 'resume')">恢复</button>
                <button class="btn btn-ghost btn-sm" @click="action(p, 'purge')">清空</button>
                <button class="btn btn-ghost btn-sm" @click="loadOptions(p)">PPD 选项</button>
                <button class="btn btn-danger btn-sm" @click="remove(p)">删除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>

  <section v-if="optionsFor" class="card" style="margin-top: 20px">
    <div class="section-head">
      <h2 class="card-title">PPD 默认选项 · {{ optionsFor }}</h2>
      <button class="btn btn-ghost btn-sm" @click="optionsFor = ''">关闭</button>
    </div>
    <div v-if="!ppdOptions.length" class="empty">该打印机 PPD 没有可配置选项，或 CUPS 未返回选项</div>
    <div v-else class="form-grid">
      <div v-for="o in ppdOptions" :key="o.keyword" class="field">
        <label>{{ o.text }}（{{ o.keyword }}）</label>
        <select v-model="optionValues[o.keyword]" class="select">
          <option v-for="c in o.choices" :key="c" :value="c">{{ c }}</option>
        </select>
      </div>
    </div>
    <button v-if="ppdOptions.length" class="btn btn-primary" style="margin-top: 16px" @click="saveOptions">保存默认选项</button>
  </section>
</template>
