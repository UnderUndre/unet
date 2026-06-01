import { useEffect } from 'react'
import { useWizard } from './WizardProvider.tsx'
import './CommitStep.css'

export default function CommitStep() {
  const { commit } = useWizard()

  useEffect(() => {
    commit()
  }, [commit])

  return (
    <div className="wizard-step wizard-step--commit">
      <div className="wizard-step__spinner-large" />
      <h2 className="wizard-step__title">Setting up your VPS</h2>
      <p className="wizard-step__subtitle">
        This may take a few minutes. Please don't close this page.
      </p>
      <div className="wizard-step__progress-bar">
        <div className="wizard-step__progress-indeterminate" />
      </div>
    </div>
  )
}
