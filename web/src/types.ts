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
  allow_discovery: boolean
  airprint_enabled: boolean
  state: string
  state_reasons: string[]
  accepting: boolean
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
  | 'held'
  | 'paused'
  | 'printing'
  | 'completed'
  | 'failed'
  | 'cancelled'

export interface Job {
  id: number
  printer_id: number
  printer_name: string
  source: 'web' | 'local' | 'test'
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
  brand: string
}

export interface PPDOption {
  keyword: string
  text: string
  default: string
  choices: string[]
}

export interface PaperArea {
  page_size: string
  width: number
  height: number
  left: number
  bottom: number
  right: number
  top: number
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
  brand: string
  device_uri: string
  ip: string
  source: string
  metadata: string
  last_seen: string
}

export interface ApiError {
  error: { code: string; message: string }
}

export interface Scanner {
  name: string
  uri: string
}

export interface ScanJob {
  id: number
  scanner: string
  filename: string
  format: string
  resolution: number
  color_mode: string
  status: string
  created_at: string
  updated_at: string
}

export interface SharingPrinter {
  id: number
  name: string
  model: string
  state: string
  is_default: boolean
  is_shared: boolean
  allow_discovery: boolean
  airprint_enabled: boolean
  device_uri: string
  ppd_queue: string
  raw_queue: string
  ppd_uri: string
  raw_uri: string
  web_url: string
}

export interface SharingInfo {
  auth_mode?: 'token' | 'fnos'
  public_host: string
  cups_port: string
  web_port: string
  printers: SharingPrinter[]
}

export interface MaintenanceAction {
  id: string
  label: string
  description: string
  timeout: number
  confirm: boolean
}

export interface MaintenanceCapabilities {
  driver: string
  status: 'ready' | 'none' | 'parsing'
  message?: string
  actions: MaintenanceAction[] | null
}

export interface AuthConfig {
  mode: 'token' | 'fnos'
  token_login: boolean
  auth_required: boolean
}
