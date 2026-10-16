// Lightweight toast notifications – a context so any page can raise one.
import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from 'react'
import { CheckCircle2, AlertTriangle, Info, X } from 'lucide-react'
import { cn } from './ui'

type ToastKind = 'success' | 'error' | 'info'
interface Toast {
  id: number
  kind: ToastKind
  msg: string
}

const ToastCtx = createContext<(kind: ToastKind, msg: string) => void>(() => {})

export function useToast() {
  return useContext(ToastCtx)
}

const icons: Record<ToastKind, ReactNode> = {
  success: <CheckCircle2 size={16} className="text-emerald-400" />,
  error: <AlertTriangle size={16} className="text-red-400" />,
  info: <Info size={16} className="text-indigo-400" />,
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const nextId = useRef(1)

  const push = useCallback((kind: ToastKind, msg: string) => {
    const id = nextId.current++
    setToasts((t) => [...t, { id, kind, msg }])
    window.setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4500)
  }, [])

  return (
    <ToastCtx.Provider value={push}>
      {children}
      <div className="pointer-events-none fixed bottom-4 right-4 z-[60] flex w-80 flex-col gap-2">
        {toasts.map((t) => (
          <div
            key={t.id}
            className={cn(
              'pointer-events-auto flex items-start gap-2 rounded-lg border bg-zinc-900 px-3 py-2.5 text-xs shadow-xl',
              t.kind === 'error'
                ? 'border-red-900/70'
                : t.kind === 'success'
                  ? 'border-emerald-900/70'
                  : 'border-zinc-800',
            )}
          >
            {icons[t.kind]}
            <span className="flex-1 text-zinc-200">{t.msg}</span>
            <button
              onClick={() => setToasts((all) => all.filter((x) => x.id !== t.id))}
              className="text-zinc-600 hover:text-zinc-300"
            >
              <X size={14} />
            </button>
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  )
}
