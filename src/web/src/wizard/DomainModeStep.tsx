import { useState } from 'react'
import { useWizard } from './WizardProvider.tsx'
import type { DomainMode } from './types.ts'
import './DomainModeStep.css'

const OPTIONS: Array<{
  mode: DomainMode
  title: string
  description: string
  icon: string
}> = [
  {
    mode: 'byo',
    title: 'I have a domain',
    description: 'Use your own domain name with DNS configured',
    icon: '🌐',
  },
  {
    mode: 'nipio',
    title: 'Use free subdomain',
    description: 'Get a free .nip.io subdomain based on your VPS IP',
    icon: '🔗',
  },
]

export default function DomainModeStep() {
  const { state, submitStep, goBack } = useWizard()
  const [selected, setSelected] = useState<DomainMode>(state.inputs.domain_mode ?? 'byo')

  return (
    <div className="wizard-step wizard-step--domain-mode">
      <h2 className="wizard-step__title">Choose Domain Setup</h2>
      <p className="wizard-step__subtitle">
        How would you like to access your services?
      </p>

      <div className="wizard-step__cards">
        {OPTIONS.map((opt) => (
          <button
            key={opt.mode}
            className={`wizard-step__card${selected === opt.mode ? ' wizard-step__card--selected' : ''}`}
            onClick={() => setSelected(opt.mode)}
            type="button"
          >
            <span className="wizard-step__card-icon">{opt.icon}</span>
            <span className="wizard-step__card-title">{opt.title}</span>
            <span className="wizard-step__card-desc">{opt.description}</span>
          </button>
        ))}
      </div>

      {state.error && (
        <p className="wizard-step__error">{state.error}</p>
      )}

      <div className="wizard-step__actions">
        <button
          className="wizard-step__btn wizard-step__btn--secondary"
          onClick={() => goBack('ssh')}
          disabled={state.loading}
        >
          Back
        </button>
        <button
          className="wizard-step__btn wizard-step__btn--primary"
          onClick={() => submitStep('domain_mode', { domain_mode: selected })}
          disabled={state.loading}
        >
          Continue
        </button>
      </div>
    </div>
  )
}
