package auth

import "os"

// EnvVar is the standard environment variable Anthropic SDKs honor.
const EnvVar = "ANTHROPIC_API_KEY"

// FromEnv returns Credentials from the ANTHROPIC_API_KEY env var, or nil if unset.
func FromEnv() *Credentials {
	if v := os.Getenv(EnvVar); v != "" {
		return &Credentials{
			Kind:   KindAPIKey,
			APIKey: v,
			Source: MethodEnv,
		}
	}
	return nil
}
