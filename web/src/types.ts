export interface Printer {
  id: number
  name: string
  model: string
  queue: string
  is_default: boolean
  state: string
  state_reasons: string[]
}

export interface AdminPrinter {
  id: number
  name: string
  model: string
  ppd_queue: string
  raw_queue: string
  device_uri: string
  ppd_file: string
  is_default: boolean
  is_shared: boolean
  state: string
  state_reasons: string[]
  queue_length: number
  ppd_address: string
  raw_address: string
  created_at: string
  updated_at: string
}

export type JobStatus =
  | 'pending'
  | 'converting'
  | 'submitted'
  | 'queued'
  | 'printing'
  | 'completed'
  | 'failed'
  | 'cancelled'

export interface Job {
  id: number
  printer_id: number
  printer_name: string
  source: 'web' | 'local'
  filename: string
  source_ip: string
  cups_user: string
  pages: number
  page_range: string
  duplex: string
  copies: number
  paper_size: string
  orientation: string
  color_mode: string
  scaling: string
  status: JobStatus
  cups_job_id: string
  error_message: string
  preview_path?: string
  created_at: string
  updated_at: string
}

export interface Driver {
  id: number
  name: string
  version: string
  kind: 'builtin' | 'package' | 'ppd'
  source: string
  state: 'ready' | 'degraded' | 'missing'
  models_count: number
  license: string
  installed_at: string
  updated_at: string
}

export interface Model {
  name: string
  description: string
  driver: string
}

export interface PPDOption {
  keyword: string
  text: string
  default: string
  choices: string[]
}

export interface Overview {
  cups: {
    version: string
    state: string
    server_name: string
    ports: number[]
    web_interface: boolean
    share_printers: boolean
  }
  printers: number
  active_jobs: number
  completed_jobs_today: number
  default_printer: string
}

export interface Device {
  id: number
  kind: string
  name: string
  model: string
  device_uri: string
  ip: string
  source: string
  metadata: string
  last_seen: string
}

export interface ApiError {
  error: { code: string; message: string }
}
