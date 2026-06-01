package wizard

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type WizardStep string

const (
	StepWelcome    WizardStep = "welcome"
	StepSSH        WizardStep = "ssh"
	StepPreflight  WizardStep = "preflight"
	StepDomainMode WizardStep = "domain_mode"
	StepDomainCheck WizardStep = "domain_check"
	StepCloudflare WizardStep = "cloudflare"
	StepCreatePeer WizardStep = "create_peer"
	StepCommit     WizardStep = "commit"
	StepSuccess    WizardStep = "success"
	StepError      WizardStep = "error"
)

var validSteps = map[WizardStep]bool{
	StepWelcome:     true,
	StepSSH:         true,
	StepPreflight:   true,
	StepDomainMode:  true,
	StepDomainCheck: true,
	StepCloudflare:  true,
	StepCreatePeer:  true,
	StepCommit:      true,
	StepSuccess:     true,
	StepError:       true,
}

func IsValidStep(s WizardStep) bool {
	return validSteps[s]
}

type SessionStatus string

const (
	StatusInProgress SessionStatus = "in_progress"
	StatusCommitted  SessionStatus = "committed"
	StatusAbandoned  SessionStatus = "abandoned"
)

type WizardInputs struct {
	SSH            *SSHInput    `json:"ssh,omitempty"`
	DomainMode     string       `json:"domain_mode,omitempty"`
	Domain         string       `json:"domain,omitempty"`
	CloudflareToken string      `json:"cloudflare_token,omitempty"`
	NipioEnabled   bool         `json:"nipio_enabled,omitempty"`
	FirstPeerName  string       `json:"first_peer_name,omitempty"`
	FirstPortExpose *PortExpose `json:"first_port_expose,omitempty"`
}

type SSHInput struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	AuthType string `json:"auth_type"`
	KeyPath  string `json:"key_path,omitempty"`
	Password string `json:"password,omitempty"`
}

type PortExpose struct {
	LocalPort int    `json:"local_port"`
	Subdomain string `json:"subdomain,omitempty"`
}

type PreflightResult struct {
	TargetHost      string   `json:"target_host"`
	CheckedAt       string   `json:"checked_at"`
	Distro          string   `json:"distro"`
	DistroVersion   string   `json:"distro_version"`
	Arch            string   `json:"arch"`
	DiskFreeGB      float64  `json:"disk_free_gb"`
	RAMMB           int      `json:"ram_mb"`
	HasSudo         bool     `json:"has_sudo"`
	HasDocker       bool     `json:"has_docker"`
	DockerRunning   bool     `json:"docker_running"`
	Port443Free     bool     `json:"port_443_free"`
	Port80Free      bool     `json:"port_80_free"`
	PortWGFree      bool     `json:"port_wg_free"`
	Compatible      bool     `json:"compatible"`
	Warnings        []string `json:"warnings"`
	BlockingFailures []string `json:"blocking_failures"`
}

type DomainCheckResult struct {
	Domain               string   `json:"domain"`
	Mode                 string   `json:"mode"`
	CheckedAt            string   `json:"checked_at"`
	ARecordIPs           []string `json:"a_record_ips"`
	PointsToVPS          bool     `json:"points_to_vps"`
	CloudflareDetected   bool     `json:"cloudflare_detected"`
	CloudflareTokenValid *bool    `json:"cloudflare_token_valid,omitempty"`
	CloudflareTokenScopes []string `json:"cloudflare_token_scopes,omitempty"`
	CloudflareZoneID     string   `json:"cloudflare_zone_id,omitempty"`
	TLSStrategy          string   `json:"tls_strategy"`
	TLSFeasible          bool     `json:"tls_feasible"`
	Warnings             []string `json:"warnings"`
	Errors               []string `json:"errors"`
}

