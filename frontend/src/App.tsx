// Root component: authentication gate + sidebar shell.
// Four pages, no router – tab state lives here.
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { LayoutGrid, Server, LogOut } from 'lucide-react'
import { api, loadAuth, saveAuth, type Auth } from './lib/api'
import { Spinner } from './components/ui'
import { ToastProvider } from './components/toast'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Instances from './pages/Instances'

type Tab = 'dashboard' | 'instances'

interface AppState {
  auth: Auth | null
  login: (a: Auth) => void
  logout: () => void
  version: string
  driver: string
  go: (t: Tab) => void // navigate (used after creating a VM)
}

const AppCtx = createContext<AppState>({
  auth: null,
  login: () => {},
  logout: () => {},
  version: '',
  driver: '',
  go: () => {},
})

export function useApp(): AppState {
  return useContext(AppCtx)
}

export default function App() {
  const [auth, setAuth] = useState<Auth | null>(() => loadAuth())
  const [health, setHealth] = useState<{ version: string; driver: string } | null>(null)
  const [tab, setTab] = useState<Tab>('dashboard')

  // Health is unauthenticated – it shows version/driver in the sidebar.
  useEffect(() => {
    api
      .health()
      .then((h) => setHealth({ version: h.version, driver: h.driver }))
      .catch(() => setHealth({ version: 'unreachable', driver: '—' }))
  }, [])

  const login = useCallback((a: Auth) => {
    saveAuth(a)
    setAuth(a)
    setTab('dashboard')
  }, [])

  const logout = useCallback(() => {
    saveAuth(null)
    setAuth(null)
  }, [])

  const value = useMemo<AppState>(
    () => ({ auth, login, logout, version: health?.version ?? '', driver: health?.driver ?? '', go: setTab }),
    [auth, login, logout, health],
  )

  return (
    <AppCtx.Provider value={value}>
      <ToastProvider>
        {!auth ? (
          <Login />
        ) : !health ? (
          <Centered>
            <Spinner className="size-6 text-zinc-500" />
          </Centered>
        ) : (
          <div className="flex min-h-full">
            <Sidebar tab={tab} setTab={setTab} />
            <main className="min-w-0 flex-1">
              {tab === 'dashboard' ? <Dashboard /> : <Instances />}
            </main>
          </div>
        )}
      </ToastProvider>
    </AppCtx.Provider>
  )
}

function Centered({ children }: { children: ReactNode }) {
  return <div className="grid h-full place-items-center">{children}</div>
}

function Sidebar({ tab, setTab }: { tab: Tab; setTab: (t: Tab) => void }) {
  const { logout, version, driver, auth } = useApp()
  return (
    <aside className="sticky top-0 flex h-screen w-56 shrink-0 flex-col border-r border-zinc-800/80 px-3 py-4">
      <div className="mb-6 flex items-center gap-2.5 px-2">
        <div className="grid size-9 place-items-center rounded-xl bg-gradient-to-br from-indigo-500 to-indigo-700 text-sm font-bold text-white shadow-lg shadow-indigo-900/40">
          Nx
        </div>
        <div>
          <div className="text-sm font-semibold leading-tight tracking-tight">NexVE</div>
          <div className="text-[10px] text-zinc-500">hypervisor · {version}</div>
        </div>
      </div>

      <nav className="space-y-1">
        <NavItem
          active={tab === 'dashboard'}
          onClick={() => setTab('dashboard')}
          icon={<LayoutGrid size={16} />}
          label="Dashboard"
        />
        <NavItem
          active={tab === 'instances'}
          onClick={() => setTab('instances')}
          icon={<Server size={16} />}
          label="VMs & Containers"
        />
      </nav>

      <div className="mt-auto space-y-2 px-1">
        <div className="space-y-1 rounded-lg border border-zinc-800/80 bg-zinc-900/50 p-2.5 text-[11px] text-zinc-500">
          <div className="flex justify-between">
            <span>driver</span>
            <span className="font-medium text-zinc-300">{driver}</span>
          </div>
          <div className="flex justify-between">
            <span>signed in</span>
            <span className="font-medium text-zinc-300">{auth?.label ?? '—'}</span>
          </div>
        </div>
        <button
          onClick={logout}
          className="flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-xs text-zinc-500 transition-colors hover:bg-zinc-900 hover:text-zinc-200"
        >
          <LogOut size={14} /> Sign out
        </button>
      </div>
    </aside>
  )
}

function NavItem({
  active,
  onClick,
  icon,
  label,
}: {
  active: boolean
  onClick: () => void
  icon: ReactNode
  label: string
}) {
  return (
    <button
      onClick={onClick}
      className={
        'flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm transition-colors ' +
        (active
          ? 'bg-zinc-900 text-zinc-50 ring-1 ring-inset ring-zinc-800'
          : 'text-zinc-400 hover:bg-zinc-900/60 hover:text-zinc-200')
      }
    >
      {icon}
      {label}
    </button>
  )
}
