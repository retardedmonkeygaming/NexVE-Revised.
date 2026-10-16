// Login page. Credentials are verified against the API before being stored,
// so a typo fails fast instead of breaking every later request.
import { useState, type FormEvent } from 'react'
import { api, ApiError, type Auth } from '../lib/api'
import { useApp } from '../App'
import { Button, Card, ErrorBox, Field, Input } from '../components/ui'

export default function Login() {
  const { login } = useApp()
  const [mode, setMode] = useState<'basic' | 'token'>('basic')
  const [user, setUser] = useState('admin')
  const [password, setPassword] = useState('')
  const [token, setToken] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')

    const candidate: Auth =
      mode === 'basic'
        ? { kind: 'basic', value: btoa(`${user}:${password}`), label: user }
        : { kind: 'bearer', value: token.trim(), label: `token…${token.trim().slice(-4)}` }

    // Verify against an authenticated endpoint.
    sessionStorage.setItem('nexve.auth', JSON.stringify(candidate))
    try {
      await api.nodeStatus()
      login(candidate)
    } catch (err) {
      sessionStorage.removeItem('nexve.auth')
      setError(err instanceof ApiError ? err.message : 'login failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid min-h-full place-items-center bg-[radial-gradient(60%_50%_at_50%_0%,rgba(79,70,229,0.15),transparent)] p-4">
      <Card className="w-full max-w-sm">
        <div className="border-b border-zinc-800/80 px-6 py-5">
          <div className="mb-3 flex items-center gap-2.5">
            <div className="grid size-9 place-items-center rounded-xl bg-gradient-to-br from-indigo-500 to-indigo-700 text-sm font-bold text-white">
              Nx
            </div>
            <div>
              <h1 className="text-base font-semibold tracking-tight">Sign in to NexVE</h1>
              <p className="text-xs text-zinc-500">open-source Type-1 hypervisor</p>
            </div>
          </div>
        </div>

        <form onSubmit={submit} className="space-y-4 px-6 py-5">
          <div className="flex gap-1 rounded-lg bg-zinc-950/70 p-1 text-xs">
            {(['basic', 'token'] as const).map((m) => (
              <button
                key={m}
                type="button"
                onClick={() => setMode(m)}
                className={
                  'flex-1 rounded-md px-3 py-1.5 font-medium transition-colors ' +
                  (mode === m ? 'bg-zinc-800 text-zinc-100' : 'text-zinc-500 hover:text-zinc-300')
                }
              >
                {m === 'basic' ? 'User & password' : 'API token'}
              </button>
            ))}
          </div>

          {mode === 'basic' ? (
            <>
              <Field label="Username">
                <Input value={user} onChange={(e) => setUser(e.target.value)} autoFocus />
              </Field>
              <Field label="Password">
                <Input
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoFocus
                />
              </Field>
            </>
          ) : (
            <Field label="Bearer token" hint="NEXVE_AUTH_TOKEN from /etc/nexve/nexve.env">
              <Input
                type="password"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="eyJhbGciOi..."
                autoFocus
              />
            </Field>
          )}

          <ErrorBox message={error} />

          <Button type="submit" variant="primary" className="w-full" disabled={busy}>
            {busy ? 'Signing in…' : 'Sign in'}
          </Button>

          <p className="text-center text-[11px] text-zinc-600">
            Credentials are kept in this tab only (sessionStorage).
          </p>
        </form>
      </Card>
    </div>
  )
}
