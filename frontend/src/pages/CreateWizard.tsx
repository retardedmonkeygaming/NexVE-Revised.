// Create-instance wizard: basics → resources → media → review.
// Handles both QEMU VMs and LXC containers; ISO upload is real (streamed to
// the node's ISO library).
import { useEffect, useState } from 'react'
import { Upload, Check, Server as ServerIcon, Boxes } from 'lucide-react'
import { api, ApiError, type CreateSpec, type InstanceType, type ISO } from '../lib/api'
import { formatBytes } from '../lib/format'
import { Badge, Button, ErrorBox, Field, Input, Modal, Select, Spinner } from '../components/ui'
import { useToast } from '../components/toast'

const steps = ['Basics', 'Resources', 'Media', 'Review'] as const

export default function CreateWizard({
  open,
  onClose,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  onCreated: () => void
}) {
  const toast = useToast()
  const [step, setStep] = useState(0)
  const [type, setType] = useState<InstanceType>('qemu')
  const [name, setName] = useState('')
  const [vcpus, setVcpus] = useState(2)
  const [ramGB, setRamGB] = useState(2)
  const [diskGB, setDiskGB] = useState(16)
  const [firmware, setFirmware] = useState<'bios' | 'uefi'>('bios')
  const [iso, setIso] = useState('')
  const [suite, setSuite] = useState('stable')
  const [startAfter, setStartAfter] = useState(true)
  const [isos, setIsos] = useState<ISO[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState('')

  // Reset when reopened.
  useEffect(() => {
    if (open) {
      setStep(0)
      setError('')
    }
  }, [open])

  // ISO library is only relevant for VMs.
  useEffect(() => {
    if (!open || type !== 'qemu') return
    api
      .listISOs()
      .then(setIsos)
      .catch(() => setIsos([]))
  }, [open, type])

  const canNext =
    (step === 0 && name.trim().length > 0) ||
    step === 1 ||
    (step === 2 && (type === 'lxc' || true)) ||
    step === 3

  async function submit() {
    setBusy(true)
    setError('')
    const spec: CreateSpec = {
      type,
      name: name.trim(),
      vcpus,
      memory_mb: ramGB * 1024,
      start: startAfter,
      ...(type === 'qemu'
        ? { disk_gb: diskGB, firmware, iso: iso || undefined }
        : { suite }),
    }
    try {
      const inst = await api.createInstance(spec)
      toast(
        'success',
        inst.state === 'creating'
          ? `${inst.name} is being provisioned…`
          : `${inst.name} created`,
      )
      onCreated()
    } catch (e: any) {
      setError(e instanceof ApiError ? e.message : 'creation failed')
    } finally {
      setBusy(false)
    }
  }

  async function upload(file: File) {
    setUploading(true)
    setError('')
    try {
      const up = await api.uploadISO(file)
      toast('success', `Uploaded ${up.name} (${formatBytes(up.size_bytes)})`)
      const list = await api.listISOs()
      setIsos(list)
      setIso(up.name)
    } catch (e: any) {
      setError(e instanceof ApiError ? e.message : 'upload failed')
    } finally {
      setUploading(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={busy ? () => {} : onClose}
      title="Create instance"
      wide
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          {step > 0 && (
            <Button onClick={() => setStep(step - 1)} disabled={busy}>
              Back
            </Button>
          )}
          {step < steps.length - 1 ? (
            <Button variant="primary" onClick={() => setStep(step + 1)} disabled={!canNext}>
              Next
            </Button>
          ) : (
            <Button variant="primary" onClick={submit} disabled={busy}>
              {busy ? (
                <>
                  <Spinner /> Creating…
                </>
              ) : (
                <>
                  <Check size={14} /> Create {type === 'qemu' ? 'VM' : 'container'}
                </>
              )}
            </Button>
          )}
        </>
      }
    >
      {/* step indicator */}
      <div className="mb-5 flex items-center gap-2">
        {steps.map((s, idx) => (
          <div key={s} className="flex flex-1 items-center gap-2">
            <div
              className={
                'grid size-6 shrink-0 place-items-center rounded-full text-[11px] font-semibold ' +
                (idx < step
                  ? 'bg-indigo-600 text-white'
                  : idx === step
                    ? 'bg-indigo-500/20 text-indigo-300 ring-1 ring-indigo-500/50'
                    : 'bg-zinc-800 text-zinc-500')
              }
            >
              {idx + 1}
            </div>
            <span
              className={
                'hidden text-xs sm:block ' + (idx === step ? 'text-zinc-200' : 'text-zinc-600')
              }
            >
              {s}
            </span>
            {idx < steps.length - 1 && <div className="h-px flex-1 bg-zinc-800" />}
          </div>
        ))}
      </div>

      <ErrorBox message={error} />

      {/* --- step 1: basics ------------------------------------------------ */}
      {step === 0 && (
        <div className="space-y-4">
          <Field label="Type">
            <div className="grid grid-cols-2 gap-3">
              <TypeCard
                active={type === 'qemu'}
                onClick={() => setType('qemu')}
                icon={<ServerIcon size={18} />}
                title="Virtual machine"
                desc="Full KVM/QEMU guest with its own kernel. Boot an ISO or cloud image."
              />
              <TypeCard
                active={type === 'lxc'}
                onClick={() => setType('lxc')}
                icon={<Boxes size={18} />}
                title="Container"
                desc="Lightweight LXC guest sharing the host kernel. Built with debootstrap."
              />
            </div>
          </Field>
          <Field
            label="Name"
            hint="Lowercase letters, digits and dashes. Used as the libvirt domain name."
          >
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={type === 'qemu' ? 'my-vm' : 'my-container'}
              autoFocus
            />
          </Field>
        </div>
      )}

      {/* --- step 2: resources --------------------------------------------- */}
      {step === 1 && (
        <div className="grid grid-cols-2 gap-4">
          <Field label="vCPUs">
            <Input
              type="number"
              min={1}
              max={128}
              value={vcpus}
              onChange={(e) => setVcpus(Math.max(1, Number(e.target.value) || 1))}
            />
          </Field>
          <Field label="Memory (GiB)">
            <Input
              type="number"
              min={0.1}
              step={0.5}
              value={ramGB}
              onChange={(e) => setRamGB(Math.max(0.1, Number(e.target.value) || 1))}
            />
          </Field>
          {type === 'qemu' && (
            <>
              <Field label="Disk (GB)" hint="qcow2, created in the default storage pool">
                <Input
                  type="number"
                  min={1}
                  value={diskGB}
                  onChange={(e) => setDiskGB(Math.max(1, Number(e.target.value) || 16))}
                />
              </Field>
              <Field label="Firmware">
                <Select value={firmware} onChange={(e) => setFirmware(e.target.value as any)}>
                  <option value="bios">Legacy BIOS</option>
                  <option value="uefi">UEFI (OVMF)</option>
                </Select>
              </Field>
            </>
          )}
          <label className="col-span-2 flex items-center gap-2 text-sm text-zinc-300">
            <input
              type="checkbox"
              checked={startAfter}
              onChange={(e) => setStartAfter(e.target.checked)}
              className="size-4 rounded border-zinc-700 bg-zinc-900"
            />
            Start immediately after creation
          </label>
        </div>
      )}

      {/* --- step 3: media ------------------------------------------------- */}
      {step === 2 &&
        (type === 'qemu' ? (
          <div className="space-y-4">
            <Field
              label="Installation ISO"
              hint="Pick an image from the node's ISO library, or upload one below. Leave empty to boot from disk only."
            >
              <Select value={iso} onChange={(e) => setIso(e.target.value)}>
                <option value="">— no ISO (empty disk) —</option>
                {(isos ?? []).map((i) => (
                  <option key={i.name} value={i.name}>
                    {i.name} ({formatBytes(i.size_bytes)})
                  </option>
                ))}
              </Select>
            </Field>

            <div className="rounded-lg border border-dashed border-zinc-800 px-4 py-5 text-center">
              {uploading ? (
                <div className="flex items-center justify-center gap-2 text-sm text-zinc-400">
                  <Spinner /> Uploading…
                </div>
              ) : (
                <>
                  <p className="mb-2 text-xs text-zinc-500">
                    {isos === null ? 'loading ISO library…' : `${isos.length} image(s) in the library`}
                  </p>
                  <label className="inline-flex cursor-pointer items-center gap-2 rounded-lg bg-zinc-800 px-3 py-2 text-xs font-medium text-zinc-200 transition-colors hover:bg-zinc-700">
                    <Upload size={14} /> Upload ISO
                    <input
                      type="file"
                      accept=".iso,.img,.raw"
                      className="hidden"
                      onChange={(e) => {
                        const f = e.target.files?.[0]
                        if (f) upload(f)
                        e.target.value = ''
                      }}
                    />
                  </label>
                  <p className="mt-2 text-[11px] text-zinc-600">
                    Uploads go straight to /var/lib/libvirt/images/iso on the node.
                  </p>
                </>
              )}
            </div>
          </div>
        ) : (
          <div className="space-y-4">
            <Field
              label="Debian suite"
              hint="The container rootfs is bootstrapped with debootstrap (minbase). This can take a few minutes."
            >
              <Select value={suite} onChange={(e) => setSuite(e.target.value)}>
                <option value="stable">stable</option>
                <option value="testing">testing</option>
                <option value="bookworm">bookworm (Debian 12)</option>
                <option value="bullseye">bullseye (Debian 11)</option>
              </Select>
            </Field>
            <p className="rounded-lg border border-zinc-800 bg-zinc-950/60 px-3 py-2 text-xs text-zinc-500">
              Container support is experimental in the baseline (libvirt LXC driver). The instance
              appears as <Badge tone="amber">creating</Badge> while the rootfs is built.
            </p>
          </div>
        ))}

      {/* --- step 4: review ------------------------------------------------ */}
      {step === 3 && (
        <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm">
          <Summary k="Type" v={type === 'qemu' ? 'Virtual machine (KVM)' : 'Container (LXC)'} />
          <Summary k="Name" v={name || '—'} />
          <Summary k="vCPUs" v={String(vcpus)} />
          <Summary k="Memory" v={`${ramGB} GiB`} />
          {type === 'qemu' ? (
            <>
              <Summary k="Disk" v={`${diskGB} GB qcow2`} />
              <Summary k="Firmware" v={firmware === 'uefi' ? 'UEFI (OVMF)' : 'Legacy BIOS'} />
              <Summary k="ISO" v={iso || 'none (boot from disk)'} />
            </>
          ) : (
            <Summary k="Rootfs" v={`Debian ${suite} (debootstrap)`} />
          )}
          <Summary k="Network" v="default (NAT)" />
          <Summary k="Start now" v={startAfter ? 'yes' : 'no'} />
        </dl>
      )}
    </Modal>
  )
}

function TypeCard({
  active,
  onClick,
  icon,
  title,
  desc,
}: {
  active: boolean
  onClick: () => void
  icon: React.ReactNode
  title: string
  desc: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={
        'rounded-lg border p-3 text-left transition-colors ' +
        (active
          ? 'border-indigo-500/60 bg-indigo-500/10 ring-1 ring-indigo-500/40'
          : 'border-zinc-800 bg-zinc-950/50 hover:border-zinc-700')
      }
    >
      <div className="mb-1.5 flex items-center gap-2 text-sm font-medium text-zinc-100">
        {icon}
        {title}
      </div>
      <p className="text-[11px] leading-relaxed text-zinc-500">{desc}</p>
    </button>
  )
}

function Summary({ k, v }: { k: string; v: string }) {
  return (
    <div>
      <dt className="text-xs text-zinc-500">{k}</dt>
      <dd className="font-medium text-zinc-200">{v}</dd>
    </div>
  )
}
