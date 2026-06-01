package wizard

import (
	"strings"
	"testing"
)

func newStateAt(step WizardStep) *WizardState {
	s := NewState()
	s.CurrentStep = step
	s.ProgressPct = stepProgress[step]
	return s
}

func withSSH(s *WizardState) *WizardState {
	s.Inputs.SSH = &SSHInput{Host: "1.2.3.4", Port: 22, User: "root", AuthType: "key"}
	return s
}

func withPreflight(s *WizardState, compatible bool) *WizardState {
	s.PreflightResult = &PreflightResult{Compatible: compatible}
	return s
}

func withDomainMode(s *WizardState, mode string) *WizardState {
	s.Inputs.DomainMode = mode
	return s
}

func withDomainCheck(s *WizardState, cloudflare, tlsFeasible bool) *WizardState {
	s.DomainCheckResult = &DomainCheckResult{CloudflareDetected: cloudflare, TLSFeasible: tlsFeasible}
	return s
}

func withCloudflareToken(s *WizardState) *WizardState {
	s.Inputs.CloudflareToken = "cf-test-token"
	return s
}

func withPeerName(s *WizardState) *WizardState {
	s.Inputs.FirstPeerName = "peer1"
	return s
}

func TestNewState(t *testing.T) {
	t.Parallel()

	s := NewState()

	if s.SessionID == "" {
		t.Fatal("SessionID must not be empty")
	}
	if len(strings.Split(s.SessionID, "-")) < 4 {
		t.Fatalf("SessionID should be a UUID, got: %s", s.SessionID)
	}
	if s.CurrentStep != StepWelcome {
		t.Fatalf("CurrentStep = %q, want %q", s.CurrentStep, StepWelcome)
	}
	if s.Status != StatusInProgress {
		t.Fatalf("Status = %q, want %q", s.Status, StatusInProgress)
	}
	if s.ProgressPct != 0 {
		t.Fatalf("ProgressPct = %d, want 0", s.ProgressPct)
	}
	if s.StartedAt == "" {
		t.Fatal("StartedAt must not be empty")
	}
	if s.LastSavedAt == "" {
		t.Fatal("LastSavedAt must not be empty")
	}
}

func TestValidForwardTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setup       func() *WizardState
		action      WizardAction
		wantStep    WizardStep
		wantPct     int
		wantErr     bool
	}{
		{
			name:   "welcome_to_ssh",
			setup:  func() *WizardState { return newStateAt(StepWelcome) },
			action: WizardAction{Type: ActionStepComplete, Step: StepSSH},
			wantStep: StepSSH,
			wantPct:  12,
		},
		{
			name:   "ssh_to_preflight_requires_ssh",
			setup:  func() *WizardState { return withSSH(newStateAt(StepSSH)) },
			action: WizardAction{Type: ActionStepComplete, Step: StepPreflight},
			wantStep: StepPreflight,
			wantPct:  25,
		},
		{
			name:   "ssh_to_preflight_no_ssh_input_fails",
			setup:  func() *WizardState { return newStateAt(StepSSH) },
			action: WizardAction{Type: ActionStepComplete, Step: StepPreflight},
			wantErr: true,
		},
		{
			name:   "preflight_to_domain_mode_compatible",
			setup:  func() *WizardState { return withPreflight(newStateAt(StepPreflight), true) },
			action: WizardAction{Type: ActionPreflight, Data: &PreflightResult{Compatible: true}},
			wantStep: StepDomainMode,
			wantPct:  37,
		},
		{
			name:   "domain_mode_byo_to_domain_check",
			setup:  func() *WizardState { return withDomainMode(newStateAt(StepDomainMode), "byo") },
			action: WizardAction{Type: ActionStepComplete, Step: StepDomainCheck},
			wantStep: StepDomainCheck,
			wantPct:  50,
		},
		{
			name:   "domain_mode_nipio_to_create_peer",
			setup:  func() *WizardState { return withDomainMode(newStateAt(StepDomainMode), "nipio") },
			action: WizardAction{Type: ActionStepComplete, Step: StepCreatePeer},
			wantStep: StepCreatePeer,
			wantPct:  75,
		},
		{
			name: "domain_check_cloudflare_to_cloudflare_step",
			setup: func() *WizardState {
				return withDomainCheck(newStateAt(StepDomainCheck), true, true)
			},
			action:   WizardAction{Type: ActionDomainCheck, Data: &DomainCheckResult{CloudflareDetected: true, TLSFeasible: true}},
			wantStep: StepCloudflare,
			wantPct:  62,
		},
		{
			name: "domain_check_no_cloudflare_to_create_peer",
			setup: func() *WizardState {
				return withDomainCheck(newStateAt(StepDomainCheck), false, true)
			},
			action:   WizardAction{Type: ActionDomainCheck, Data: &DomainCheckResult{CloudflareDetected: false, TLSFeasible: true}},
			wantStep: StepCreatePeer,
			wantPct:  75,
		},
		{
			name:   "cloudflare_to_create_peer_with_token",
			setup:  func() *WizardState { return withCloudflareToken(newStateAt(StepCloudflare)) },
			action: WizardAction{Type: ActionStepComplete, Step: StepCreatePeer},
			wantStep: StepCreatePeer,
			wantPct:  75,
		},
		{
			name:   "cloudflare_to_create_peer_no_token_fails",
			setup:  func() *WizardState { return newStateAt(StepCloudflare) },
			action: WizardAction{Type: ActionStepComplete, Step: StepCreatePeer},
			wantErr: true,
		},
		{
			name:   "create_peer_to_commit_with_peer_name",
			setup:  func() *WizardState { return withPeerName(newStateAt(StepCreatePeer)) },
			action: WizardAction{Type: ActionStepComplete, Step: StepCommit},
			wantStep: StepCommit,
			wantPct:  87,
		},
		{
			name:   "create_peer_to_commit_no_peer_name_fails",
			setup:  func() *WizardState { return newStateAt(StepCreatePeer) },
			action: WizardAction{Type: ActionStepComplete, Step: StepCommit},
			wantErr: true,
		},
		{
			name:   "commit_to_success",
			setup:  func() *WizardState { return newStateAt(StepCommit) },
			action: WizardAction{Type: ActionCommitSuccess},
			wantStep: StepSuccess,
			wantPct:  100,
		},
		{
			name:   "commit_to_error",
			setup:  func() *WizardState { return newStateAt(StepCommit) },
			action: WizardAction{Type: ActionCommitFailure, Error: "disk full"},
			wantStep: StepError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state := tt.setup()
			got, err := Reducer(state, tt.action)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.CurrentStep != tt.wantStep {
				t.Errorf("CurrentStep = %q, want %q", got.CurrentStep, tt.wantStep)
			}
			if tt.wantPct != 0 && got.ProgressPct != tt.wantPct {
				t.Errorf("ProgressPct = %d, want %d", got.ProgressPct, tt.wantPct)
			}
		})
	}
}

func TestInvalidTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		setup  func() *WizardState
		action WizardAction
	}{
		{
			name:   "ssh_before_welcome",
			setup:  func() *WizardState { return newStateAt(StepWelcome) },
			action: WizardAction{Type: ActionStepComplete, Step: StepPreflight},
		},
		{
			name:   "commit_before_create_peer",
			setup:  func() *WizardState { return newStateAt(StepDomainMode) },
			action: WizardAction{Type: ActionStepComplete, Step: StepCommit},
		},
		{
			name:   "preflight_to_domain_mode_not_compatible",
			setup:  func() *WizardState { return withPreflight(newStateAt(StepPreflight), false) },
			action: WizardAction{Type: ActionStepComplete, Step: StepDomainMode},
		},
		{
			name:   "skip_welcome_to_commit",
			setup:  func() *WizardState { return newStateAt(StepWelcome) },
			action: WizardAction{Type: ActionStepComplete, Step: StepCommit},
		},
		{
			name:   "domain_mode_byo_to_create_peer_wrong",
			setup:  func() *WizardState { return withDomainMode(newStateAt(StepDomainMode), "byo") },
			action: WizardAction{Type: ActionStepComplete, Step: StepCreatePeer},
		},
		{
			name:   "domain_mode_nipio_to_domain_check_wrong",
			setup:  func() *WizardState { return withDomainMode(newStateAt(StepDomainMode), "nipio") },
			action: WizardAction{Type: ActionStepComplete, Step: StepDomainCheck},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state := tt.setup()
			_, err := Reducer(state, tt.action)
			if err == nil {
				t.Fatalf("expected error for invalid transition, got nil")
			}
		})
	}
}

