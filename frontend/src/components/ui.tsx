// Small, hand-rolled UI primitives in the spirit of shadcn/ui – no extra
// dependency, fully Tailwind-styled, dark-first.
import { useEffect, type ReactNode } from 'react'
import { X } from 'lucide-react'

export function cn(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(' ')
}

// ---------------------------------------------------------------------------
// Button
// ---------------------------------------------------------------------------

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'

const btnVariants: Record<ButtonVariant, string> = {
  primary:
    'bg-indigo-600 text-white hover:bg-indigo-500 focus-visible:ring-indigo-500 disabled:bg-indigo-600/50',
  secondary:
    'bg-zinc-800 text-zinc-100 hover:bg-zinc-700 focus-visible:ring-zinc-500 disabled:opacity-50',
  ghost: 'bg-transparent text-zinc-300 hover:bg-zinc-800/70 hover:text-zinc-100 focus-visible:ring-zinc-600',
  danger:
    'bg-transparent text-red-400 border border-red-900/60 hover:bg-red-950/60 hover:text-red-300 focus-visible:ring-red-500',
}

export function Button({
  variant = 'secondary',
  size = 'md',
  className,
  children,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant
  size?: 'sm' | 'md'
}) {
  return (
    <button
      className={cn(
        'inline-flex items-center justify-center gap-1.5 rounded-lg font-medium transition-colors',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-950',
        'disabled:cursor-not-allowed',
        size === 'sm' ? 'px-2.5 py-1.5 text-xs' : 'px-3.5 py-2 text-sm',
        btnVariants[variant],
        className,
      )}
      {...props}
    >
      {children}
    </button>
  )
}

// ---------------------------------------------------------------------------
// Card / section
// ---------------------------------------------------------------------------

export function Card({
  className,
  children,
}: {
  className?: string
  children: ReactNode
}) {
  return (
    <div
      className={cn(
        'rounded-xl border border-zinc-800/80 bg-zinc-900/60 shadow-lg shadow-black/20',
        className,
      )}
    >
      {children}
    </div>
  )
}

export function CardTitle({ children, sub }: { children: ReactNode; sub?: ReactNode }) {
  return (
    <div className="border-b border-zinc-800/80 px-5 py-3.5">
      <h2 className="text-sm font-semibold text-zinc-100">{children}</h2>
      {sub && <p className="mt-0.5 text-xs text-zinc-500">{sub}</p>}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Badge
// ---------------------------------------------------------------------------

const badgeTones = {
  green: 'bg-emerald-500/10 text-emerald-400 ring-emerald-500/30',
  amber: 'bg-amber-500/10 text-amber-400 ring-amber-500/30 animate-pulse',
  red: 'bg-red-500/10 text-red-400 ring-red-500/30',
  gray: 'bg-zinc-500/10 text-zinc-400 ring-zinc-500/30',
  indigo: 'bg-indigo-500/10 text-indigo-400 ring-indigo-500/30',
} as const

export type BadgeTone = keyof typeof badgeTones

export function Badge({
  tone = 'gray',
  className,
  children,
}: {
  tone?: BadgeTone
  className?: string
  children: ReactNode
}) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium ring-1 ring-inset',
        badgeTones[tone],
        className,
      )}
    >
      {children}
    </span>
  )
}

// ---------------------------------------------------------------------------
// Meter (progress bar)
// ---------------------------------------------------------------------------

export function Meter({
  value,
  tone = 'indigo',
}: {
  value: number // 0..100
  tone?: 'indigo' | 'emerald' | 'amber' | 'red'
}) {
  const v = Math.max(0, Math.min(100, value || 0))
  const fill =
    v > 90 ? 'bg-red-500' : v > 75 ? 'bg-amber-500' : tone === 'emerald' ? 'bg-emerald-500' : 'bg-indigo-500'
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-zinc-800">
      <div className={cn('meter-fill h-full rounded-full', fill)} style={{ width: `${v}%` }} />
    </div>
  )
}

// ---------------------------------------------------------------------------
// Form inputs
// ---------------------------------------------------------------------------

export function Field({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: ReactNode
}) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium text-zinc-400">{label}</span>
      {children}
      {hint && <span className="block text-[11px] text-zinc-600">{hint}</span>}
    </label>
  )
}

const inputCls =
  'w-full rounded-lg border border-zinc-800 bg-zinc-950/70 px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-600 ' +
  'focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500/60'

export function Input(props: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={cn(inputCls, props.className)} />
}

export function Select(props: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      {...props}
      className={cn(inputCls, 'appearance-none bg-zinc-900', props.className)}
    />
  )
}

// ---------------------------------------------------------------------------
// Modal
// ---------------------------------------------------------------------------

export function Modal({
  open,
  onClose,
  title,
  children,
  footer,
  wide,
}: {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
  footer?: ReactNode
  wide?: boolean
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <div
        className={cn(
          'w-full rounded-xl border border-zinc-800 bg-zinc-900 shadow-2xl shadow-black/50',
          wide ? 'max-w-2xl' : 'max-w-md',
        )}
      >
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h3 className="text-sm font-semibold">{title}</h3>
          <button
            onClick={onClose}
            className="rounded-md p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
            aria-label="Close"
          >
            <X size={16} />
          </button>
        </div>
        <div className="px-5 py-4 text-sm">{children}</div>
        {footer && (
          <div className="flex justify-end gap-2 border-t border-zinc-800 px-5 py-3">{footer}</div>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Misc
// ---------------------------------------------------------------------------

export function Spinner({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        'inline-block size-4 animate-spin rounded-full border-2 border-zinc-600 border-t-zinc-200',
        className,
      )}
    />
  )
}

export function EmptyState({
  title,
  hint,
  action,
}: {
  title: string
  hint?: string
  action?: ReactNode
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-16 text-center">
      <p className="text-sm font-medium text-zinc-300">{title}</p>
      {hint && <p className="max-w-sm text-xs text-zinc-500">{hint}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}

export function ErrorBox({ message }: { message: string }) {
  if (!message) return null
  return (
    <div className="rounded-lg border border-red-900/60 bg-red-950/40 px-3 py-2 text-xs text-red-300">
      {message}
    </div>
  )
}
