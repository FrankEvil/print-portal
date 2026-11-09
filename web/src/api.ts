import { ref } from 'vue'

const TOKEN_KEY = 'print_portal_admin_token'

// When the app is opened through the fnOS unified gateway, all API calls
// must stay under /app/<appname>; direct deployments keep the root prefix.
export const apiBase = (() => {
  const match = window.location.pathname.match(/^\/app\/[^/]+/)
  return match ? match[0] : ''
})()

export function apiUrl(path: string): string {
  return path.startsWith('/api/') ? apiBase + path : path
}

export const adminToken = ref<string>(sessionStorage.getItem(TOKEN_KEY) || '')

export function setAdminToken(token: string) {
  adminToken.value = token
  if (token) sessionStorage.setItem(TOKEN_KEY, token)
  else sessionStorage.removeItem(TOKEN_KEY)
}

export class ApiRequestError extends Error {
  status: number
  code: string
  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers)
  if (adminToken.value) headers.set('X-Admin-Token', adminToken.value)
  if (options.body && !(options.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }
  const res = await fetch(apiUrl(path), { ...options, headers })
  if (!res.ok) {
    let code = 'request_failed'
    let message = `HTTP ${res.status}`
    try {
      const body = await res.json()
      if (body?.error) {
        code = body.error.code || code
        message = body.error.message || message
      }
    } catch {
      /* ignore */
    }
    throw new ApiRequestError(res.status, code, message)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export const api = {
  get<T>(path: string): Promise<T> {
    return request<T>(path)
  },
  post<T>(path: string, body?: unknown): Promise<T> {
    return request<T>(path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) })
  },
  put<T>(path: string, body: unknown): Promise<T> {
    return request<T>(path, { method: 'PUT', body: JSON.stringify(body) })
  },
  del<T>(path: string): Promise<T> {
    return request<T>(path, { method: 'DELETE' })
  },
  upload<T>(path: string, form: FormData): Promise<T> {
    return request<T>(path, { method: 'POST', body: form })
  },
  // Upload with progress: fetch() cannot report upload bytes, so a large file
  // otherwise looks frozen until the server replies.
  uploadWithProgress<T>(path: string, form: FormData, onProgress: (ratio: number) => void): Promise<T> {
    return new Promise<T>((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('POST', apiUrl(path))
      if (adminToken.value) xhr.setRequestHeader('X-Admin-Token', adminToken.value)
      xhr.upload.onprogress = (event) => {
        if (event.lengthComputable) onProgress(event.loaded / event.total)
      }
      xhr.onload = () => {
        let payload: any = null
        try {
          payload = JSON.parse(xhr.responseText)
        } catch {
          /* ignore */
        }
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve(payload as T)
          return
        }
        const message = payload?.error?.message || `HTTP ${xhr.status}`
        reject(new ApiRequestError(xhr.status, payload?.error?.code || 'request_failed', message))
      }
      xhr.onerror = () => reject(new ApiRequestError(0, 'network_error', '上传失败，请检查网络'))
      xhr.onabort = () => reject(new ApiRequestError(0, 'aborted', '已取消'))
      xhr.send(form)
    })
  },
}
