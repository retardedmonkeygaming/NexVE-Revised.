// Dashboard: node resources (CPU / memory / storage / uptime) + host info.
// Polls the status endpoint every 3 seconds.
import { useEffect, useState } from 'react'
import { Cpu, MemoryStick, HardDrive, Clock, CircuitBoard, Info } from 'lucide-react'
import { api, type NodeStatus } from '../lib/api'
import { formatBytes, formatUptime, pct } from '../lib/format'
import { Badge, Card, CardTitle, ErrorBox, Meter, Spinner } from '../components/ui'

export default function Dashboard() {
  const [status, setStatus] = useState<NodeStatus | null>(null)
  const [error, setError] = useState('')
  const [updatedAt, setUpdatedAt] = useState<Date | null>(null)

  useEffect(() => {
    let alive = true
    const load = async () => {
      try {
        const s = await api.nodeStatus()
        if (!alive) return
        setStatus(s)
        setUpdatedAt(new Date())
        setError('')
      } catch (e: any) {
        if (alive) setError(e.message)
      }
    }
    load()
    const t = window.setInterval(load, 3000)
    return () => {
      alive = false
      window.clearInterval(t)
    }
  }, [])

  return (
    <div className="mx-auto max-w-6xl space-y-6 p-6">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Dashboard</h1>
          <p className="text-sm text-zinc-500">
            {status ? `${status.hostname} · ${status.os}` : 'loading node status…'}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {status && (
            <>
              <Badge tone="indigo">{status.arch}</Badge>
              <Badge tone={status.firmware === 'uefi' ? 'green' : 'gray'}>{status.firmware}</Badge>
              <Badge tone={status.kvm ? 'green' : 'red'}>{status.kvm ? 'KVM ok' : 'no KVM'}</Badge>
            </>
          )}
          <span className="text-xs text-zinc-600">
            {updatedAt ? `updated ${updatedAt.toLocaleTimeString()}` : <Spinner className="align-middle" />}
          </span>
        </div>
      </header>

      <ErrorBox message={error} />

      {/* --- headline stat cards ------------------------------------------- */}
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          icon={<Cpu size={16} className="text-indigo-400" />}
          label="CPU"
          value={status ? pct(status.cpu.percent) : '—'}
          sub={status ? `${status.cpu.model || 'CPU'} · ${status.cpu.logical} threads` : ''}
          meter={status?.cpu.percent ?? 0}
        />
        <StatCard
          icon={<MemoryStick size={16} className="text-emerald-400" />}
          label="Memory"
          value={status ? pct(status.memory.percent) : '—'}
          sub={
            status
              ? `${formatBytes(status.memory.used_bytes)} / ${formatBytes(status.memory.total_bytes)}`
              : ''
          }
          meter={status?.memory.percent ?? 0}
        />
        <StatCard
          icon={<HardDrive size={16} className="text-amber-400" />}
          label="Storage"
          value={status ? pct(status.storage_totals.percent) : '—'}
          sub={
            status
              ? `${formatBytes(status.storage_totals.used_bytes)} / ${formatBytes(status.storage_totals.total_bytes)}`
              : ''
          }
          meter={status?.storage_totals.percent ?? 0}
        />
        <StatCard
          icon={<Clock size={16} className="text-zinc-400" />}
          label="Uptime"
          value={status ? formatUptime(status.uptime_seconds) : '—'}
          sub={status ? `load ${status.cpu.load1} / ${status.cpu.load5} / ${status.cpu.load15}` : ''}
        />
      </div>

      {/* --- details ------------------------------------------------------- */}
      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardTitle sub="physical filesystems reported by the node">Storage</CardTitle>
          <div className="divide-y divide-zinc-800/60">
            {(status?.storage ?? []).map((d) => (
              <div key={d.device + d.mount} className="px-5 py-3.5">
                <div className="mb-1.5 flex items-baseline justify-between text-sm">
                  <span className="font-medium">
                    {d.mount}
                    <span className="ml-2 text-xs text-zinc-500">{d.fs}</span>
                  </span>
                  <span className="text-xs text-zinc-500">
                    {formatBytes(d.used_bytes)} / {formatBytes(d.total_bytes)} · {pct(d.percent)}
                  </span>
                </div>
                <Meter value={d.percent} />
              </div>
            ))}
            {status && status.storage.length === 0 && (
              <p className="px-5 py-6 text-sm text-zinc-500">No physical filesystems found.</p>
            )}
          </div>
        </Card>

        <Card>
          <CardTitle sub="as reported by the node">System</CardTitle>
          <dl className="space-y-2.5 px-5 py-4 text-sm">
            <Row k="Hostname" v={status?.hostname ?? '—'} />
            <Row k="OS" v={status?.os ?? '—'} />
            <Row k="Kernel" v={status?.kernel ?? '—'} />
            <Row k="Architecture" v={status?.arch ?? '—'} />
            <Row k="Boot firmware" v={status?.firmware ?? '—'} />
            <Row
              k="KVM"
              v={status?.kvm ? 'available' : 'unavailable'}
              tone={status?.kvm ? 'ok' : 'bad'}
            />
            <Row
              k="Swap"
              v={
                status
                  ? `${formatBytes(status.memory.swap_used_bytes)} / ${formatBytes(status.memory.swap_total_bytes)}`
                  : '—'
              }
            />
            <Row k="Local time" v={status ? new Date(status.time).toLocaleString() : '—'} />
          </dl>
          <div className="flex items-start gap-2 border-t border-zinc-800/60 px-5 py-3 text-[11px] text-zinc-500">
            <Info size={13} className="mt-0.5 shrink-0" />
            <span>
              Values refresh every 3 seconds. Storage excludes loop/pseudo filesystems.
            </span>
          </div>
        </Card>
      </div>
    </div>
  )
}

function StatCard({
  icon,
  label,
  value,
  sub,
  meter,
}: {
  icon: React.ReactNode
  label: string
  value: string
  sub?: string
  meter?: number
}) {
  return (
    <Card className="p-5">
      <div className="mb-3 flex items-center gap-2 text-xs font-medium text-zinc-400">
        {icon}
        {label}
      </div>
      <div className="text-2xl font-semibold tracking-tight tabular-nums">{value}</div>
      {sub && <div className="mt-1 truncate text-xs text-zinc-500">{sub}</div>}
      {meter !== undefined && <div className="mt-3"><Meter value={meter} /></div>}
      <div className="mt-3">
        <CircuitBoard size={14} className="hidden text-zinc-800" />
      </div>
    </Card>
  )
}

function Row({ k, v, tone }: { k: string; v: string; tone?: 'ok' | 'bad' }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <dt className="text-zinc-500">{k}</dt>
      <dd
        className={
          'truncate text-right ' +
          (tone === 'ok' ? 'text-emerald-400' : tone === 'bad' ? 'text-red-400' : 'text-zinc-200')
        }
      >
        {v}
      </dd>
    </div>
  )
}
