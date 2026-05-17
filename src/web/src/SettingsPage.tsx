import { useState, type FormEvent, type ChangeEvent } from 'react'
import './SettingsPage.css'

// --- Types ---

type DnsMode = 'cloudflare' | 'manual'

interface DnsFormData {
  mode: DnsMode
  cloudflareToken: string
  zoneName: string
  domain: string
}

// --- Component ---

function SettingsPage() {
  const [form, setForm] = useState<DnsFormData>({
    mode: 'cloudflare',
    cloudflareToken: '',
    zoneName: '',
    domain: '',
  })

  const [submitting, setSubmitting] = useState(false)
  const [submitMessage, setSubmitMessage] = useState<string | null>(null)
  const [submitError, setSubmitError] = useState<string | null>(null)

  function handleChange(
    e: ChangeEvent<HTMLInputElement | HTMLSelectElement>
  ) {
    const { name, value } = e.target
    setForm((prev) => ({ ...prev, [name]: value }))
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setSubmitMessage(null)
    setSubmitError(null)

    // Basic validation
    if (!form.domain.trim()) {
      setSubmitError('Domain is required.')
      return
    }

    if (form.mode === 'cloudflare' && !form.cloudflareToken.trim()) {
      setSubmitError('Cloudflare API token is required when using Cloudflare mode.')
      return
    }

    if (form.mode === 'cloudflare' && !form.zoneName.trim()) {
      setSubmitError('Zone name is required when using Cloudflare mode.')
      return
    }

    setSubmitting(true)
    try {
      const res = await fetch('/api/dns/configure', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      })

      if (res.ok) {
        setSubmitMessage('DNS configuration saved successfully.')
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

  return (
    <div className="settings-section">
      {/* DNS Configuration */}
      <div className="settings-panel">
        <h3>DNS Configuration</h3>
        <form className="settings-form" onSubmit={handleSubmit}>
          <div className="settings-form-grid">
            {/* DNS Mode */}
            <div className="settings-field">
              <label htmlFor="dns-mode">DNS Mode</label>
              <select
                id="dns-mode"
                name="mode"
                value={form.mode}
                onChange={handleChange}
              >
                <option value="cloudflare">Cloudflare</option>
                <option value="manual">Manual</option>
              </select>
            </div>

            {/* Domain */}
            <div className="settings-field">
              <label htmlFor="dns-domain">Domain *</label>
              <input
                id="dns-domain"
                name="domain"
                type="text"
                value={form.domain}
                onChange={handleChange}
                placeholder="e.g. example.com"
              />
            </div>

            {/* Cloudflare-specific fields */}
            {form.mode === 'cloudflare' && (
              <>
                <div className="settings-field">
                  <label htmlFor="dns-cf-token">Cloudflare API Token *</label>
                  <input
                    id="dns-cf-token"
                    name="cloudflareToken"
                    type="password"
                    value={form.cloudflareToken}
                    onChange={handleChange}
                    placeholder="Cloudflare API token"
                  />
                </div>

                <div className="settings-field">
                  <label htmlFor="dns-zone">Zone Name *</label>
                  <input
                    id="dns-zone"
                    name="zoneName"
                    type="text"
                    value={form.zoneName}
                    onChange={handleChange}
                    placeholder="e.g. example.com"
                  />
                </div>
              </>
            )}
          </div>

          <div className="settings-form-actions">
            <button
              type="submit"
              className="settings-submit-btn"
              disabled={submitting}
            >
              {submitting ? 'Saving...' : 'Save DNS Configuration'}
            </button>
          </div>

          {submitMessage && (
            <p className="settings-submit-ok">{submitMessage}</p>
          )}
          {submitError && (
            <p className="settings-submit-err">{submitError}</p>
          )}
        </form>
      </div>
    </div>
  )
}

export default SettingsPage
