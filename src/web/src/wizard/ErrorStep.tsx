import { useWizard } from './WizardProvider.tsx'
import './ErrorStep.css'

export default function ErrorStep() {
  const { state, retryFromSsh, abandon } = useWizard()

  return (
    <div className="wizard-step wizard-step--error">
      <div className="wizard-step__icon">⚠️</div>
      <h2 className="wizard-step__title">Setup Failed</h2>
      <p className="wizard-step__subtitle">
        Something went wrong during setup.
      </p>

      {state.error && (
        <div className="wizard-step__error-box">
          {state.error}
        </div>
      )}

      <div className="wizard-step__actions wizard-step__actions--center">
        <button
          className="wizard-step__btn wizard-step__btn--primary"
          onClick={retryFromSsh}
          disabled={state.loading}
        >
          Retry from SSH
        </button>
        <button
          className="wizard-step__btn wizard-step__btn--danger"
          onClick={abandon}
          disabled={state.loading}
        >
          Abandon
        </button>
      </div>
    </div>
  )
}
