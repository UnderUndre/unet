import { useState, useEffect, useRef, type FormEvent, type ChangeEvent } from 'react'
import './VpsConfigForm.css'

interface SshHostEntry {
  host: string
  hostName?: string
  user?: string
  port?: number
  identityFile?: string
}

// --- Types ---

interface VpsFormData {
  host: string
  sshPort: number
  username: string
  authMode: 'key' | 'password'
  privateKeyPath: string
  password: string
}

interface VpsStatus {
  status: string
  host?: string
  message?: string
  provisionedAt?: string
  error?: string
}

interface ApiStatus {
  status: string
  uptime?: number
  version?: string
  tunnels?: number
  connectedVps?: number
  exposedPorts?: number
}

interface FormErrors {
  host?: string
  sshPort?: string
  privateKeyPath?: string
  password?: string
}

// --- Validation helpers ---

const SHELL_METACHAR_REGEX = /[;&|`$(){}[\]<>!#~\\'" \t\n\r]/

function validateForm(data: VpsFormData): FormErrors {
  const errors: FormErrors = {}

  // Host is required, no shell metacharacters
  if (!data.host.trim()) {
    errors.host = 'Host is required'
  } else if (SHELL_METACHAR_REGEX.test(data.host)) {
    errors.host = 'Host contains invalid characters (shell metacharacters rejected)'
  }

  // Port range 1-65535
  if (data.sshPort < 1 || data.sshPort > 65535) {
    errors.sshPort = 'Port must be between 1 and 65535'
  }

  // Key mode requires a path
  if (data.authMode === 'key' && !data.privateKeyPath.trim()) {
    errors.privateKeyPath = 'Private key path is required when using key authentication'
  }

  // Password mode requires a password
  if (data.authMode === 'password' && !data.password.trim()) {
    errors.password = 'Password is required when using password authentication'
  }

  return errors
}

// --- Component ---

function VpsConfigForm() {
  const [formData, setFormData] = useState<VpsFormData>({
    host: '',
    sshPort: 22,
    username: 'root',
    authMode: 'key',
    privateKeyPath: '',
    password: '',
  })

  const [errors, setErrors] = useState<FormErrors>({})
  const [submitting, setSubmitting] = useState(false)
  const [submitMessage, setSubmitMessage] = useState<string | null>(null)
  const [submitError, setSubmitError] = useState<string | null>(null)

  const [vpsStatus, setVpsStatus] = useState<VpsStatus | null>(null)
  const [apiStatus, setApiStatus] = useState<ApiStatus | null>(null)
  const [vpsStatusError, setVpsStatusError] = useState<string | null>(null)
  const [apiStatusError, setApiStatusError] = useState<string | null>(null)

  const [sshHosts, setSshHosts] = useState<SshHostEntry[]>([])
  const [showSuggestions, setShowSuggestions] = useState(false)
  const [activeSuggestion, setActiveSuggestion] = useState(-1)
  const hostRef = useRef<HTMLInputElement>(null)
  const suggestionsRef = useRef<HTMLDivElement>(null)

  // Fetch statuses on mount
  useEffect(() => {
    fetchVpsStatus()
    fetchApiStatus()
    fetchSSHHosts()
  }, [])

  // --- API helpers ---

  async function fetchVpsStatus() {
    setVpsStatusError(null)
    try {
      const res = await fetch('/api/vps/status')
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`)
      }
      const data: VpsStatus = await res.json()
      setVpsStatus(data)
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to fetch VPS status'
      setVpsStatusError(msg)
    }
  }

  async function fetchApiStatus() {
    setApiStatusError(null)
    try {
      const res = await fetch('/api/status')
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`)
      }
      const data: ApiStatus = await res.json()
      setApiStatus(data)
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to fetch API status'
      setApiStatusError(msg)
    }
  }

  async function fetchSSHHosts() {
    try {
      const res = await fetch('/api/ssh/hosts')
      if (!res.ok) return
      const data: SshHostEntry[] = await res.json()
      setSshHosts(data)
    } catch {
      // silently ignore
    }
  }

  const filteredHosts = sshHosts.filter(h =>
    h.host.toLowerCase().includes(formData.host.toLowerCase()) ||
    (h.hostName && h.hostName.toLowerCase().includes(formData.host.toLowerCase()))
  )

  function selectHost(entry: SshHostEntry) {
    setFormData(prev => ({
      ...prev,
      host: entry.host,
      hostName: entry.hostName || entry.host,
      sshPort: entry.port || 22,
      username: entry.user || prev.username,
      privateKeyPath: entry.identityFile || prev.privateKeyPath,
    }))
    setShowSuggestions(false)
    setActiveSuggestion(-1)
  }

  function handleHostKeyDown(e: React.KeyboardEvent) {
    if (!showSuggestions || filteredHosts.length === 0) return

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActiveSuggestion(prev => (prev + 1) % filteredHosts.length)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActiveSuggestion(prev => (prev - 1 + filteredHosts.length) % filteredHosts.length)
    } else if (e.key === 'Enter' && activeSuggestion >= 0) {
      e.preventDefault()
      selectHost(filteredHosts[activeSuggestion])
    } else if (e.key === 'Escape') {
      setShowSuggestions(false)
      setActiveSuggestion(-1)
    }
  }

  // --- Form handlers ---

  function handleChange(
    e: ChangeEvent<HTMLInputElement | HTMLSelectElement>
  ) {
    const { name, value } = e.target
    setFormData((prev) => ({
      ...prev,
      [name]: name === 'sshPort' ? Number(value) : value,
    }))
    // Clear field error on change
    if (errors[name as keyof FormErrors]) {
      setErrors((prev) => ({ ...prev, [name]: undefined }))
    }
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setSubmitMessage(null)
    setSubmitError(null)

    const validationErrors = validateForm(formData)
    if (Object.keys(validationErrors).length > 0) {
      setErrors(validationErrors)
      return
    }
    setErrors({})

    setSubmitting(true)
    try {
      const res = await fetch('/api/vps/configure', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(formData),
      })

      if (res.status === 202) {
        setSubmitMessage('Provisioning started successfully.')
        // Refresh status after a short delay
        setTimeout(() => {
          fetchVpsStatus()
          fetchApiStatus()
        }, 2000)
      } else {
        let detail = `HTTP ${res.status}`
        try {
          const body = await res.json()
          if (body.error || body.message) {
            detail = body.error || body.message
          }
        } catch {
          // ignore JSON parse error
        }
        setSubmitError(detail)
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Network error'
      setSubmitError(msg)
    } finally {
      setSubmitting(false)
    }
  }

  // --- Render ---

  return (
    <div className="vps-section">
      {/* Status panels */}
      <div className="vps-status-panels">
        <div className="vps-panel">
          <h3>VPS Status</h3>
          {vpsStatusError && (
            <p className="vps-error-text">{vpsStatusError}</p>
          )}
          {vpsStatus && !vpsStatusError && (
            <div className="vps-status-details">
              <div className="vps-status-row">
                <span className="vps-status-label">State</span>
                <span
                  className={`vps-status-value ${
                    vpsStatus.status === 'connected' || vpsStatus.status === 'provisioned'
                      ? 'vps-ok'
                      : vpsStatus.status === 'provisioning'
                        ? 'vps-warn'
                        : vpsStatus.status === 'error'
                          ? 'vps-err'
                          : ''
                  }`}
                >
                  {vpsStatus.status}
                </span>
              </div>
              {vpsStatus.host && (
                <div className="vps-status-row">
                  <span className="vps-status-label">Host</span>
                  <span className="vps-status-value">{vpsStatus.host}</span>
                </div>
              )}
              {vpsStatus.message && (
                <div className="vps-status-row">
                  <span className="vps-status-label">Message</span>
                  <span className="vps-status-value">{vpsStatus.message}</span>
                </div>
              )}
              {vpsStatus.provisionedAt && (
                <div className="vps-status-row">
                  <span className="vps-status-label">Provisioned</span>
                  <span className="vps-status-value">{vpsStatus.provisionedAt}</span>
                </div>
              )}
              {vpsStatus.error && (
                <div className="vps-status-row">
                  <span className="vps-status-label">Error</span>
                  <span className="vps-status-value vps-err">{vpsStatus.error}</span>
                </div>
              )}
            </div>
          )}
          {!vpsStatus && !vpsStatusError && (
            <p className="vps-muted">Loading...</p>
          )}
        </div>

        <div className="vps-panel">
          <h3>System Status</h3>
          {apiStatusError && (
            <p className="vps-error-text">{apiStatusError}</p>
          )}
          {apiStatus && !apiStatusError && (
            <div className="vps-status-details">
              <div className="vps-status-row">
                <span className="vps-status-label">API</span>
                <span className={`vps-status-value ${apiStatus.status === 'ok' ? 'vps-ok' : 'vps-err'}`}>
                  {apiStatus.status}
                </span>
              </div>
              {apiStatus.uptime != null && (
                <div className="vps-status-row">
                  <span className="vps-status-label">Uptime</span>
                  <span className="vps-status-value">{Math.floor(apiStatus.uptime / 60)}m</span>
                </div>
              )}
              {apiStatus.version && (
                <div className="vps-status-row">
                  <span className="vps-status-label">Version</span>
                  <span className="vps-status-value">{apiStatus.version}</span>
                </div>
              )}
              {apiStatus.tunnels != null && (
                <div className="vps-status-row">
                  <span className="vps-status-label">Tunnels</span>
                  <span className="vps-status-value">{apiStatus.tunnels}</span>
                </div>
              )}
              {apiStatus.connectedVps != null && (
                <div className="vps-status-row">
                  <span className="vps-status-label">Connected VPS</span>
                  <span className="vps-status-value">{apiStatus.connectedVps}</span>
                </div>
              )}
              {apiStatus.exposedPorts != null && (
                <div className="vps-status-row">
                  <span className="vps-status-label">Exposed Ports</span>
                  <span className="vps-status-value">{apiStatus.exposedPorts}</span>
                </div>
              )}
            </div>
          )}
          {!apiStatus && !apiStatusError && (
            <p className="vps-muted">Loading...</p>
          )}
        </div>
      </div>

      {/* Configuration form */}
      <div className="vps-panel">
        <h3>Configure VPS</h3>
        <form className="vps-form" onSubmit={handleSubmit}>
          <div className="vps-form-grid">
            <div className="vps-field">
              <label htmlFor="vps-host">Host *</label>
              <div className="vps-host-wrapper">
                <input
                  ref={hostRef}
                  id="vps-host"
                  name="host"
                  type="text"
                  value={formData.host}
                  onChange={(e) => {
                    handleChange(e)
                    setShowSuggestions(true)
                    setActiveSuggestion(-1)
                  }}
                  onFocus={() => setShowSuggestions(true)}
                  onKeyDown={handleHostKeyDown}
                  onBlur={() => setTimeout(() => setShowSuggestions(false), 150)}
                  placeholder="e.g. my-server or 203.0.113.50"
                  autoComplete="off"
                  className={errors.host ? 'vps-input-error' : ''}
                />
                {showSuggestions && filteredHosts.length > 0 && (
                  <div className="vps-host-suggestions" ref={suggestionsRef}>
                    {filteredHosts.map((entry, idx) => (
                      <div
                        key={entry.host}
                        className={`vps-host-suggestion ${idx === activeSuggestion ? 'vps-host-suggestion-active' : ''}`}
                        onMouseDown={() => selectHost(entry)}
                      >
                        <span className="vps-host-suggestion-alias">{entry.host}</span>
                        <span className="vps-host-suggestion-detail">
                          {entry.hostName || entry.host}{entry.user ? ` (${entry.user})` : ''}
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
              {errors.host && <span className="vps-field-error">{errors.host}</span>}
            </div>

            <div className="vps-field">
              <label htmlFor="vps-port">SSH Port</label>
              <input
                id="vps-port"
                name="sshPort"
                type="number"
                min={1}
                max={65535}
                value={formData.sshPort}
                onChange={handleChange}
                className={errors.sshPort ? 'vps-input-error' : ''}
              />
              {errors.sshPort && <span className="vps-field-error">{errors.sshPort}</span>}
            </div>

            <div className="vps-field">
              <label htmlFor="vps-username">Username</label>
              <input
                id="vps-username"
                name="username"
                type="text"
                value={formData.username}
                onChange={handleChange}
                placeholder="root"
              />
            </div>

            <div className="vps-field">
              <label htmlFor="vps-authmode">Auth Mode</label>
              <select
                id="vps-authmode"
                name="authMode"
                value={formData.authMode}
                onChange={handleChange}
              >
                <option value="key">SSH Key</option>
                <option value="password">Password</option>
              </select>
            </div>

            {formData.authMode === 'key' && (
              <div className="vps-field vps-field-wide">
                <label htmlFor="vps-keypath">Private Key Path</label>
                <input
                  id="vps-keypath"
                  name="privateKeyPath"
                  type="text"
                  value={formData.privateKeyPath}
                  onChange={handleChange}
                  placeholder="e.g. ~/.ssh/id_rsa"
                  className={errors.privateKeyPath ? 'vps-input-error' : ''}
                />
                {errors.privateKeyPath && (
                  <span className="vps-field-error">{errors.privateKeyPath}</span>
                )}
              </div>
            )}

            {formData.authMode === 'password' && (
              <div className="vps-field vps-field-wide">
                <label htmlFor="vps-password">Password</label>
                <input
                  id="vps-password"
                  name="password"
                  type="password"
                  value={formData.password}
                  onChange={handleChange}
                  placeholder="SSH password"
                  className={errors.password ? 'vps-input-error' : ''}
                />
                {errors.password && (
                  <span className="vps-field-error">{errors.password}</span>
                )}
              </div>
            )}
          </div>

          <div className="vps-form-actions">
            <button type="submit" disabled={submitting} className="vps-submit-btn">
              {submitting ? 'Provisioning...' : 'Provision VPS'}
            </button>
          </div>

          {submitMessage && <p className="vps-submit-ok">{submitMessage}</p>}
          {submitError && <p className="vps-submit-err">{submitError}</p>}
        </form>
      </div>
    </div>
  )
}

export default VpsConfigForm
