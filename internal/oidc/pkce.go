package oidc

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// RandomString returns a URL-safe random value of n bytes' entropy, for the
// state, nonce and PKCE verifier.
//
// It returns an error rather than falling back to something weaker. Each of
// these values is the whole of a defence — state against a login being
// initiated by somebody else, nonce against a replayed ID token, the verifier
// against a stolen code being redeemed — and a guessable one is worse than a
// sign-in that fails and says so.
func RandomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("oidc: generate random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CodeChallenge returns the S256 challenge for a PKCE verifier.
//
// S256 rather than "plain": a plain challenge is the verifier, so anything
// that saw the authorization request can redeem the code it produced, which is
// the attack PKCE exists to stop.
func CodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
