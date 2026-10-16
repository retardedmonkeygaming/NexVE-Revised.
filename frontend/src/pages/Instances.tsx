// Instances page: list of VMs/containers with start/stop/console/delete,
// plus the create-instance wizard (opened as a full dialog).
import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Plus,
  Play,
  Square,
  Monitor,
  Trash2,
  RefreshCw,
  Search,
  Boxes,
  Server as ServerIcon,
  Copy,
} from 'lucide-react'
import { api, ApiError, type Instance } from '../lib/api'
import { formatRAM, stateTone } from '../lib/format'
import {
  Badge,
  Button,
  Card,
  EmptyState,
  ErrorBox,
  Input,
  Modal,
  Spinner,
  Select,
} from '../components/ui'
import { useToast } from '../components/toast'
import CreateWizard from './CreateWizard'

export default function Instances() {
  const toast = useToast()
  const [items, setItems] = useState<Instance[] | null>(null)
  const [error, setError] = useState('')
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<'all' | 'qemu' | 'lxc'>('all')
  const [busyId, setBusyId] = useState<string | null>(null)
  const [wizardOpen, setWizardOpen] = useState(false)
  const [consoleFor, setConsoleFor] = useState<{ id: string; info: any } | null>(null)
  const [deleteFor, setDeleteFor] = useState<Instance | null>(null)
  const [purge, setPurge] = useState(true)
  const [stopFor, setStopFor] = useState<Instance | null>(null)
  const [stopForce, setStopForce] = useState(false)

  const load = useCallback(async () => {
    try {
      const list = await api.listInstances()
      setItems(list)
      setError('')
    } catch (e: any) {
      setError(e.message)
      setItems((prev) => prev ?? [])
    }
  }, [])

  useEffect(() => {
    load()
    const t = window.setInterval(load, 5000)
    return () => window.clearInterval(t)
  }, [load])

  const visible = useMemo(() => {
    const list = items ?? []
    return list.filter(
      (i) =>
        (filter === 'all' || i.type === filter) &&
        i.name.toLowerCase().includes(query.trim().toLowerCase()),
    )
  }, [items, filter, query])

  async function act(id: string, fn: () => Promise<unknown>, okMsg: string) {
    setBusyId(id)
    try {
      await fn()
      toast('success', okMsg)
      await load()
    } catch (e: any) {
      toast('error', e instanceof ApiError ? e.message : 'operation failed')
    } finally {
      setBusyId(null)
    }
  }

  const running = (i: Instance) => i.running

  return (
    <div className="mx-auto max-w-6xl space-y-4 p-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">VMs &amp; Containers</h1>
          <p className="text-sm text-zinc-500">
            {items === null ? 'loading…' : `${items.length} instance(s)`}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" onClick={load} disabled={items === null}>
            <RefreshCw size={14} className={items === null ? 'animate-spin' : ''} /> Refresh
          </Button>
          <Button size="sm" variant="primary" onClick={() => setWizardOpen(true)}>
            <Plus size={14} /> Create
          </Button>
        </div>
      </header>

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative flex-1 min-w-48">
          <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-zinc-600" />
          <Input
            placeholder="Search by name…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="pl-8"
          />
        </div>
        <Select
          value={filter}
          onChange={(e) => setFilter(e.target.value as any)}
          className="w-40"
        >
          <option value="all">All types</option>
          <option value="qemu">Virtual machines</option>
          <option value="lxc">Containers</option>
        </Select>
      </div>

      <ErrorBox message={error} />

      <Card>
        {items === null ? (
          <div className="flex justify-center py-16">
            <Spinner className="size-6 text-zinc-500" />
          </div>
        ) : visible.length === 0 ? (
          <EmptyState
            title={items.length === 0 ? 'No instances yet' : 'No matches'}
            hint={
              items.length === 0
                ? 'Create your first VM or container – it only takes a minute.'
                : 'Try a different search term or filter.'
            }
            action={
              items.length === 0 ? (
                <Button variant="primary" onClick={() => setWizardOpen(true)}>
                  <Plus size={14} /> Create instance
                </Button>
              ) : undefined
            }
          />
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-zinc-800/80 text-left text-xs text-zinc-500">
                <th className="px-5 py-3 font-medium">Name</th>
                <th className="px-3 py-3 font-medium">State</th>
                <th className="px-3 py-3 font-medium">vCPU</th>
                <th className="px-3 py-3 font-medium">Memory</th>
                <th className="px-3 py-3 font-medium">Disk</th>
                <th className="px-3 py-3 font-medium">Firmware</th>
                <th className="px-5 py-3 text-right font-medium">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-800/60">
              {visible.map((i) => (
                <tr key={i.id} className="group transition-colors hover:bg-zinc-800/25">
                  <td className="px-5 py-3">
                    <div className="flex items-center gap-2.5">
                      <span className="grid size-7 place-items-center rounded-lg bg-zinc-800/80 text-zinc-400">
                        {i.type === 'qemu' ? <ServerIcon size={14} /> : <Boxes size={14} />}
                      </span>
                      <div>
                        <div className="font-medium text-zinc-100">{i.name}</div>
                        <div className="text-[11px] text-zinc-600">
                          {i.type === 'qemu' ? 'virtual machine' : 'container'}
                          {i.detail ? ` · ${i.detail}` : ''}
                        </div>
                      </div>
                    </div>
                  </td>
                  <td className="px-3 py-3">
                    <Badge tone={stateTone(i.state)}>{i.state}</Badge>
                  </td>
                  <td className="px-3 py-3 tabular-nums text-zinc-300">{i.vcpus}</td>
                  <td className="px-3 py-3 tabular-nums text-zinc-300">{formatRAM(i.memory_mb)}</td>
                  <td className="px-3 py-3 tabular-nums text-zinc-300">
                    {i.disk_gb ? `${i.disk_gb} GB` : '—'}
                  </td>
                  <td className="px-3 py-3 text-zinc-400">{i.type === 'qemu' ? i.firmware : '—'}</td>
                  <td className="px-5 py-3">
                    <div className="flex justify-end gap-1.5">
                      {busyId === i.id ? (
                        <Spinner className="my-1" />
                      ) : (
                        <>
                          <ActionBtn
                            label="Start"
                            disabled={running(i)}
                            onClick={() => act(i.id, () => api.startInstance(i.id), `Starting ${i.name}…`)}
                            icon={<Play size={13} />}
                          />
                          <ActionBtn
                            label="Stop"
                            disabled={!running(i)}
                            onClick={() => {
                              setStopFor(i)
                              setStopForce(false)
                            }}
                            icon={<Square size={13} />}
                          />
                          <ActionBtn
                            label="Console"
                            disabled={!running(i)}
                            onClick={async () => {
                              try {
                                const info = await api.console(i.id)
                                setConsoleFor({ id: i.id, info })
                              } catch (e: any) {
                                toast('error', e.message)
                              }
                            }}
                            icon={<Monitor size={13} />}
                          />
                          <ActionBtn
                            label="Delete"
                            danger
                            onClick={() => {
                              setDeleteFor(i)
                              setPurge(true)
                            }}
                            icon={<Trash2 size={13} />}
                          />
                        </>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      {/* --- dialogs ------------------------------------------------------ */}
      <CreateWizard
        open={wizardOpen}
        onClose={() => setWizardOpen(false)}
        onCreated={() => {
          setWizardOpen(false)
          load()
        }}
      />

      <ConsoleModal
        data={consoleFor}
        onClose={() => setConsoleFor(null)}
        onCopied={() => toast('success', 'Copied to clipboard')}
      />

      <Modal
        open={!!stopFor}
        onClose={() => setStopFor(null)}
        title={`Stop ${stopFor?.name ?? ''}`}
        footer={
          <>
            <Button onClick={() => setStopFor(null)}>Cancel</Button>
            <Button
              variant="primary"
              onClick={() => {
                const i = stopFor!
                setStopFor(null)
                act(i.id, () => api.stopInstance(i.id, stopForce), `Stopping ${i.name}…`)
              }}
            >
              {stopForce ? 'Force stop' : 'Graceful shutdown'}
            </Button>
          </>
        }
      >
        <label className="flex items-center gap-2 text-sm text-zinc-300">
          <input
            type="checkbox"
            checked={stopForce}
            onChange={(e) => setStopForce(e.target.checked)}
            className="size-4 rounded border-zinc-700 bg-zinc-900"
          />
          Force stop (pull the plug – unsaved data is lost)
        </label>
      </Modal>

      <Modal
        open={!!deleteFor}
        onClose={() => setDeleteFor(null)}
        title={`Delete ${deleteFor?.name ?? ''}`}
        footer={
          <>
            <Button onClick={() => setDeleteFor(null)}>Cancel</Button>
            <Button
              variant="danger"
              onClick={() => {
                const i = deleteFor!
                setDeleteFor(null)
                act(i.id, () => api.deleteInstance(i.id, purge), `Deleted ${i.name}`)
              }}
            >
              <Trash2 size={14} /> Delete
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <p className="text-sm text-zinc-300">
            This removes the instance definition from libvirt.
          </p>
          <label className="flex items-center gap-2 text-sm text-zinc-300">
            <input
              type="checkbox"
              checked={purge}
              onChange={(e) => setPurge(e.target.checked)}
              className="size-4 rounded border-zinc-700 bg-zinc-900"
            />
            Also delete its disk image / rootfs (cannot be undone)
          </label>
        </div>
      </Modal>
    </div>
  )
}

function ActionBtn({
  label,
  icon,
  disabled,
  danger,
  onClick,
}: {
  label: string
  icon: React.ReactNode
  disabled?: boolean
  danger?: boolean
  onClick: () => void
}) {
  return (
    <Button
      size="sm"
      variant={danger ? 'danger' : 'ghost'}
      disabled={disabled}
      onClick={onClick}
      title={label}
    >
      {icon}
      <span className="hidden lg:inline">{label}</span>
    </Button>
  )
}

function ConsoleModal({
  data,
  onClose,
  onCopied,
}: {
  data: { id: string; info: any } | null
  onClose: () => void
  onCopied: () => void
}) {
  return (
    <Modal open={!!data} onClose={onClose} title={`Console – ${data?.id ?? ''}`}>
      {data && (
        <div className="space-y-3">
          <Badge tone={data.info.mode === 'vnc' ? 'indigo' : 'gray'}>{data.info.mode}</Badge>
          <div className="rounded-lg border border-zinc-800 bg-zinc-950/70 px-3 py-2 font-mono text-xs text-zinc-300">
            {data.info.endpoint}
          </div>
          <pre className="whitespace-pre-wrap rounded-lg bg-zinc-950/70 px-3 py-2 text-xs text-zinc-400">
            {data.info.hint}
          </pre>
          <p className="text-[11px] text-zinc-600">
            Browser-based console (noVNC) is on the roadmap; for now use the command above.
          </p>
          <Button
            size="sm"
            onClick={() => {
              navigator.clipboard?.writeText(data.info.hint).then(onCopied).catch(() => {})
            }}
          >
            <Copy size={13} /> Copy instructions
          </Button>
        </div>
      )}
    </Modal>
  )
}
