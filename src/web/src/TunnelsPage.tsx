import { useState, useEffect, useCallback } from 'react'
import './TunnelsPage.css'

// --- Types ---

type TunnelConnectionState = 'disconnected' | 'connecting' | 'connected' | 'error'

interface TunnelDetails {
  localIp: string
  serverIp: string
  serverEndpoint: string
  connectedAt: string
}

interface TunnelStatus {
  status: TunnelConnectionState
  details?: TunnelDetails
  error?: string
}

// --- Helpers ---

function formatTimestamp(iso: string): string {
  try {
    const d = new Date(iso)
    return d.toLocaleString()
  } catch {
    return iso
  }
}

// --- Component ---

function TunnelsPage() {
  const [tunnelStatus, setTunnelStatus] = useState<TunnelStatus | null>(null)
  const [fetchError, setFetchError] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState<'connect' | 'disconnect' | null>(null)
  const [actionMessage, setActionMessage] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const fetchStatus = useCallback(async () => {
    setFetchError(null)
    try {
      const res = await fetch('/api/tunnel/status')
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`)
      }
      const data: TunnelStatus = await res.json()
      setTunnelStatus(data)
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to fetch tunnel status'
      setFetchError(msg)
    }
  }, [])

  // Fetch on mount
  useEffect(() => {
    fetchStatus()
  }, [fetchStatus])

  // Auto-refresh every 5 seconds when connected
  useEffect(() => {
    if (tunnelStatus?.status !== 'connected') return

    const interval = setInterval(() => {
      fetchStatus()
    }, 5000)

    return () => clearInterval(interval)
  }, [tunnelStatus?.status, fetchStatus])

  // --- Actions ---

  async function handleConnect() {
    setActionLoading('connect')
    setActionMessage(null)
    setActionError(null)

    try {
      const res = await fetch('/api/tunnel/connect', { method: 'POST' })
      if (!res.ok) {
        let detail = `HTTP ${res.status}`
        try {
          const body = await res.json()
          if (body.error || body.message) {
            detail = body.error || body.message
          }
        } catch {
          // ignore JSON parse error
        }
        throw new Error(detail)
      }
      setActionMessage('Tunnel connected successfully.')
      // Refresh status immediately
      await fetchStatus()
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Network error'
      setActionError(msg)
    } finally {
      setActionLoading(null)
    }
  }

  async function handleDisconnect() {
    setActionLoading('disconnect')
    setActionMessage(null)
    setActionError(null)

    try {
      const res = await fetch('/api/tunnel/disconnect', { method: 'POST' })
      if (!res.ok) {
        let detail = `HTTP ${res.status}`
        try {
          const body = await res.json()
          if (body.error || body.message) {
            detail = body.error || body.message
          }
        } catch {
          // ignore JSON parse error
        }
        throw new Error(detail)
      }
      setActionMessage('Tunnel disconnected.')
      await fetchStatus()
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Network error'
      setActionError(msg)
    } finally {
      setActionLoading(null)
    }
  }

  // --- Render helpers ---

  const statusColor = (() => {
    switch (tunnelStatus?.status) {
      case 'connected':
        return 'var(--status-ok)'
      case 'connecting':
        return 'var(--status-warn)'
      case 'error':
        return 'var(--status-error)'
      default:
        return 'var(--text-secondary)'
    }
  })()

  const statusLabel = tunnelStatus?.status
    ? tunnelStatus.status.charAt(0).toUpperCase() + tunnelStatus.status.slice(1)
    : 'Unknown'

  return (
    <div className="tunnels-section">
      {/* Status panel */}
      <div className="tunnel-panel">
        <h3>Tunnel Status</h3>

        {fetchError && (
          <p className="tunnel-error-text">{fetchError}</p>
        )}

        {!tunnelStatus && !fetchError && (
          <p className="tunnel-muted">Loading...</p>
        )}

        {tunnelStatus && (
          <div className="tunnel-status-details">
            <div className="tunnel-status-row">
              <span className="tunnel-status-label">Status</span>
              <span className="tunnel-status-value" style={{ color: statusColor }}>
                {statusLabel}
              </span>
            </div>

            {tunnelStatus.error && (
              <div className="tunnel-status-row">
                <span className="tunnel-status-label">Error</span>
                <span className="tunnel-status-value tunnel-err">{tunnelStatus.error}</span>
              </div>
            )}

            {tunnelStatus.details && (
              <>
                <div className="tunnel-status-row">
                  <span className="tunnel-status-label">Local IP</span>
                  <span className="tunnel-status-value">{tunnelStatus.details.localIp}</span>
                </div>
                <div className="tunnel-status-row">
                  <span className="tunnel-status-label">Server IP</span>
                  <span className="tunnel-status-value">{tunnelStatus.details.serverIp}</span>
                </div>
                <div className="tunnel-status-row">
                  <span className="tunnel-status-label">Server Endpoint</span>
                  <span className="tunnel-status-value">{tunnelStatus.details.serverEndpoint}</span>
                </div>
                <div className="tunnel-status-row">
                  <span className="tunnel-status-label">Connected At</span>
                  <span className="tunnel-status-value">
                    {formatTimestamp(tunnelStatus.details.connectedAt)}
                  </span>
                </div>
              </>
            )}
          </div>
        )}
      </div>

      {/* Actions panel */}
      <div className="tunnel-panel">
        <h3>Actions</h3>

        <div className="tunnel-actions">
          <button
            className="tunnel-btn tunnel-btn-connect"
            onClick={handleConnect}
            disabled={
              actionLoading !== null ||
              tunnelStatus?.status === 'connected' ||
              tunnelStatus?.status === 'connecting'
            }
          >
            {actionLoading === 'connect' ? 'Connecting...' : 'Connect'}
          </button>

          <button
            className="tunnel-btn tunnel-btn-disconnect"
            onClick={handleDisconnect}
            disabled={
              actionLoading !== null ||
              tunnelStatus?.status !== 'connected'
            }
          >
            {actionLoading === 'disconnect' ? 'Disconnecting...' : 'Disconnect'}
          </button>
        </div>

        {actionMessage && <p className="tunnel-action-ok">{actionMessage}</p>}
        {actionError && <p className="tunnel-action-err">{actionError}</p>}
      </div>
    </div>
  )
}

export default TunnelsPage