func TestBackNavigation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setup    func() *WizardState
		target   WizardStep
		wantPct  int
	}{
		{
			name:    "ssh_to_welcome",
			setup:   func() *WizardState { return newStateAt(StepSSH) },
			target:  StepWelcome,
			wantPct: 0,
		},
		{
			name:    "preflight_to_ssh",
			setup:   func() *WizardState { return newStateAt(StepPreflight) },
			target:  StepSSH,
			wantPct: 12,
		},
		{
			name:    "domain_mode_to_preflight",
			setup:   func() *WizardState { return newStateAt(StepDomainMode) },
			target:  StepPreflight,
			wantPct: 25,
		},
		{
			name:    "domain_check_to_domain_mode",
			setup:   func() *WizardState { return newStateAt(StepDomainCheck) },
			target:  StepDomainMode,
			wantPct: 37,
		},
		{
			name:    "cloudflare_to_domain_check",
			setup:   func() *WizardState { return newStateAt(StepCloudflare) },
			target:  StepDomainCheck,
			wantPct: 50,
		},
		{
			name:    "create_peer_to_cloudflare",
			setup:   func() *WizardState { return newStateAt(StepCreatePeer) },
			target:  StepCloudflare,
			wantPct: 62,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state := tt.setup()
			got, err := Reducer(state, WizardAction{Type: ActionStepBack, Step: tt.target})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.CurrentStep != tt.target {
				t.Errorf("CurrentStep = %q, want %q", got.CurrentStep, tt.target)
			}
			if got.ProgressPct != tt.wantPct {
				t.Errorf("ProgressPct = %d, want %d", got.ProgressPct, tt.wantPct)
			}
		})
	}
}

func TestBackNavigationBlocked(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func() *WizardState
	}{
		{
			name:  "commit_blocks_back",
			setup: func() *WizardState { return newStateAt(StepCommit) },
		},
		{
			name:  "success_blocks_back",
			setup: func() *WizardState { return newStateAt(StepSuccess) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state := tt.setup()
			_, err := Reducer(state, WizardAction{Type: ActionStepBack, Step: StepWelcome})
			if err == nil {
				t.Fatalf("expected error for back navigation from blocked step, got nil")
			}
			if te, ok := err.(*TransitionError); ok {
				if !strings.Contains(te.Msg, "back navigation not allowed") {
					t.Errorf("error message should mention back navigation not allowed, got: %s", te.Msg)
				}
			}
		})
	}
}

func TestErrorStateTransitions(t *testing.T) {
	t.Parallel()

	t.Run("error_retry_to_ssh", func(t *testing.T) {
		t.Parallel()
		s := newStateAt(StepError)
		s.ErrorMessage = "something broke"
		s.PreflightResult = &PreflightResult{}
		s.DomainCheckResult = &DomainCheckResult{}

		got, err := Reducer(s, WizardAction{Type: ActionRetry})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.CurrentStep != StepSSH {
			t.Errorf("CurrentStep = %q, want %q", got.CurrentStep, StepSSH)
		}
		if got.ErrorMessage != "" {
			t.Errorf("ErrorMessage should be cleared, got %q", got.ErrorMessage)
		}
		if got.PreflightResult != nil {
			t.Error("PreflightResult should be nil after retry")
		}
		if got.DomainCheckResult != nil {
			t.Error("DomainCheckResult should be nil after retry")
		}
		if got.ProgressPct != stepProgress[StepSSH] {
			t.Errorf("ProgressPct = %d, want %d", got.ProgressPct, stepProgress[StepSSH])
		}
	})

	t.Run("error_to_welcome_via_step_complete", func(t *testing.T) {
		t.Parallel()
		s := newStateAt(StepError)
		got, err := Reducer(s, WizardAction{Type: ActionStepComplete, Step: StepWelcome})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.CurrentStep != StepWelcome {
			t.Errorf("CurrentStep = %q, want %q", got.CurrentStep, StepWelcome)
		}
	})

	t.Run("retry_from_non_error_fails", func(t *testing.T) {
		t.Parallel()
		s := newStateAt(StepSSH)
		_, err := Reducer(s, WizardAction{Type: ActionRetry})
		if err == nil {
			t.Fatal("expected error for retry from non-error step")
		}
	})
}

