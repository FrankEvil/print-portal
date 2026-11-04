import { ref } from 'vue'

const TOKEN_KEY = 'print_portal_admin_token'

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
  const res = await fetch(path, { ...options, headers })
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
}
