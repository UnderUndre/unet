import { useState, useEffect, useCallback } from 'react'
import './App.css'
import DashboardPage from './DashboardPage'
import VpsConfigForm from './VpsConfigForm'
import TunnelsPage from './TunnelsPage'
import PortsPage from './PortsPage'
import SettingsPage from './SettingsPage'
import PrivilegeOverlay from './PrivilegeOverlay'

// --- Types ---

interface ApiStatus {
  privileged: boolean
  vps: string
  tunnel: string
  ports: number
  daemonPort: number
}

type Page = 'dashboard' | 'tunnels' | 'vps' | 'ports' | 'settings'

// --- App ---

function App() {
  const [page, setPage] = useState<Page>('dashboard')
  const [_privileged, setPrivileged] = useState<boolean | null>(null)
  const [showOverlay, setShowOverlay] = useState(false)

  // Check privilege status on mount
  const checkPrivilege = useCallback(async () => {
    try {
      const res = await fetch('/api/status')
      if (!res.ok) return
      const data: ApiStatus = await res.json()
      if (data.privileged === false) {
        setPrivileged(false)
        setShowOverlay(true)
      } else {
        setPrivileged(true)
      }
    } catch {
      // On network error, don't show overlay — the dashboard page will show its own error
    }
  }, [])

  useEffect(() => {
    checkPrivilege()
  }, [checkPrivilege])

  return (
    <div className="app">
      {/* Privilege warning overlay */}
      {showOverlay && (
        <PrivilegeOverlay onDismiss={() => setShowOverlay(false)} />
      )}

      <header className="app-header">
        <h1 className="app-title">Unet</h1>
        <nav className="app-nav">
          {(['dashboard', 'tunnels', 'vps', 'ports', 'settings'] as const).map((p) => (
            <span
              key={p}
              className={`nav-item${page === p ? ' active' : ''}`}
              onClick={() => setPage(p)}
            >
              {p.charAt(0).toUpperCase() + p.slice(1)}
            </span>
          ))}
        </nav>
      </header>

      <main className="app-main">
        {page === 'dashboard' && <DashboardPage />}
        {page === 'tunnels' && <TunnelsPage />}
        {page === 'vps' && <VpsConfigForm />}
        {page === 'ports' && <PortsPage />}
        {page === 'settings' && <SettingsPage />}
      </main>

      <footer className="app-footer">
        <span>Unet v0.1.0 &mdash; localhost:8080</span>
      </footer>
    </div>
  )
}

export default App
