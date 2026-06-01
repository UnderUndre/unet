import { useState } from 'react'
import { useWizard } from './WizardProvider.tsx'
import './DomainCheckStep.css'

export default function DomainCheckStep() {
  const { state, submitStep, goBack } = useWizard()
  const [domain, setDomain] = useState(state.inputs.domain ?? '')

  const result = state.domainCheckResult
  const isNipio = state.inputs.domain_mode === 'nipio'

  const handleSubmit = () => {
    submitStep('domain_check', { domain: domain.trim() })
  }

  return (
    <div className="wizard-step wizard-step--domain-check">
      <h2 className="wizard-step__title">
        {isNipio ? 'Confirm Subdomain' : 'Check Domain'}
      </h2>
      <p className="wizard-step__subtitle">
        {isNipio
          ? 'Your subdomain will be based on the VPS IP address.'
          : 'Enter your domain name to verify DNS and TLS configuration.'}
      </p>

      <div className="wizard-step__form">
        <label className="wizard-step__field">
          <span className="wizard-step__label">Domain</span>
          <input
            className="wizard-step__input"
            type="text"
            placeholder={isNipio ? 'myservice.203.0.113.50.nip.io' : 'example.com'}
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
          />
        </label>
      </div>

      {result && (
        <div className="wizard-step__domain-result">
          <div className={`wizard-step__domain-status wizard-step__domain-status--${result.available ? 'ok' : 'fail'}`}>
            {result.available ? '✓ Domain available' : '✗ Domain not available'}
          </div>
          <div className="wizard-step__domain-detail">
            <span>DNS resolves: {result.dns_resolves ? 'Yes' : 'No'}</span>
            <span>Cloudflare: {result.cloudflare_detected ? 'Detected' : 'Not detected'}</span>
            <span>TLS strategy: {result.tls_strategy}</span>
          </div>
          {result.message && (
            <p className="wizard-step__domain-msg">{result.message}</p>
          )}
        </div>
      )}

      {state.error && (
        <p className="wizard-step__error">{state.error}</p>
      )}

      <div className="wizard-step__actions">
        <button
          className="wizard-step__btn wizard-step__btn--secondary"
          onClick={() => goBack('domain_mode')}
          disabled={state.loading}
        >
          Back
        </button>
        <button
          className="wizard-step__btn wizard-step__btn--primary"
          onClick={handleSubmit}
          disabled={state.loading || domain.trim() === ''}
        >
          {state.loading ? 'Checking...' : 'Check Domain'}
        </button>
      </div>
    </div>
  )
}
