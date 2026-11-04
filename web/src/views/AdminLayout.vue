<script setup lang="ts">
import { useRouter } from 'vue-router'
import { setAdminToken } from '../api'

const router = useRouter()

function logout() {
  setAdminToken('')
  router.replace('/admin/login')
}
</script>

<template>
  <div class="admin-shell">
    <aside class="admin-sidebar">
      <div class="brand">
        <div class="brand-mark">P</div>
        <div>
          <div class="brand-name">Print Portal</div>
          <div class="brand-sub">管理后台</div>
        </div>
      </div>
      <nav class="admin-nav">
        <router-link to="/admin" exact-active-class="router-link-active">概览</router-link>
        <router-link to="/admin/printers">打印机</router-link>
        <router-link to="/admin/drivers">驱动管理</router-link>
        <router-link to="/admin/cups">CUPS 控制台</router-link>
        <router-link to="/admin/jobs">任务管理</router-link>
        <router-link to="/admin/settings">应用设置</router-link>
        <router-link to="/admin/logs">日志</router-link>
      </nav>
      <div style="margin-top: auto; display: flex; flex-direction: column; gap: 6px">
        <router-link to="/" style="display: block; padding: 10px 12px; color: var(--text-2); font-size: 14px">← 打印页</router-link>
        <button class="btn btn-ghost btn-sm" @click="logout">退出登录</button>
      </div>
    </aside>
    <main class="admin-content">
      <router-view />
    </main>
  </div>
</template>
