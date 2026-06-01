import { useEffect } from 'react'
import { useWizard } from './WizardProvider.tsx'
import './PreflightStep.css'

export default function PreflightStep() {
  const { state, runPreflight } = useWizard()

  useEffect(() => {
    runPreflight()
  }, [runPreflight])

  const checks = state.preflightResult?.checks ?? []

  return (
    <div className="wizard-step wizard-step--preflight">
      <h2 className="wizard-step__title">Running Preflight Checks</h2>
      <p className="wizard-step__subtitle">
        Verifying your server meets all requirements...
      </p>

      <div className="wizard-step__checks">
        {checks.map((check) => (
          <div
            key={check.name}
            className={`wizard-step__check-item wizard-step__check-item--${check.status}`}
          >
            <span className="wizard-step__check-icon">
              {check.status === 'pass' ? '✓' : check.status === 'fail' ? '✗' : '⚠'}
            </span>
            <div className="wizard-step__check-info">
              <span className="wizard-step__check-name">{check.name}</span>
              <span className="wizard-step__check-msg">{check.message}</span>
            </div>
          </div>
        ))}

        {state.loading && checks.length === 0 && (
          <div className="wizard-step__check-item wizard-step__check-item--pending">
            <span className="wizard-step__spinner" />
            <span className="wizard-step__check-name">Checking server...</span>
          </div>
        )}
      </div>

      {state.error && (
        <p className="wizard-step__error">{state.error}</p>
      )}
    </div>
  )
}
