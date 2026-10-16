// Typed REST client for the NexVE backend.
// Auth is stored in sessionStorage: either a bearer token or HTTP basic
// credentials (base64 "user:pass") – whatever was used at login.

export type InstanceType = 'qemu' | 'lxc'

export interface Instance {
  id: string
  name: string
  type: InstanceType
  state: string
  running: boolean
  vcpus: number
  memory_mb: number
  disk_gb: number
  firmware: string
  autostart: boolean
  detail?: string
}

export interface CpuStatus {
  model: string
  logical: number
  physical: number
  percent: number
  load1: number
  load5: number
  load15: number
}

export interface MemoryStatus {
  total_bytes: number
  used_bytes: number
  available_bytes: number
  percent: number
  swap_total_bytes: number
  swap_used_bytes: number
}

export interface DiskStatus {
  device: string
  mount: string
  fs: string
  total_bytes: number
  used_bytes: number
  percent: number
}

export interface StorageTotals {
  total_bytes: number
  used_bytes: number
  free_bytes: number
  percent: number
}

export interface NodeStatus {
  time: string
  hostname: string
  os: string
  kernel: string
  arch: string
  firmware: string
  uptime_seconds: number
  cpu: CpuStatus
  memory: MemoryStatus
  storage: DiskStatus[]
  storage_totals: StorageTotals
  kvm: boolean
}

export interface ISO {
  name: string
  path: string
  size_bytes: number
}

export interface ConsoleInfo {
  mode: string
  endpoint: string
  hint: string
}

export interface CreateSpec {
  type: InstanceType
  name: string
  vcpus: number
  memory_mb: number
  disk_gb?: number
  iso?: string
  firmware?: 'bios' | 'uefi'
  network?: string
  suite?: string
  start?: boolean
}

export interface Health {
  status: string
  version: string
  driver: string
  time: string
}

// ---------------------------------------------------------------------------
// Auth storage
// ---------------------------------------------------------------------------

export interface Auth {
  kind: 'bearer' | 'basic'
  value: string // token, or base64("user:password")
  label: string // what to show in the UI ("admin" or "token…")
}

const AUTH_KEY = 'nexve.auth'

export function loadAuth(): Auth | null {
  try {
    const raw = sessionStorage.getItem(AUTH_KEY)
    return raw ? (JSON.parse(raw) as Auth) : null
  } catch {
    return null
  }
}

export function saveAuth(auth: Auth | null) {
  if (auth) sessionStorage.setItem(AUTH_KEY, JSON.stringify(auth))
  else sessionStorage.removeItem(AUTH_KEY)
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
    this.name = 'ApiError'
  }
}

// ---------------------------------------------------------------------------
// Request plumbing
// ---------------------------------------------------------------------------

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  const auth = loadAuth()
  if (auth) {
    headers.set(
      'Authorization',
      auth.kind === 'bearer' ? `Bearer ${auth.value}` : `Basic ${auth.value}`,
    )
  }
  if (init.body && !(init.body instanceof FormData)) {
    headers.set('Content-Type', 'application/json')
  }

  let res: Response
  try {
    res = await fetch(path, { ...init, headers })
  } catch {
    throw new ApiError(0, 'cannot reach the NexVE backend')
  }

  if (res.status === 401) {
    saveAuth(null)
    throw new ApiError(401, 'unauthorized – please sign in again')
  }
  const text = await res.text()
  let data: any = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = null
    }
  }
  if (!res.ok) {
    throw new ApiError(res.status, data?.error ?? res.statusText ?? 'request failed')
  }
  return data as T
}

// ---------------------------------------------------------------------------
// Endpoints
// ---------------------------------------------------------------------------

export const api = {
  // public (no auth)
  health: () => request<Health>('/api/v1/health'),

  // node
  nodeStatus: () => request<NodeStatus>('/api/v1/node/status'),

  // instances
  listInstances: async () =>
    (await request<{ instances: Instance[] }>('/api/v1/vms')).instances,
  getInstance: (id: string) => request<Instance>(`/api/v1/vms/${encodeURIComponent(id)}`),
  createInstance: (spec: CreateSpec) =>
    request<Instance>('/api/v1/vms', { method: 'POST', body: JSON.stringify(spec) }),
  startInstance: (id: string) =>
    request<Instance>(`/api/v1/vms/${encodeURIComponent(id)}/start`, { method: 'POST' }),
  stopInstance: (id: string, force = false) =>
    request<Instance>(
      `/api/v1/vms/${encodeURIComponent(id)}/stop${force ? '?force=1' : ''}`,
      { method: 'POST' },
    ),
  deleteInstance: (id: string, purge: boolean) =>
    request<{ deleted: string }>(
      `/api/v1/vms/${encodeURIComponent(id)}${purge ? '?purge=1' : ''}`,
      { method: 'DELETE' },
    ),
  console: (id: string) =>
    request<ConsoleInfo>(`/api/v1/vms/${encodeURIComponent(id)}/console`),

  // ISO library
  listISOs: async () => (await request<{ isos: ISO[] }>('/api/v1/isos')).isos,
  uploadISO: (file: File) => {
    const fd = new FormData()
    fd.append('iso', file, file.name)
    return request<ISO>('/api/v1/isos', { method: 'POST', body: fd })
  },
}
