import {
  createContext,
  useContext,
  useReducer,
  useEffect,
  useCallback,
  type ReactNode,
} from 'react'
import type {
  WizardStep,
  WizardState,
  WizardInputs,
  PreflightResult,
  DomainCheckResult,
} from './types.ts'
import * as api from './api.ts'

const STORAGE_KEY = 'wizard_session_id'

export interface WizardContextState {
  sessionId: string | null
  currentStep: WizardStep
  progressPct: number
  inputs: WizardInputs
  preflightResult: PreflightResult | null
  domainCheckResult: DomainCheckResult | null
  loading: boolean
  error: string | null
  status: WizardState['status'] | null
}

type Action =
  | { type: 'SESSION_STARTED'; state: WizardState }
  | { type: 'STEP_COMPLETE'; state: WizardState }
  | { type: 'STEP_ERROR'; error: string }
  | { type: 'PREFLIGHT_RESULT'; state: WizardState }
  | { type: 'DOMAIN_CHECK_RESULT'; state: WizardState }
  | { type: 'STEP_BACK'; state: WizardState }
  | { type: 'RESUME'; state: WizardState }
  | { type: 'COMMIT_STARTED' }
  | { type: 'COMMIT_SUCCESS'; state: WizardState }
  | { type: 'COMMIT_FAILURE'; error: string }
  | { type: 'SET_LOADING'; loading: boolean }

const initialInputs: WizardInputs = {
  ssh: null,
  domain_mode: null,
  domain: '',
  cloudflare_token: '',
  peer_name: '',
  ports: [],
}

const initialState: WizardContextState = {
  sessionId: null,
  currentStep: 'welcome',
  progressPct: 0,
  inputs: initialInputs,
  preflightResult: null,
  domainCheckResult: null,
  loading: false,
  error: null,
  status: null,
}

function reducer(state: WizardContextState, action: Action): WizardContextState {
  switch (action.type) {
    case 'SESSION_STARTED':
      return {
        ...state,
        sessionId: action.state.session_id,
        currentStep: action.state.current_step,
        progressPct: action.state.progress_pct,
        status: action.state.status,
        inputs: action.state.inputs ?? initialInputs,
        loading: false,
        error: null,
      }
    case 'STEP_COMPLETE':
      return {
        ...state,
        currentStep: action.state.current_step,
        progressPct: action.state.progress_pct,
        inputs: action.state.inputs ?? state.inputs,
        preflightResult: action.state.preflight_result ?? state.preflightResult,
        domainCheckResult: action.state.domain_check_result ?? state.domainCheckResult,
        loading: false,
        error: null,
      }
    case 'STEP_ERROR':
      return { ...state, loading: false, error: action.error }
    case 'PREFLIGHT_RESULT':
      return {
        ...state,
        preflightResult: action.state.preflight_result,
        currentStep: action.state.current_step,
        progressPct: action.state.progress_pct,
        loading: false,
      }
    case 'DOMAIN_CHECK_RESULT':
      return {
        ...state,
        domainCheckResult: action.state.domain_check_result,
        currentStep: action.state.current_step,
        progressPct: action.state.progress_pct,
        loading: false,
      }
    case 'STEP_BACK':
      return {
        ...state,
        currentStep: action.state.current_step,
        progressPct: action.state.progress_pct,
        loading: false,
        error: null,
      }
    case 'RESUME':
      return {
        ...state,
        sessionId: action.state.session_id,
        currentStep: action.state.current_step,
        progressPct: action.state.progress_pct,
        status: action.state.status,
        inputs: action.state.inputs ?? initialInputs,
        preflightResult: action.state.preflight_result,
        domainCheckResult: action.state.domain_check_result,
        loading: false,
        error: null,
      }
    case 'COMMIT_STARTED':
      return { ...state, loading: true, error: null }
    case 'COMMIT_SUCCESS':
      return {
        ...state,
        currentStep: action.state.current_step,
        progressPct: action.state.progress_pct,
        status: action.state.status,
        loading: false,
        error: null,
      }
    case 'COMMIT_FAILURE':
      return {
        ...state,
        currentStep: 'error',
        loading: false,
        error: action.error,
      }
    case 'SET_LOADING':
      return { ...state, loading: action.loading }
    default:
      return state
  }
}

interface WizardContextValue {
  state: WizardContextState
  startWizard: () => Promise<void>
  submitStep: (step: string, data: Record<string, unknown>) => Promise<void>
  goBack: (step: string) => Promise<void>
  runPreflight: () => Promise<void>
  commit: () => Promise<void>
  abandon: () => Promise<void>
  retryFromSsh: () => Promise<void>
}

