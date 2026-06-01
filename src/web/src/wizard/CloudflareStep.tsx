import { useState } from 'react'
import { useWizard } from './WizardProvider.tsx'
import './CloudflareStep.css'

export default function CloudflareStep() {
  const { state, submitStep, goBack } = useWizard()
  const [token, setToken] = useState(state.inputs.cloudflare_token ?? '')

  const handleSubmit = () => {
    submitStep('cloudflare', { cloudflare_token: token.trim() })
  }

  const handleSkip = () => {
    submitStep('cloudflare', { cloudflare_token: '' })
  }

  return (
    <div className="wizard-step wizard-step--cloudflare">
      <h2 className="wizard-step__title">Cloudflare Setup</h2>
      <p className="wizard-step__subtitle">
        Your domain is using Cloudflare DNS. Provide an API token for automatic
        DNS and TLS configuration, or skip to set up manually.
      </p>

      <div className="wizard-step__form">
        <label className="wizard-step__field">
          <span className="wizard-step__label">CF API Token</span>
          <input
            className="wizard-step__input"
            type="password"
            placeholder="Enter your Cloudflare API token"
            value={token}
            onChange={(e) => setToken(e.target.value)}
          />
        </label>
      </div>

      {state.error && (
        <p className="wizard-step__error">{state.error}</p>
      )}

      <div className="wizard-step__actions">
        <button
          className="wizard-step__btn wizard-step__btn--secondary"
          onClick={() => goBack('domain_check')}
          disabled={state.loading}
        >
          Back
        </button>
        <button
          className="wizard-step__btn wizard-step__btn--link"
          onClick={handleSkip}
          disabled={state.loading}
        >
          Skip
        </button>
        <button
          className="wizard-step__btn wizard-step__btn--primary"
          onClick={handleSubmit}
          disabled={state.loading || token.trim() === ''}
        >
          {state.loading ? 'Validating...' : 'Validate'}
        </button>
      </div>
    </div>
  )
}