type WizardState struct {
	SessionID        string            `json:"session_id"`
	CurrentStep      WizardStep        `json:"current_step"`
	Status           SessionStatus     `json:"status"`
	Inputs           WizardInputs      `json:"inputs"`
	PreflightResult  *PreflightResult  `json:"preflight_result,omitempty"`
	DomainCheckResult *DomainCheckResult `json:"domain_check_result,omitempty"`
	ProgressPct      int               `json:"progress_pct"`
	StartedAt        string            `json:"started_at"`
	LastSavedAt      string            `json:"last_saved_at"`
	CommittedAt      string            `json:"committed_at,omitempty"`
	ErrorMessage     string            `json:"error_message,omitempty"`
}

func NewState() *WizardState {
	now := time.Now().UTC().Format(time.RFC3339)
	return &WizardState{
		SessionID:   uuid.New().String(),
		CurrentStep: StepWelcome,
		Status:      StatusInProgress,
		ProgressPct: 0,
		StartedAt:   now,
		LastSavedAt: now,
	}
}

type WizardAction struct {
	Type     string      `json:"type"`
	Step     WizardStep  `json:"step,omitempty"`
	Data     interface{} `json:"data,omitempty"`
	Error    string      `json:"error,omitempty"`
}

const (
	ActionStartSession  = "start_session"
	ActionStepComplete  = "step_complete"
	ActionStepError     = "step_error"
	ActionPreflight     = "preflight_result"
	ActionDomainCheck   = "domain_check_result"
	ActionStepBack      = "step_back"
	ActionCommitStart   = "commit_start"
	ActionCommitSuccess = "commit_success"
	ActionCommitFailure = "commit_failure"
	ActionRetry         = "retry"
	ActionAbandon       = "abandon"
)

var stepProgress = map[WizardStep]int{
	StepWelcome:     0,
	StepSSH:         12,
	StepPreflight:   25,
	StepDomainMode:  37,
	StepDomainCheck: 50,
	StepCloudflare:  62,
	StepCreatePeer:  75,
	StepCommit:      87,
	StepSuccess:     100,
	StepError:       0,
}

type transition struct {
	From  WizardStep
	To    WizardStep
	Guard func(state *WizardState) bool
}

var transitions = []transition{
	{StepWelcome, StepSSH, func(s *WizardState) bool { return true }},
	{StepSSH, StepWelcome, func(s *WizardState) bool { return true }},
	{StepSSH, StepPreflight, func(s *WizardState) bool { return s.Inputs.SSH != nil }},
	{StepPreflight, StepSSH, func(s *WizardState) bool { return true }},
	{StepPreflight, StepDomainMode, func(s *WizardState) bool {
		return s.PreflightResult != nil && s.PreflightResult.Compatible
	}},
	{StepPreflight, StepSSH, func(s *WizardState) bool {
		return s.PreflightResult != nil && !s.PreflightResult.Compatible
	}},
	{StepDomainMode, StepPreflight, func(s *WizardState) bool { return true }},
	{StepDomainMode, StepDomainCheck, func(s *WizardState) bool {
		return s.Inputs.DomainMode == "byo"
	}},
	{StepDomainMode, StepCreatePeer, func(s *WizardState) bool {
		return s.Inputs.DomainMode == "nipio"
	}},
	{StepDomainCheck, StepDomainMode, func(s *WizardState) bool { return true }},
	{StepDomainCheck, StepCloudflare, func(s *WizardState) bool {
		return s.DomainCheckResult != nil && s.DomainCheckResult.CloudflareDetected && s.DomainCheckResult.TLSFeasible
	}},
	{StepDomainCheck, StepCreatePeer, func(s *WizardState) bool {
		return s.DomainCheckResult != nil && s.DomainCheckResult.TLSFeasible && (!s.DomainCheckResult.CloudflareDetected)
	}},
	{StepCloudflare, StepDomainCheck, func(s *WizardState) bool { return true }},
	{StepCloudflare, StepCreatePeer, func(s *WizardState) bool {
		return s.Inputs.CloudflareToken != ""
	}},
	{StepCreatePeer, StepCloudflare, func(s *WizardState) bool { return true }},
	{StepCreatePeer, StepDomainMode, func(s *WizardState) bool { return true }},
	{StepCreatePeer, StepCommit, func(s *WizardState) bool {
		return s.Inputs.FirstPeerName != ""
	}},
	{StepCommit, StepSuccess, func(s *WizardState) bool { return true }},
	{StepCommit, StepError, func(s *WizardState) bool { return true }},
	{StepError, StepSSH, func(s *WizardState) bool { return true }},
	{StepError, StepWelcome, func(s *WizardState) bool { return true }},
}

