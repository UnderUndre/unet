import type {
  WizardState,
  StartSessionResponse,
  SubmitStepRequest,
  WizardApiError,
} from './types.ts'

const BASE = '/v1/wizard'

class WizardError extends Error {
  code: string
  context?: Record<string, unknown>

  constructor(apiError: WizardApiError) {
    super(apiError.message)
    this.name = 'WizardError'
    this.code = apiError.code
    this.context = apiError.context
  }
}

async function request<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  })

  if (!res.ok) {
    let apiError: WizardApiError
    try {
      apiError = await res.json()
    } catch {
      apiError = {
        code: `HTTP_${res.status}`,
        message: `Request failed with status ${res.status}`,
      }
    }
    throw new WizardError(apiError)
  }

  return res.json() as Promise<T>
}

export function startSession(): Promise<StartSessionResponse> {
  return request<StartSessionResponse>('/sessions', { method: 'POST' })
}

export function getSession(id: string): Promise<WizardState> {
  return request<WizardState>(`/sessions/${id}`)
}

export function abandonSession(id: string): Promise<void> {
  return request<void>(`/sessions/${id}`, { method: 'DELETE' })
}

export function submitStep(
  sessionId: string,
  step: string,
  data: Record<string, unknown>,
): Promise<WizardState> {
  const body: SubmitStepRequest = { data }
  return request<WizardState>(`/sessions/${sessionId}/steps/${step}`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function runPreflight(sessionId: string): Promise<WizardState> {
  return request<WizardState>(`/sessions/${sessionId}/preflight`, {
    method: 'POST',
  })
}

export function commitSession(sessionId: string): Promise<WizardState> {
  return request<WizardState>(`/sessions/${sessionId}/commit`, {
    method: 'POST',
  })
}

export { WizardError }
