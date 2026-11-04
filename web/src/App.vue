<script setup lang="ts">
import { ref } from 'vue'

const tab = ref<'print' | 'admin'>('print')
</script>

<template>
  <main class="shell">
    <header class="topbar">
      <div>
        <p class="eyebrow">Print Portal</p>
        <h1>打印门户</h1>
      </div>
      <div class="tabs">
        <button :class="{ active: tab === 'print' }" @click="tab = 'print'">打印</button>
        <button :class="{ active: tab === 'admin' }" @click="tab = 'admin'">管理</button>
      </div>
    </header>

    <section v-if="tab === 'print'" class="card">
      <h2>网页打印</h2>
      <p class="muted">P1 将在这里支持 PDF、图片、Office 上传，以及页码、双面、份数设置。</p>
      <label class="dropzone">
        <input type="file" accept=".pdf,.jpg,.jpeg,.png" disabled />
        <span>拖拽文件到这里，或点击选择</span>
      </label>
      <button class="primary" disabled>提交打印（脚手架占位）</button>
    </section>

    <section v-else class="card">
      <h2>管理中心</h2>
      <p class="muted">P1 将在这里提供打印机、CUPS 参数、任务、设备发现和扫描管理。</p>
      <ul class="plain">
        <li>打印机配置：PPD / RAW 队列、设备地址、驱动</li>
        <li>CUPS 控制台：状态、参数、日志、备份</li>
        <li>设备发现：USB / IPP / AirPrint / 扫描仪</li>
        <li>任务管理：队列、取消、重试</li>
      </ul>
    </section>

    <footer class="footer">
      单用户 · 局域网 · 自带 CUPS · PPD + RAW
    </footer>
  </main>
</template>