var backNavAllowed = map[WizardStep]bool{
	StepWelcome:     false,
	StepSSH:         true,
	StepPreflight:   true,
	StepDomainMode:  true,
	StepDomainCheck: true,
	StepCloudflare:  true,
	StepCreatePeer:  true,
	StepCommit:      false,
	StepSuccess:     false,
	StepError:       false,
}

type TransitionError struct {
	From WizardStep
	To   WizardStep
	Msg  string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("invalid transition %s → %s: %s", e.From, e.To, e.Msg)
}

func Reducer(state *WizardState, action WizardAction) (*WizardState, error) {
	if state == nil {
		return nil, fmt.Errorf("state is nil")
	}

	switch action.Type {
	case ActionStartSession:
		return handleStartSession(state, action)

	case ActionStepComplete:
		return handleStepComplete(state, action)

	case ActionStepBack:
		return handleStepBack(state, action)

	case ActionPreflight:
		return handlePreflightResult(state, action)

	case ActionDomainCheck:
		return handleDomainCheckResult(state, action)

	case ActionCommitStart:
		return handleCommitStart(state, action)

	case ActionCommitSuccess:
		return handleCommitSuccess(state, action)

	case ActionCommitFailure:
		return handleCommitFailure(state, action)

	case ActionRetry:
		return handleRetry(state, action)

	case ActionAbandon:
		return handleAbandon(state, action)

	default:
		return nil, fmt.Errorf("unknown action type: %s", action.Type)
	}
}

func handleStartSession(state *WizardState, _ WizardAction) (*WizardState, error) {
	if state.Status != "" && state.Status != StatusInProgress {
		return nil, &TransitionError{From: state.CurrentStep, To: StepWelcome, Msg: "session already exists"}
	}
	state.CurrentStep = StepWelcome
	state.Status = StatusInProgress
	state.ProgressPct = stepProgress[StepWelcome]
	return state, nil
}

func handleStepComplete(state *WizardState, action WizardAction) (*WizardState, error) {
	targetStep := action.Step
	if !IsValidStep(targetStep) {
		return nil, fmt.Errorf("invalid step: %s", targetStep)
	}

	if !isTransitionAllowed(state.CurrentStep, targetStep, state) {
		return nil, &TransitionError{
			From: state.CurrentStep,
			To:   targetStep,
			Msg:  "transition not allowed",
		}
	}

	state.CurrentStep = targetStep
	state.ProgressPct = stepProgress[targetStep]
	state.LastSavedAt = time.Now().UTC().Format(time.RFC3339)
	return state, nil
}

func handleStepBack(state *WizardState, action WizardAction) (*WizardState, error) {
	targetStep := action.Step

	if !backNavAllowed[state.CurrentStep] {
		return nil, &TransitionError{
			From: state.CurrentStep,
			To:   targetStep,
			Msg:  "back navigation not allowed from current step",
		}
	}

	if !IsValidStep(targetStep) {
		return nil, fmt.Errorf("invalid step: %s", targetStep)
	}

	state.CurrentStep = targetStep
	state.ProgressPct = stepProgress[targetStep]
	state.LastSavedAt = time.Now().UTC().Format(time.RFC3339)
	return state, nil
}

