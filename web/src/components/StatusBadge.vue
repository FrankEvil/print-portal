<script setup lang="ts">
const props = defineProps<{ status: string }>()

const labels: Record<string, string> = {
  pending: '待处理',
  converting: '转换中',
  submitted: '已提交',
  queued: '排队中',
  held: '已暂停',
  paused: '已暂停',
  printing: '打印中',
  completed: '已完成',
  failed: '失败',
  cancelled: '已取消',
  ready: '可用',
  degraded: '降级',
  missing: '缺失',
  idle: '空闲',
  stopped: '已停止',
  unknown: '未知',
}

const cls = (s: string) => {
  const map: Record<string, string> = {
    pending: 'neutral',
    converting: 'info',
    submitted: 'info',
    queued: 'warning',
    held: 'warning',
    paused: 'warning',
    printing: 'accent',
    completed: 'success',
    failed: 'danger',
    cancelled: 'neutral',
    ready: 'success',
    degraded: 'warning',
    missing: 'danger',
    idle: 'success',
    stopped: 'danger',
  }
  return map[s] || 'neutral'
}
</script>

<template>
  <span class="status-badge" :class="`is-${cls(props.status)}`">
    <span class="status-dot" />
    {{ labels[props.status] || props.status }}
  </span>
</template>
