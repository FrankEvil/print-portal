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

const modeLabels: Record<string, string> = {
  idcard: '身份证',
  invoice: '发票',
  photos: '图片打印',
  booklet: '书籍打印',
}

export function formatJobParams(job: Job): string {
  if (job.source === 'test') {
    const parts = ['测试页', `${job.copies || 1} 份`, job.paper_size || 'A4']
    if (job.duplex) parts.push(duplexLabels[job.duplex] || job.duplex)
    return parts.join(' · ')
  }
  if (job.source !== 'web') {
    return job.cups_user ? `本地直连 · ${job.cups_user}` : '本地直连'
  }
  if (job.print_mode && job.print_mode !== 'document') {
    const parts = [modeLabels[job.print_mode] || job.print_mode, `${job.copies} 份`]
    if (job.pages > 0) parts.push(`${job.pages} 页`)
    return parts.join(' · ')
  }
  const parts = [
    `${job.copies} 份`,
    job.paper_size || 'A4',
    duplexLabels[job.duplex] || job.duplex || '单面',
    job.color_mode === 'color' ? '彩色' : '黑白',
    job.orientation === 'landscape' ? '横向' : '纵向',
  ]
  if (job.page_range) parts.push(`页码 ${job.page_range}`)
  if (job.page_parity === 'odd') parts.push('奇数页')
  if (job.page_parity === 'even') parts.push('偶数页')
  if (job.output_order === 'reverse') parts.push('倒序出纸')
  if (job.pages > 0) parts.push(`${job.pages} 页`)
  if (job.scaling) parts.push(scalingLabels[job.scaling] || job.scaling)
  return parts.join(' · ')
}