func TestAbandon(t *testing.T) {
	t.Parallel()

	steps := []WizardStep{
		StepWelcome, StepSSH, StepPreflight, StepDomainMode,
		StepDomainCheck, StepCloudflare, StepCreatePeer,
		StepCommit, StepSuccess, StepError,
	}

	for _, step := range steps {
		t.Run("abandon_from_"+string(step), func(t *testing.T) {
			t.Parallel()
			s := newStateAt(step)
			got, err := Reducer(s, WizardAction{Type: ActionAbandon})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Status != StatusAbandoned {
				t.Errorf("Status = %q, want %q", got.Status, StatusAbandoned)
			}
			if got.CurrentStep != step {
				t.Errorf("CurrentStep should remain %q, got %q", step, got.CurrentStep)
			}
		})
	}
}

func TestIdempotency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		setup  func() *WizardState
		action WizardAction
	}{
		{
			name:   "step_complete_welcome_to_ssh_twice",
			setup:  func() *WizardState { return newStateAt(StepWelcome) },
			action: WizardAction{Type: ActionStepComplete, Step: StepSSH},
		},
		{
			name:   "commit_success_twice",
			setup:  func() *WizardState { return newStateAt(StepCommit) },
			action: WizardAction{Type: ActionCommitSuccess},
		},
		{
			name:   "abandon_twice",
			setup:  func() *WizardState { return newStateAt(StepSSH) },
			action: WizardAction{Type: ActionAbandon},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state := tt.setup()

			got1, err1 := Reducer(state, tt.action)
			if err1 != nil {
				t.Fatalf("first call error: %v", err1)
			}

			step1 := got1.CurrentStep
			pct1 := got1.ProgressPct

			got2, err2 := Reducer(got1, tt.action)
			if tt.action.Type == ActionStepComplete && tt.action.Step == StepSSH {
				if err2 == nil {
					t.Fatal("second step_complete from ssh should fail (no transition ssh→ssh)")
				}
				return
			}
			if tt.action.Type == ActionCommitSuccess {
				if err2 == nil {
					t.Fatal("second commit_success from success should fail (not at commit)")
				}
				return
			}
			if err2 != nil {
				t.Fatalf("second call error: %v", err2)
			}
			if got2.CurrentStep != step1 {
				t.Errorf("idempotency broken: step changed from %q to %q", step1, got2.CurrentStep)
			}
			if got2.ProgressPct != pct1 {
				t.Errorf("idempotency broken: pct changed from %d to %d", pct1, got2.ProgressPct)
			}
		})
	}
}

func TestCommitSuccessSetsCommitted(t *testing.T) {
	t.Parallel()

	s := newStateAt(StepCommit)
	got, err := Reducer(s, WizardAction{Type: ActionCommitSuccess})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != StatusCommitted {
		t.Errorf("Status = %q, want %q", got.Status, StatusCommitted)
	}
	if got.CommittedAt == "" {
		t.Error("CommittedAt must be set")
	}
	if got.CurrentStep != StepSuccess {
		t.Errorf("CurrentStep = %q, want %q", got.CurrentStep, StepSuccess)
	}
}

