<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, setAdminToken, adminToken } from '../api'
import { toast } from '../toast'

const route = useRoute()
const router = useRouter()
const token = ref(adminToken.value)
const loading = ref(false)

async function login() {
  if (!token.value.trim()) return toast.error('请输入 ADMIN_TOKEN')
  loading.value = true
  const previous = adminToken.value
  setAdminToken(token.value.trim())
  try {
    await api.post('/api/auth/login', { token: token.value.trim() })
    toast.success('登录成功')
    router.replace((route.query.redirect as string) || '/')
  } catch (e) {
    setAdminToken(previous)
    toast.error((e as Error).message)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <main class="page" style="max-width: 440px; padding-top: 12vh">
    <div class="brand" style="justify-content: center; margin-bottom: 22px">
      <div class="brand-mark">P</div>
      <div>
        <div class="brand-name">Print Portal</div>
        <div class="brand-sub">访问登录</div>
      </div>
    </div>
    <div class="card">
      <h1 class="card-title" style="font-size: 22px">Print Portal 登录</h1>
      <p class="card-desc">输入部署时配置的访问 Token</p>
      <div class="field" style="margin-top: 18px">
        <label>访问 Token</label>
        <input v-model="token" type="password" class="input" placeholder="请输入访问令牌" @keyup.enter="login" />
      </div>
      <button class="btn btn-primary" style="width: 100%; margin-top: 16px" :disabled="loading" @click="login">
        {{ loading ? '验证中…' : '登录' }}
      </button>
      <p class="card-desc" style="text-align: center; margin-top: 14px">
        <router-link to="/">返回打印页</router-link>
      </p>
    </div>
  </main>
</template>
