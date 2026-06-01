export type WizardStep =
  | 'welcome'
  | 'ssh'
  | 'preflight'
  | 'domain_mode'
  | 'domain_check'
  | 'cloudflare'
  | 'create_peer'
  | 'commit'
  | 'success'
  | 'error'

export type SessionStatus = 'active' | 'completed' | 'abandoned' | 'error'

export type AuthType = 'key' | 'password'

export type DomainMode = 'byo' | 'nipio'

export interface SSHInput {
  host: string
  port: number
  user: string
  auth_type: AuthType
  key_path: string
  password: string
}

export interface PreflightCheckResult {
  name: string
  status: 'pass' | 'fail' | 'warn'
  message: string
}

export interface PreflightResult {
  checks: PreflightCheckResult[]
  all_passed: boolean
}

export interface DomainCheckResult {
  domain: string
  dns_resolves: boolean
  cloudflare_detected: boolean
  tls_strategy: string
  available: boolean
  message: string
}

export interface PortExpose {
  local_port: number
  protocol: string
}

export interface WizardInputs {
  ssh: SSHInput | null
  domain_mode: DomainMode | null
  domain: string
  cloudflare_token: string
  peer_name: string
  ports: PortExpose[]
}

export interface WizardState {
  session_id: string
  current_step: WizardStep
  status: SessionStatus
  progress_pct: number
  started_at: string
  inputs: WizardInputs
  preflight_result: PreflightResult | null
  domain_check_result: DomainCheckResult | null
}

export interface StartSessionResponse {
  session_id: string
  current_step: WizardStep
  status: SessionStatus
  progress_pct: number
  started_at: string
}

export interface SubmitStepRequest {
  data: Record<string, unknown>
}

export interface WizardApiError {
  code: string
  message: string
  context?: Record<string, unknown>
}
