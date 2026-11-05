import type { Job } from './types'

const duplexLabels: Record<string, string> = {
  'one-sided': '单面',
  'two-sided-long-edge': '双面（长边）',
  'two-sided-short-edge': '双面（短边）',
}

const scalingLabels: Record<string, string> = {
  auto: '自动缩放',
  fit: '适应页面',
  fill: '填满页面',
  none: '原始大小',
}

export function formatJobParams(job: Job): string {
  if (job.source !== 'web') {
    return job.cups_user ? `本地直连 · ${job.cups_user}` : '本地直连'
  }
  const parts = [
    `${job.copies} 份`,
    job.paper_size || 'A4',
    duplexLabels[job.duplex] || job.duplex || '单面',
    job.color_mode === 'color' ? '彩色' : '黑白',
    job.orientation === 'landscape' ? '横向' : '纵向',
  ]
  if (job.page_range) parts.push(`页码 ${job.page_range}`)
  if (job.pages > 0) parts.push(`${job.pages} 页`)
  if (job.scaling) parts.push(scalingLabels[job.scaling] || job.scaling)
  return parts.join(' · ')
}
