package auth

import "testing"

func TestScopeAllows(t *testing.T) {
	tests := []struct {
		name     string
		token    Scope
		required Scope
		want     bool
	}{
		{"admin allows admin", ScopeAdmin, ScopeAdmin, true},
		{"admin allows write", ScopeAdmin, ScopeWrite, true},
		{"admin allows read", ScopeAdmin, ScopeRead, true},
		{"write rejects admin", ScopeWrite, ScopeAdmin, false},
		{"write allows write", ScopeWrite, ScopeWrite, true},
		{"write allows read", ScopeWrite, ScopeRead, true},
		{"read rejects admin", ScopeRead, ScopeAdmin, false},
		{"read rejects write", ScopeRead, ScopeWrite, false},
		{"read allows read", ScopeRead, ScopeRead, true},
		{"unknown token scope rejects read", Scope("unknown"), ScopeRead, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.Allows(tt.required); got != tt.want {
				t.Errorf("%q.Allows(%q) = %v; want %v", tt.token, tt.required, got, tt.want)
			}
		})
	}
}
