import { useWizard } from './WizardProvider.tsx'
import './WelcomeStep.css'

const PREREQUISITES = [
  'A VPS or dedicated server with a public IP address',
  'SSH access with root or sudo privileges',
  'A domain name (optional — free subdomain available)',
  'Cloudflare account (optional, for BYO domain with Cloudflare DNS)',
]

export default function WelcomeStep() {
  const { state, startWizard } = useWizard()

  return (
    <div className="wizard-step wizard-step--welcome">
      <div className="wizard-step__icon">🚀</div>
      <h2 className="wizard-step__title">Welcome to Unet Setup</h2>
      <p className="wizard-step__subtitle">
        This wizard will guide you through connecting your VPS and setting up
        your first peer. Before we begin, make sure you have:
      </p>

      <ul className="wizard-step__checklist">
        {PREREQUISITES.map((item) => (
          <li key={item} className="wizard-step__checklist-item">
            <span className="wizard-step__check">✓</span>
            {item}
          </li>
        ))}
      </ul>

      {state.error && (
        <p className="wizard-step__error">{state.error}</p>
      )}

      <button
        className="wizard-step__btn wizard-step__btn--primary"
        onClick={startWizard}
        disabled={state.loading}
      >
        {state.loading ? 'Starting...' : 'Get Started'}
      </button>
    </div>
  )
}
