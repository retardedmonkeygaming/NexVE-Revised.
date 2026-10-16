// Human-readable formatting helpers (binary units, durations).

export function formatBytes(bytes: number, digits = 1): string {
  if (!bytes || bytes < 0) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let v = bytes
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : digits)} ${units[i]}`
}

export function formatRAM(mb: number): string {
  if (!mb) return '—'
  if (mb >= 1024) return `${(mb / 1024).toFixed(mb % 1024 === 0 ? 0 : 1)} GiB`
  return `${mb} MiB`
}

export function formatUptime(seconds: number): string {
  if (!seconds) return '—'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}

export function pct(value: number): string {
  return `${(value ?? 0).toFixed(1)}%`
}

/** Icon/colour tone for an instance state string from libvirt. */
export function stateTone(state: string): 'green' | 'amber' | 'red' | 'gray' {
  const s = (state || '').toLowerCase()
  if (s.startsWith('running')) return 'green'
  if (s.startsWith('paused') || s.startsWith('creating') || s.startsWith('idle')) return 'amber'
  if (s.startsWith('failed') || s.startsWith('error') || s.includes('crash')) return 'red'
  return 'gray'
}
