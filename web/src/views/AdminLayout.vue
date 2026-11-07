<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, setAdminToken } from '../api'

const router = useRouter()
const menuOpen = ref(false)
const authMode = ref<'token' | 'fnos'>('token')

onMounted(async () => {
  try {
    const cfg = await api.get<{ mode: 'token' | 'fnos' }>('/api/auth/config')
    authMode.value = cfg.mode
  } catch {
    /* default token */
  }
})

function logout() {
  setAdminToken('')
  router.replace('/admin/login')
}
</script>

<template>
  <div class="admin-shell">
    <aside class="admin-sidebar" :class="{ 'menu-open': menuOpen }">
      <div class="admin-mobile-top">
        <router-link to="/" class="admin-mobile-home" aria-label="返回打印页" title="返回打印页">←</router-link>
        <div class="brand">
          <div class="brand-mark">P</div>
          <div>
            <div class="brand-name">Print Portal</div>
            <div class="brand-sub">管理后台</div>
          </div>
        </div>
        <button class="admin-mobile-toggle" type="button" :aria-expanded="menuOpen" @click="menuOpen = !menuOpen">
          {{ menuOpen ? '×' : '☰' }}
        </button>
      </div>
      <nav class="admin-nav" @click="menuOpen = false">
        <router-link to="/admin" exact-active-class="router-link-active">概览</router-link>
        <router-link to="/admin/printers">打印机</router-link>
        <router-link to="/admin/sharing">共享</router-link>
        <router-link to="/admin/drivers">驱动管理</router-link>
        <router-link to="/admin/cups">CUPS 控制台</router-link>
        <router-link to="/admin/jobs">任务管理</router-link>
        <router-link to="/admin/settings">应用设置</router-link>
        <router-link to="/admin/logs">日志</router-link>
      </nav>
      <div class="admin-sidebar-footer">
        <router-link to="/" class="admin-print-link" style="display: block; padding: 10px 12px; color: var(--text-2); font-size: 14px">← 打印页</router-link>
        <button v-if="authMode !== 'fnos'" class="btn btn-ghost btn-sm" @click="logout">退出登录</button>
      </div>
    </aside>
    <main class="admin-content">
      <router-view />
    </main>
  </div>
</template>
