import type { WizardStep } from './types.ts'
import { useWizard } from './WizardProvider.tsx'
import WelcomeStep from './WelcomeStep.tsx'
import SshStep from './SshStep.tsx'
import PreflightStep from './PreflightStep.tsx'
import DomainModeStep from './DomainModeStep.tsx'
import DomainCheckStep from './DomainCheckStep.tsx'
import CloudflareStep from './CloudflareStep.tsx'
import CreatePeerStep from './CreatePeerStep.tsx'
import CommitStep from './CommitStep.tsx'
import SuccessStep from './SuccessStep.tsx'
import ErrorStep from './ErrorStep.tsx'
import './WizardPage.css'

const STEP_ORDER: WizardStep[] = [
  'welcome',
  'ssh',
  'preflight',
  'domain_mode',
  'domain_check',
  'cloudflare',
  'create_peer',
  'commit',
  'success',
]

const STEP_LABELS: Record<WizardStep, string> = {
  welcome: 'Welcome',
  ssh: 'SSH',
  preflight: 'Preflight',
  domain_mode: 'Domain',
  domain_check: 'DNS',
  cloudflare: 'CF',
  create_peer: 'Peer',
  commit: 'Setup',
  success: 'Done',
  error: 'Error',
}

function StepRenderer({ step }: { step: WizardStep }) {
  switch (step) {
    case 'welcome': return <WelcomeStep />
    case 'ssh': return <SshStep />
    case 'preflight': return <PreflightStep />
    case 'domain_mode': return <DomainModeStep />
    case 'domain_check': return <DomainCheckStep />
    case 'cloudflare': return <CloudflareStep />
    case 'create_peer': return <CreatePeerStep />
    case 'commit': return <CommitStep />
    case 'success': return <SuccessStep />
    case 'error': return <ErrorStep />
    default: return <WelcomeStep />
  }
}

export default function WizardPage() {
  const { state } = useWizard()
  const currentIdx = STEP_ORDER.indexOf(state.currentStep)

  return (
    <div className="wizard">
      <div className="wizard__progress">
        <div
          className="wizard__progress-fill"
          style={{ width: `${state.progressPct}%` }}
        />
      </div>

      {state.currentStep !== 'welcome' && state.currentStep !== 'success' && state.currentStep !== 'error' && (
        <div className="wizard__steps">
          {STEP_ORDER.map((step, idx) => {
            if (step === 'welcome' || step === 'success') return null
            const isCompleted = idx < currentIdx
            const isCurrent = step === state.currentStep
            const isSkipped = step === 'cloudflare' && idx > currentIdx

            if (isSkipped) return null

            return (
              <div
                key={step}
                className={`wizard__step-dot${isCompleted ? ' wizard__step-dot--completed' : ''}${isCurrent ? ' wizard__step-dot--current' : ''}`}
                title={STEP_LABELS[step]}
              >
                <span className="wizard__step-dot-inner">
                  {isCompleted ? '✓' : idx + 1}
                </span>
                <span className="wizard__step-label">{STEP_LABELS[step]}</span>
              </div>
            )
          })}
        </div>
      )}

      <div className="wizard__content">
        <StepRenderer step={state.currentStep} />
      </div>
    </div>
  )
}
