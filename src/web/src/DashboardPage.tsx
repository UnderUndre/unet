import { useState, useEffect, useCallback } from 'react'
import './DashboardPage.css'

// --- Types ---

interface VpsStatus {
  configured: boolean
  provisioned: boolean
  host: string
}

interface TunnelStatus {
  status: string
  localIp?: string
  serverIp?: string
}

interface PortInfo {
  id: string
  localPort: number
  subdomain: string
  protocol: string
  status: string
  createdAt?: string
}

interface ApiStatus {
  privileged: boolean
  vps: VpsStatus
  tunnel: TunnelStatus
  ports: PortInfo[]
  daemonPort: number
}

// --- Helpers ---

function statusToSeverity(status: string): 'ok' | 'warn' | 'error' | 'none' {
  const s = status.toLowerCase()
  if (s === 'connected' || s === 'running' || s === 'active' || s === 'ok') return 'ok'
  if (s === 'connecting' || s === 'provisioning' || s === 'pending') return 'warn'
  if (s === 'error' || s === 'stopped' || s === 'disconnected' || s === 'failed') return 'error'
  return 'none'
}

function statusColor(severity: 'ok' | 'warn' | 'error' | 'none'): string {
  switch (severity) {
    case 'ok': return 'var(--status-ok)'
    case 'warn': return 'var(--status-warn)'
    case 'error': return 'var(--status-error)'
    default: return 'var(--text-secondary)'
  }
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}

// --- Component ---

function DashboardPage() {
  const [status, setStatus] = useState<ApiStatus | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const fetchStatus = useCallback(async () => {
    setError(null)
    try {
      const res = await fetch('/api/status')
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`)
      }
      const data: ApiStatus = await res.json()
      setStatus(data)
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to fetch status'
      setError(msg)
    } finally {
      setLoading(false)
    }
  }, [])

  // Fetch on mount
  useEffect(() => {
    fetchStatus()
  }, [fetchStatus])

  // Poll every 5 seconds
  useEffect(() => {
    const interval = setInterval(() => {
      fetchStatus()
    }, 5000)
    return () => clearInterval(interval)
  }, [fetchStatus])

  // --- Cards derived from status ---

  const vpsLabel = status?.vps.provisioned
    ? 'Provisioned'
    : status?.vps.configured
      ? 'Configured'
      : 'Not configured'

  const cards: Array<{ label: string; value: string; severity: ReturnType<typeof statusToSeverity> }> = status
    ? [
        {
          label: 'VPS Status',
          value: vpsLabel,
          severity: status.vps.provisioned ? 'ok' : status.vps.configured ? 'warn' : 'none',
        },
        {
          label: 'Tunnel Status',
          value: capitalize(status.tunnel.status || 'unknown'),
          severity: statusToSeverity(status.tunnel.status || 'unknown'),
        },
        {
          label: 'Exposed Ports',
          value: String(status.ports.length),
          severity: status.ports.length > 0 ? 'ok' : 'none',
        },
        {
          label: 'Daemon Port',
          value: String(status.daemonPort),
          severity: 'none',
        },
      ]
    : []

  return (
    <div className="dashboard-section">
      {/* Status grid */}
      <section className="status-grid">
        {loading && !status && (
          <div className="status-card">
            <span className="status-label">Loading...</span>
            <span className="status-value" style={{ color: 'var(--text-secondary)' }}>
              ...
            </span>
          </div>
        )}

        {error && !status && (
          <div className="status-card">
            <span className="status-label">Error</span>
            <span className="status-value" style={{ color: 'var(--status-error)' }}>
              {error}
            </span>
          </div>
        )}

        {cards.map((card) => (
          <div className="status-card" key={card.label}>
            <span className="status-label">{card.label}</span>
            <span className="status-value" style={{ color: statusColor(card.severity) }}>
              {card.value}
            </span>
          </div>
        ))}
      </section>

      {/* Connection log placeholder */}
      <section className="dashboard-panel">
        <h3>Connection Log</h3>
        <div className="dashboard-log-placeholder">
          <p className="dashboard-muted">
            {status
              ? 'Live status updates every 5 seconds.'
              : 'Waiting for status data...'}
          </p>
          {error && (
            <p className="dashboard-error-text">
              Last poll failed: {error}
            </p>
          )}
        </div>
      </section>
    </div>
  )
}

export default DashboardPage