const WizardContext = createContext<WizardContextValue | null>(null)

export function useWizard(): WizardContextValue {
  const ctx = useContext(WizardContext)
  if (!ctx) throw new Error('useWizard must be used within WizardProvider')
  return ctx
}

export function WizardProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, initialState)

  useEffect(() => {
    const storedId = localStorage.getItem(STORAGE_KEY)
    if (!storedId) return

    let cancelled = false
    ;(async () => {
      try {
        const session = await api.getSession(storedId)
        if (cancelled) return
        if (session.status === 'active') {
          dispatch({ type: 'RESUME', state: session })
        } else {
          localStorage.removeItem(STORAGE_KEY)
        }
      } catch {
        localStorage.removeItem(STORAGE_KEY)
      }
    })()
    return () => { cancelled = true }
  }, [])

  const startWizard = useCallback(async () => {
    dispatch({ type: 'SET_LOADING', loading: true })
    try {
      const session = await api.startSession()
      localStorage.setItem(STORAGE_KEY, session.session_id)
      dispatch({ type: 'SESSION_STARTED', state: session })
      const afterWelcome = await api.submitStep(session.session_id, 'welcome', {})
      dispatch({ type: 'STEP_COMPLETE', state: afterWelcome })
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to start wizard'
      dispatch({ type: 'STEP_ERROR', error: msg })
    }
  }, [])

  const submitStepAction = useCallback(
    async (step: string, data: Record<string, unknown>) => {
      if (!state.sessionId) return
      dispatch({ type: 'SET_LOADING', loading: true })
      try {
        const result = await api.submitStep(state.sessionId, step, data)
        if (step === 'domain_check') {
          dispatch({ type: 'DOMAIN_CHECK_RESULT', state: result })
        } else {
          dispatch({ type: 'STEP_COMPLETE', state: result })
        }
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : 'Step submission failed'
        dispatch({ type: 'STEP_ERROR', error: msg })
      }
    },
    [state.sessionId],
  )

  const goBack = useCallback(
    async (step: string) => {
      if (!state.sessionId) return
      dispatch({ type: 'SET_LOADING', loading: true })
      try {
        const result = await api.submitStep(state.sessionId, step, { direction: 'back' })
        dispatch({ type: 'STEP_BACK', state: result })
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : 'Navigation failed'
        dispatch({ type: 'STEP_ERROR', error: msg })
      }
    },
    [state.sessionId],
  )

  const runPreflightAction = useCallback(async () => {
    if (!state.sessionId) return
    dispatch({ type: 'SET_LOADING', loading: true })
    try {
      const result = await api.runPreflight(state.sessionId)
      dispatch({ type: 'PREFLIGHT_RESULT', state: result })
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Preflight failed'
      dispatch({ type: 'STEP_ERROR', error: msg })
    }
  }, [state.sessionId])

  const commitAction = useCallback(async () => {
    if (!state.sessionId) return
    dispatch({ type: 'COMMIT_STARTED' })
    try {
      const result = await api.commitSession(state.sessionId)
      dispatch({ type: 'COMMIT_SUCCESS', state: result })
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Commit failed'
      dispatch({ type: 'COMMIT_FAILURE', error: msg })
    }
  }, [state.sessionId])

  const abandon = useCallback(async () => {
    if (!state.sessionId) return
    try {
      await api.abandonSession(state.sessionId)
    } catch {
      // session cleanup best-effort
    }
    localStorage.removeItem(STORAGE_KEY)
    dispatch({ type: 'RESUME', state: { ...initialState, currentStep: 'welcome' } as WizardState })
  }, [state.sessionId])

  const retryFromSsh = useCallback(async () => {
    if (!state.sessionId) return
    dispatch({ type: 'SET_LOADING', loading: true })
    try {
      const result = await api.submitStep(state.sessionId, 'ssh', { retry: true })
      dispatch({ type: 'STEP_BACK', state: result })
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Retry failed'
      dispatch({ type: 'STEP_ERROR', error: msg })
    }
  }, [state.sessionId])

  return (
    <WizardContext.Provider
      value={{
        state,
        startWizard,
        submitStep: submitStepAction,
        goBack,
        runPreflight: runPreflightAction,
        commit: commitAction,
        abandon,
        retryFromSsh,
      }}
    >
      {children}
    </WizardContext.Provider>
  )
}
