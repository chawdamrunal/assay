package auth

// Resolve runs each method in priority order: env → assay keychain → claude code.
// Returns the first hit, or ErrNoCredentials if all are empty/expired.
//
// assayKeychainService is the keychain service name to look up Assay-managed keys
// (production uses "assay"; tests use "assay-test").
func Resolve(assayKeychainService string) (*Credentials, error) {
	if c := FromEnv(); c != nil {
		return c, nil
	}
	if c := FromAssayKeychain(assayKeychainService); c != nil {
		return c, nil
	}
	if c := FromClaudeCode(); c != nil {
		return c, nil
	}
	return nil, ErrNoCredentials
}

// MethodStatus describes the outcome of attempting a single auth method.
// Used by ResolveAll to power `assay auth status`.
type MethodStatus struct {
	Method    Method
	Available bool
	Detail    string // optional context, e.g., "expired", "x-api-key", "bearer"
}

// ResolveAll returns the first available credential and a list of all methods
// that were attempted with their status — useful for `assay auth status`.
func ResolveAll(assayKeychainService string) (*Credentials, []MethodStatus) {
	var statuses []MethodStatus
	var winner *Credentials

	if c := FromEnv(); c != nil {
		statuses = append(statuses, MethodStatus{Method: MethodEnv, Available: true, Detail: "api-key from ANTHROPIC_API_KEY"})
		if winner == nil {
			winner = c
		}
	} else {
		statuses = append(statuses, MethodStatus{Method: MethodEnv, Available: false, Detail: "ANTHROPIC_API_KEY not set"})
	}

	if c := FromAssayKeychain(assayKeychainService); c != nil {
		statuses = append(statuses, MethodStatus{Method: MethodAssayKey, Available: true, Detail: "api-key from assay keychain"})
		if winner == nil {
			winner = c
		}
	} else {
		statuses = append(statuses, MethodStatus{Method: MethodAssayKey, Available: false, Detail: "no assay-stored key (run `assay config set api-key`)"})
	}

	if c := FromClaudeCode(); c != nil {
		detail := "bearer from Claude Code"
		if c.Subscription != "" {
			detail += " (subscription: " + c.Subscription + ")"
		}
		statuses = append(statuses, MethodStatus{Method: MethodClaudeCode, Available: true, Detail: detail})
		if winner == nil {
			winner = c
		}
	} else {
		statuses = append(statuses, MethodStatus{Method: MethodClaudeCode, Available: false, Detail: "Claude Code not installed, expired, or access denied"})
	}

	return winner, statuses
}
