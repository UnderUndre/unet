import { useState } from 'react'
import { useWizard } from './WizardProvider.tsx'
import type { AuthType } from './types.ts'
import './SshStep.css'

export default function SshStep() {
  const { state, submitStep, goBack } = useWizard()
  const [host, setHost] = useState(state.inputs.ssh?.host ?? '')
  const [port, setPort] = useState(String(state.inputs.ssh?.port ?? 22))
  const [user, setUser] = useState(state.inputs.ssh?.user ?? 'root')
  const [authType, setAuthType] = useState<AuthType>(state.inputs.ssh?.auth_type ?? 'key')
  const [keyPath, setKeyPath] = useState(state.inputs.ssh?.key_path ?? '')
  const [password, setPassword] = useState('')

  const portNum = Number(port)
  const isPortValid = port !== '' && Number.isFinite(portNum) && Number.isInteger(portNum) && portNum >= 1 && portNum <= 65535

  const canSubmit = host.trim() !== '' && user.trim() !== '' && isPortValid &&
    (authType === 'key' ? keyPath.trim() !== '' : password.trim() !== '')

  const handleSubmit = () => {
    submitStep('ssh', {
      host: host.trim(),
      port: isPortValid ? portNum : 0,
      user: user.trim(),
      auth_type: authType,
      key_path: keyPath.trim(),
      password: password,
    })
  }

  return (
    <div className="wizard-step wizard-step--ssh">
      <h2 className="wizard-step__title">Connect to your VPS</h2>
      <p className="wizard-step__subtitle">
        Provide SSH credentials for the server you want to set up.
      </p>

      <div className="wizard-step__form">
        <label className="wizard-step__field">
          <span className="wizard-step__label">Host</span>
          <input
            className="wizard-step__input"
            type="text"
            placeholder="203.0.113.50 or vps.example.com"
            value={host}
            onChange={(e) => setHost(e.target.value)}
          />
        </label>

        <div className="wizard-step__row">
          <label className="wizard-step__field wizard-step__field--small">
            <span className="wizard-step__label">Port</span>
            <input
              className="wizard-step__input"
              type="number"
              value={port}
              onChange={(e) => setPort(e.target.value)}
            />
          </label>

          <label className="wizard-step__field wizard-step__field--small">
            <span className="wizard-step__label">User</span>
            <input
              className="wizard-step__input"
              type="text"
              value={user}
              onChange={(e) => setUser(e.target.value)}
            />
          </label>
        </div>

        <div className="wizard-step__toggle-group">
          <button
            className={`wizard-step__toggle${authType === 'key' ? ' active' : ''}`}
            onClick={() => setAuthType('key')}
            type="button"
          >
            SSH Key
          </button>
          <button
            className={`wizard-step__toggle${authType === 'password' ? ' active' : ''}`}
            onClick={() => setAuthType('password')}
            type="button"
          >
            Password
          </button>
        </div>

        {authType === 'key' ? (
          <label className="wizard-step__field">
            <span className="wizard-step__label">Key Path</span>
            <input
              className="wizard-step__input"
              type="text"
              placeholder="~/.ssh/id_rsa"
              value={keyPath}
              onChange={(e) => setKeyPath(e.target.value)}
            />
          </label>
        ) : (
          <label className="wizard-step__field">
            <span className="wizard-step__label">Password</span>
            <input
              className="wizard-step__input"
              type="password"
              placeholder="Server password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </label>
        )}
      </div>

      {state.error && (
        <p className="wizard-step__error">{state.error}</p>
      )}

      <div className="wizard-step__actions">
        <button
          className="wizard-step__btn wizard-step__btn--secondary"
          onClick={() => goBack('welcome')}
          disabled={state.loading}
        >
          Back
        </button>
        <button
          className="wizard-step__btn wizard-step__btn--primary"
          onClick={handleSubmit}
          disabled={state.loading || !canSubmit}
        >
          {state.loading ? 'Connecting...' : 'Connect'}
        </button>
      </div>
    </div>
  )
}
