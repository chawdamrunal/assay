package auth

import (
	"errors"

	"github.com/chawdamrunal/assay/internal/store"
)

// FromAssayKeychain reads the API key Assay stores under its own service.
// Returns nil if no key is stored (not an error — just a miss).
func FromAssayKeychain(service string) *Credentials {
	kr := store.NewKeyring(service)
	v, err := kr.GetAPIKey()
	if err != nil {
		if errors.Is(err, store.ErrAPIKeyNotSet) {
			return nil
		}
		// Keychain access denied or other system error — silently skip,
		// the resolver will try other sources. Assay-managed keychain
		// is one of several options.
		return nil
	}
	return &Credentials{
		Kind:   KindAPIKey,
		APIKey: v,
		Source: MethodAssayKey,
	}
}
