import { useState } from 'react'
import { useWizard } from './WizardProvider.tsx'
import type { PortExpose } from './types.ts'
import './CreatePeerStep.css'

const PEER_NAME_RE = /^[a-zA-Z0-9_-]+$/

export default function CreatePeerStep() {
  const { state, submitStep, goBack } = useWizard()
  const [peerName, setPeerName] = useState(state.inputs.peer_name ?? '')
  const [ports, setPorts] = useState<PortExpose[]>(state.inputs.ports.length > 0 ? state.inputs.ports : [])
  const [newPort, setNewPort] = useState('')
  const [newProtocol, setNewProtocol] = useState('http')

  const nameValid = peerName.length > 0 && PEER_NAME_RE.test(peerName)

  const addPort = () => {
    const portNum = Number(newPort)
    if (!Number.isFinite(portNum) || portNum < 1 || portNum > 65535) return
    setPorts([...ports, { local_port: portNum, protocol: newProtocol }])
    setNewPort('')
  }

  const removePort = (idx: number) => {
    setPorts(ports.filter((_, i) => i !== idx))
  }

  const handleSubmit = () => {
    submitStep('create_peer', {
      peer_name: peerName,
      ports,
    })
  }

  const backStep = state.domainCheckResult?.cloudflare_detected ? 'cloudflare' : 'domain_check'

  return (
    <div className="wizard-step wizard-step--create-peer">
      <h2 className="wizard-step__title">Create Your First Peer</h2>
      <p className="wizard-step__subtitle">
        Name your peer and optionally expose local ports.
      </p>

      <div className="wizard-step__form">
        <label className="wizard-step__field">
          <span className="wizard-step__label">Peer Name</span>
          <input
            className="wizard-step__input"
            type="text"
            placeholder="my-vps"
            value={peerName}
            onChange={(e) => setPeerName(e.target.value)}
          />
          {peerName && !nameValid && (
            <span className="wizard-step__hint">
              Only letters, numbers, hyphens, and underscores
            </span>
          )}
        </label>

        <div className="wizard-step__ports">
          <span className="wizard-step__label">Exposed Ports (optional)</span>

          {ports.length > 0 && (
            <div className="wizard-step__port-list">
              {ports.map((p, i) => (
                <span key={i} className="wizard-step__port-tag">
                  {p.local_port}/{p.protocol}
                  <button
                    className="wizard-step__port-remove"
                    onClick={() => removePort(i)}
                    type="button"
                  >
                    ×
                  </button>
                </span>
              ))}
            </div>
          )}

          <div className="wizard-step__port-add">
            <input
              className="wizard-step__input wizard-step__input--small"
              type="number"
              placeholder="Port"
              value={newPort}
              onChange={(e) => setNewPort(e.target.value)}
            />
            <select
              className="wizard-step__select"
              value={newProtocol}
              onChange={(e) => setNewProtocol(e.target.value)}
            >
              <option value="http">HTTP</option>
              <option value="https">HTTPS</option>
              <option value="tcp">TCP</option>
            </select>
            <button
              className="wizard-step__btn wizard-step__btn--secondary wizard-step__btn--small"
              onClick={addPort}
              type="button"
              disabled={!newPort}
            >
              Add
            </button>
          </div>
        </div>
      </div>

      {state.error && (
        <p className="wizard-step__error">{state.error}</p>
      )}

      <div className="wizard-step__actions">
        <button
          className="wizard-step__btn wizard-step__btn--secondary"
          onClick={() => goBack(backStep)}
          disabled={state.loading}
        >
          Back
        </button>
        <button
          className="wizard-step__btn wizard-step__btn--primary"
          onClick={handleSubmit}
          disabled={state.loading || !nameValid}
        >
          {state.loading ? 'Creating...' : 'Create'}
        </button>
      </div>
    </div>
  )
}
