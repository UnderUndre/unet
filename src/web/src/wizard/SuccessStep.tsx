import { useWizard } from './WizardProvider.tsx'
import './SuccessStep.css'

export default function SuccessStep() {
  const { state } = useWizard()

  const firstUrl = state.inputs.domain
    ? `https://${state.inputs.domain}`
    : ''

  return (
    <div className="wizard-step wizard-step--success">
      <div className="wizard-step__icon">🎉</div>
      <h2 className="wizard-step__title">Setup Complete!</h2>
      <p className="wizard-step__subtitle">
        Your VPS has been configured and your first peer is ready.
      </p>

      {firstUrl && (
        <div className="wizard-step__success-url">
          <span className="wizard-step__label">Your first URL</span>
          <a
            className="wizard-step__link"
            href={firstUrl}
            target="_blank"
            rel="noopener noreferrer"
          >
            {firstUrl}
          </a>
        </div>
      )}

      <div className="wizard-step__qr-placeholder">
        <span>QR Code placeholder</span>
      </div>

      <button
        className="wizard-step__btn wizard-step__btn--primary"
        onClick={() => window.location.href = '/'}
      >
        Open Dashboard
      </button>
    </div>
  )
}