func handlePreflightResult(state *WizardState, action WizardAction) (*WizardState, error) {
	if state.CurrentStep != StepPreflight {
		return nil, &TransitionError{
			From: state.CurrentStep,
			To:   StepPreflight,
			Msg:  "preflight result can only be set during preflight step",
		}
	}

	result, ok := action.Data.(*PreflightResult)
	if !ok {
		return nil, fmt.Errorf("invalid preflight result data")
	}

	state.PreflightResult = result
	state.LastSavedAt = time.Now().UTC().Format(time.RFC3339)

	if result.Compatible {
		state.CurrentStep = StepDomainMode
		state.ProgressPct = stepProgress[StepDomainMode]
	}

	return state, nil
}

func handleDomainCheckResult(state *WizardState, action WizardAction) (*WizardState, error) {
	if state.CurrentStep != StepDomainCheck {
		return nil, &TransitionError{
			From: state.CurrentStep,
			To:   StepDomainCheck,
			Msg:  "domain check result can only be set during domain_check step",
		}
	}

	result, ok := action.Data.(*DomainCheckResult)
	if !ok {
		return nil, fmt.Errorf("invalid domain check result data")
	}

	state.DomainCheckResult = result
	state.LastSavedAt = time.Now().UTC().Format(time.RFC3339)

	if !result.TLSFeasible {
		return state, nil
	}

	if result.CloudflareDetected {
		state.CurrentStep = StepCloudflare
		state.ProgressPct = stepProgress[StepCloudflare]
	} else {
		state.CurrentStep = StepCreatePeer
		state.ProgressPct = stepProgress[StepCreatePeer]
	}

	return state, nil
}

func handleCommitStart(state *WizardState, _ WizardAction) (*WizardState, error) {
	if state.CurrentStep != StepCommit {
		return nil, &TransitionError{
			From: state.CurrentStep,
			To:   StepCommit,
			Msg:  "commit can only start from commit step",
		}
	}
	state.LastSavedAt = time.Now().UTC().Format(time.RFC3339)
	return state, nil
}

func handleCommitSuccess(state *WizardState, _ WizardAction) (*WizardState, error) {
	if state.CurrentStep != StepCommit {
		return nil, &TransitionError{
			From: state.CurrentStep,
			To:   StepSuccess,
			Msg:  "commit success can only happen from commit step",
		}
	}

	state.CurrentStep = StepSuccess
	state.Status = StatusCommitted
	state.ProgressPct = 100
	state.CommittedAt = time.Now().UTC().Format(time.RFC3339)
	state.LastSavedAt = state.CommittedAt
	return state, nil
}

func handleCommitFailure(state *WizardState, action WizardAction) (*WizardState, error) {
	if state.CurrentStep != StepCommit {
		return nil, &TransitionError{
			From: state.CurrentStep,
			To:   StepError,
			Msg:  "commit failure can only happen from commit step",
		}
	}

	state.CurrentStep = StepError
	state.ErrorMessage = action.Error
	state.LastSavedAt = time.Now().UTC().Format(time.RFC3339)
	return state, nil
}

func handleRetry(state *WizardState, _ WizardAction) (*WizardState, error) {
	if state.CurrentStep != StepError {
		return nil, &TransitionError{
			From: state.CurrentStep,
			To:   StepSSH,
			Msg:  "retry only allowed from error step",
		}
	}

	state.CurrentStep = StepSSH
	state.ErrorMessage = ""
	state.PreflightResult = nil
	state.DomainCheckResult = nil
	state.ProgressPct = stepProgress[StepSSH]
	state.LastSavedAt = time.Now().UTC().Format(time.RFC3339)
	return state, nil
}

func handleAbandon(state *WizardState, _ WizardAction) (*WizardState, error) {
	state.Status = StatusAbandoned
	state.LastSavedAt = time.Now().UTC().Format(time.RFC3339)
	return state, nil
}

func isTransitionAllowed(from, to WizardStep, state *WizardState) bool {
	for _, t := range transitions {
		if t.From == from && t.To == to {
			if t.Guard != nil {
				return t.Guard(state)
			}
			return true
		}
	}
	return false
}
