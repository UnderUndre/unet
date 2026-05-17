import { useState, useEffect, useCallback } from 'react'
import './PortsPage.css'

// --- Types ---

type TunnelConnectionState = 'disconnected' | 'connecting' | 'connected' | 'error'

interface TunnelStatus {
  status: TunnelConnectionState
  error?: string
}

interface Port {
  id: string
  localPort: number
  subdomain: string
  status: string
  createdAt: string
}

interface AddPortForm {
  localPort: string
  subdomain: string
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

function PortsPage() {
  const [ports, setPorts] = useState<Port[]>([])
  const [portsLoading, setPortsLoading] = useState<boolean>(true)
  const [portsError, setPortsError] = useState<string | null>(null)

  const [tunnelStatus, setTunnelStatus] = useState<TunnelStatus | null>(null)

  const [form, setForm] = useState<AddPortForm>({ localPort: '', subdomain: '' })
  const [formLoading, setFormLoading] = useState<boolean>(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [formSuccess, setFormSuccess] = useState<string | null>(null)

  const [deleteLoading, setDeleteLoading] = useState<string | null>(null) // port id being deleted
  const [deleteError, setDeleteError] = useState<string | null>(null)

  // --- Fetch ports ---

  const fetchPorts = useCallback(async () => {
    setPortsError(null)
    try {
      const res = await fetch('/api/ports')
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`)
      }
      const data: Port[] = await res.json()
      setPorts(data)
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to fetch ports'
      setPortsError(msg)
    } finally {
      setPortsLoading(false)
    }
  }, [])

  // --- Fetch tunnel status ---

  const fetchTunnelStatus = useCallback(async () => {
    try {
      const res = await fetch('/api/tunnel/status')
      if (!res.ok) {
        return
      }
      const data: TunnelStatus = await res.json()
      setTunnelStatus(data)
    } catch {
      // Silently ignore — tunnel status is supplementary here
    }
  }, [])

  // Initial fetch
  useEffect(() => {
    fetchPorts()
    fetchTunnelStatus()
  }, [fetchPorts, fetchTunnelStatus])

  // --- Add port ---

  async function handleAddPort(e: React.FormEvent) {
    e.preventDefault()
    setFormError(null)
    setFormSuccess(null)

    const localPort = Number(form.localPort)
    if (!Number.isInteger(localPort) || localPort < 1 || localPort > 65535) {
      setFormError('Port must be an integer between 1 and 65535.')
      return
    }

    const subdomain = form.subdomain.trim()
    if (!subdomain) {
      setFormError('Subdomain is required.')
      return
    }

    setFormLoading(true)
    try {
      const res = await fetch('/api/ports', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ localPort, subdomain }),
      })
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
      setFormSuccess('Port added successfully.')
      setForm({ localPort: '', subdomain: '' })
      await fetchPorts()
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Network error'
      setFormError(msg)
    } finally {
      setFormLoading(false)
    }
  }

  // --- Delete port ---

  async function handleDelete(id: string) {
    if (!window.confirm('Are you sure you want to delete this port?')) {
      return
    }

    setDeleteError(null)
    setDeleteLoading(id)
    try {
      const res = await fetch(`/api/ports/${id}`, { method: 'DELETE' })
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
      await fetchPorts()
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Network error'
      setDeleteError(msg)
    } finally {
      setDeleteLoading(null)
    }
  }

  // --- Tunnel warning ---

  const tunnelConnected = tunnelStatus?.status === 'connected'

  // --- Render ---

  return (
    <div className="ports-section">
      {/* Tunnel status warning */}
      {!tunnelConnected && (
        <div className="ports-tunnel-warning">
          <span className="ports-warning-icon">&#9888;</span>
          <span>
            {tunnelStatus?.status === 'connecting'
              ? 'Tunnel is connecting... Ports may not be reachable yet.'
              : tunnelStatus?.status === 'error'
                ? `Tunnel error: ${tunnelStatus.error ?? 'unknown'}. Ports will not be reachable.`
                : 'Tunnel is not connected. Exposed ports will not be reachable from the outside.'}
          </span>
        </div>
      )}

      {/* Add port form */}
      <div className="ports-panel">
        <h3>Expose a Port</h3>
        <form className="ports-form" onSubmit={handleAddPort}>
          <div className="ports-form-row">
            <label className="ports-form-label">
              Local Port
              <input
                type="number"
                min={1}
                max={65535}
                className="ports-form-input"
                placeholder="8080"
                value={form.localPort}
                onChange={(e) => setForm((f) => ({ ...f, localPort: e.target.value }))}
                disabled={formLoading}
              />
            </label>
            <label className="ports-form-label">
              Subdomain
              <input
                type="text"
                className="ports-form-input"
                placeholder="myapp"
                value={form.subdomain}
                onChange={(e) => setForm((f) => ({ ...f, subdomain: e.target.value }))}
                disabled={formLoading}
              />
            </label>
            <button
              type="submit"
              className="ports-btn ports-btn-add"
              disabled={formLoading}
            >
              {formLoading ? 'Adding...' : 'Add Port'}
            </button>
          </div>
          {formError && <p className="ports-form-error">{formError}</p>}
          {formSuccess && <p className="ports-form-success">{formSuccess}</p>}
        </form>
      </div>

      {/* Port list */}
      <div className="ports-panel">
        <h3>Exposed Ports</h3>

        {portsLoading && <p className="ports-muted">Loading...</p>}
        {portsError && <p className="ports-error-text">{portsError}</p>}
        {deleteError && <p className="ports-error-text">{deleteError}</p>}

        {!portsLoading && !portsError && ports.length === 0 && (
          <p className="ports-muted">No exposed ports yet. Use the form above to add one.</p>
        )}

        {ports.length > 0 && (
          <table className="ports-table">
            <thead>
              <tr>
                <th>Local Port</th>
                <th>Subdomain</th>
                <th>Status</th>
                <th>Created</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {ports.map((port) => (
                <tr key={port.id}>
                  <td className="ports-table-mono">{port.localPort}</td>
                  <td>{port.subdomain}</td>
                  <td>
                    <span
                      className={`ports-status-badge ${
                        port.status === 'active' ? 'ports-status-active' : 'ports-status-inactive'
                      }`}
                    >
                      {port.status}
                    </span>
                  </td>
                  <td className="ports-table-muted">{formatTimestamp(port.createdAt)}</td>
                  <td>
                    <button
                      className="ports-btn ports-btn-delete"
                      onClick={() => handleDelete(port.id)}
                      disabled={deleteLoading !== null}
                    >
                      {deleteLoading === port.id ? 'Deleting...' : 'Delete'}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

export default PortsPage
