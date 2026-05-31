package auth

// Scope represents a permission level for API access.
type Scope string

const (
	// ScopeRead allows read-only access (e.g., GET endpoints).
	ScopeRead Scope = "read"
	// ScopeWrite allows read and write access (e.g., GET, POST, DELETE).
	ScopeWrite Scope = "write"
	// ScopeAdmin allows full access, including token management.
	ScopeAdmin Scope = "admin"
)

// Allows returns true if the token's scope grants the required scope.
// The hierarchy is: admin ⊃ write ⊃ read.
func (s Scope) Allows(required Scope) bool {
	switch required {
	case ScopeRead:
		return s == ScopeAdmin || s == ScopeWrite || s == ScopeRead
	case ScopeWrite:
		return s == ScopeAdmin || s == ScopeWrite
	case ScopeAdmin:
		return s == ScopeAdmin
	default:
		return false
	}
}