func TestCommitFailureSetsError(t *testing.T) {
	t.Parallel()

	s := newStateAt(StepCommit)
	got, err := Reducer(s, WizardAction{Type: ActionCommitFailure, Error: "disk full"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CurrentStep != StepError {
		t.Errorf("CurrentStep = %q, want %q", got.CurrentStep, StepError)
	}
	if got.ErrorMessage != "disk full" {
		t.Errorf("ErrorMessage = %q, want %q", got.ErrorMessage, "disk full")
	}
}

func TestPreflightResultNotCompatible(t *testing.T) {
	t.Parallel()

	s := newStateAt(StepPreflight)
	got, err := Reducer(s, WizardAction{Type: ActionPreflight, Data: &PreflightResult{Compatible: false, BlockingFailures: []string{"no docker"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CurrentStep != StepPreflight {
		t.Errorf("CurrentStep = %q, want %q (should stay on preflight)", got.CurrentStep, StepPreflight)
	}
	if got.PreflightResult == nil || got.PreflightResult.Compatible {
		t.Error("PreflightResult should be stored with Compatible=false")
	}
}

func TestDomainCheckNotTLSFeasible(t *testing.T) {
	t.Parallel()

	s := newStateAt(StepDomainCheck)
	got, err := Reducer(s, WizardAction{Type: ActionDomainCheck, Data: &DomainCheckResult{TLSFeasible: false}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CurrentStep != StepDomainCheck {
		t.Errorf("CurrentStep = %q, want %q (should stay on domain_check)", got.CurrentStep, StepDomainCheck)
	}
}

func TestNilState(t *testing.T) {
	t.Parallel()

	_, err := Reducer(nil, WizardAction{Type: ActionStepComplete, Step: StepSSH})
	if err == nil {
		t.Fatal("expected error for nil state")
	}
}

func TestUnknownActionType(t *testing.T) {
	t.Parallel()

	s := NewState()
	_, err := Reducer(s, WizardAction{Type: "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown action type")
	}
}

func TestPreflightResultWrongStep(t *testing.T) {
	t.Parallel()

	s := newStateAt(StepSSH)
	_, err := Reducer(s, WizardAction{Type: ActionPreflight, Data: &PreflightResult{Compatible: true}})
	if err == nil {
		t.Fatal("expected error for preflight result on wrong step")
	}
}

func TestDomainCheckResultWrongStep(t *testing.T) {
	t.Parallel()

	s := newStateAt(StepSSH)
	_, err := Reducer(s, WizardAction{Type: ActionDomainCheck, Data: &DomainCheckResult{TLSFeasible: true}})
	if err == nil {
		t.Fatal("expected error for domain check result on wrong step")
	}
}

func TestCommitStartWrongStep(t *testing.T) {
	t.Parallel()

	s := newStateAt(StepSSH)
	_, err := Reducer(s, WizardAction{Type: ActionCommitStart})
	if err == nil {
		t.Fatal("expected error for commit start on wrong step")
	}
}

func TestCommitSuccessWrongStep(t *testing.T) {
	t.Parallel()

	s := newStateAt(StepSSH)
	_, err := Reducer(s, WizardAction{Type: ActionCommitSuccess})
	if err == nil {
		t.Fatal("expected error for commit success on wrong step")
	}
}

func TestCommitFailureWrongStep(t *testing.T) {
	t.Parallel()

	s := newStateAt(StepSSH)
	_, err := Reducer(s, WizardAction{Type: ActionCommitFailure, Error: "fail"})
	if err == nil {
		t.Fatal("expected error for commit failure on wrong step")
	}
}

func TestPreflightResultBadData(t *testing.T) {
	t.Parallel()

	s := newStateAt(StepPreflight)
	_, err := Reducer(s, WizardAction{Type: ActionPreflight, Data: "not a preflight result"})
	if err == nil {
		t.Fatal("expected error for bad preflight result data")
	}
}

func TestDomainCheckResultBadData(t *testing.T) {
	t.Parallel()

	s := newStateAt(StepDomainCheck)
	_, err := Reducer(s, WizardAction{Type: ActionDomainCheck, Data: 42})
	if err == nil {
		t.Fatal("expected error for bad domain check result data")
	}
}

func TestInvalidStepInStepComplete(t *testing.T) {
	t.Parallel()

	s := newStateAt(StepWelcome)
	_, err := Reducer(s, WizardAction{Type: ActionStepComplete, Step: WizardStep("nonexistent")})
	if err == nil {
		t.Fatal("expected error for invalid step")
	}
}

func TestInvalidStepInStepBack(t *testing.T) {
	t.Parallel()

	s := newStateAt(StepSSH)
	_, err := Reducer(s, WizardAction{Type: ActionStepBack, Step: WizardStep("nonexistent")})
	if err == nil {
		t.Fatal("expected error for invalid step in step_back")
	}
}

func TestCreatePeerBackToDomainMode(t *testing.T) {
	t.Parallel()

	s := newStateAt(StepCreatePeer)
	got, err := Reducer(s, WizardAction{Type: ActionStepBack, Step: StepDomainMode})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CurrentStep != StepDomainMode {
		t.Errorf("CurrentStep = %q, want %q", got.CurrentStep, StepDomainMode)
	}
}
