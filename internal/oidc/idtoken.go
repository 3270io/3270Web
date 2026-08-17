// SPDX-License-Identifier: AGPL-3.0-or-later

package oidc

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"strings"
	"time"
)

// clockSkew is how far out of step this server and the provider may be before
// a token is refused. Two minutes is enough for machines that are roughly in
// sync and short enough that an expired token is not usable for long.
const clockSkew = 2 * time.Minute

// IDToken is a verified ID token.
//
// A value of this type only exists because VerifyIDToken made one, so holding
// it is the proof that the signature and the claims were checked. Claims keeps
// everything the provider sent, because which claim carries a username or a
// group list differs between providers and is configuration rather than code.
type IDToken struct {
	Issuer   string
	Subject  string
	Audience []string
	Expiry   time.Time
	IssuedAt time.Time
	Nonce    string
	Claims   map[string]any
	// Raw is the encoded token, kept for the provider's end-session endpoint,
	// which asks for it back as a hint.
	Raw string
}

// Claim returns a string claim, or "".
//
// Numbers and booleans are rendered rather than refused: a provider that
// numbers its users still has a usable subject, and refusing would be a
// sign-in failure over a JSON type.
func (t *IDToken) Claim(name string) string {
	if t == nil || name == "" {
		return ""
	}
	switch v := t.Claims[name].(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%v", v)
	case bool:
		return fmt.Sprintf("%t", v)
	default:
		return ""
	}
}

// ClaimList returns a claim that names a set — groups, roles, entitlements.
//
// Providers disagree about the shape: an array of strings, or one
// space-separated string. Both are read, because an operator naming a claim
// should not have to know which their provider chose.
func (t *IDToken) ClaimList(name string) []string {
	if t == nil || name == "" {
		return nil
	}
	switch v := t.Claims[name].(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case string:
		return strings.Fields(v)
	default:
		return nil
	}
}

// jwtHeader is the part of a token that says how it was signed.
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

// VerifyIDToken checks a token's signature and claims and returns what it says.
//
// Everything here is a refusal that matters:
//
//   - "none" and symmetric algorithms are rejected before a key is looked up,
//     so a token can never talk this client out of checking its signature.
//   - The issuer must be the one configured, not the one in the token.
//   - The audience must contain this client, or somebody else's token for the
//     same provider would sign them in here.
//   - The nonce must match the one this server put in the request, which is
//     what stops a token captured elsewhere from being replayed into a login.
func (p *Provider) VerifyIDToken(ctx context.Context, raw, nonce string) (*IDToken, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, errors.New("oidc: the ID token is not a JWT")
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("oidc: ID token header: %w", err)
	}
	var header jwtHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("oidc: ID token header: %w", err)
	}
	if header.Typ != "" && !strings.EqualFold(header.Typ, "JWT") {
		return nil, fmt.Errorf("oidc: ID token is typed %q", header.Typ)
	}

	key, err := p.signingKey(ctx, header.Kid)
	if err != nil {
		return nil, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("oidc: ID token signature: %w", err)
	}
	if err := verifySignature(header.Alg, key, parts[0]+"."+parts[1], signature); err != nil {
		return nil, err
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("oidc: ID token payload: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("oidc: ID token payload: %w", err)
	}

	token := &IDToken{Claims: claims, Raw: raw}
	token.Issuer, _ = claims["iss"].(string)
	token.Subject, _ = claims["sub"].(string)
	token.Nonce, _ = claims["nonce"].(string)
	token.Audience = audienceOf(claims["aud"])
	token.Expiry = timeClaim(claims["exp"])
	token.IssuedAt = timeClaim(claims["iat"])

	if strings.TrimRight(token.Issuer, "/") != p.cfg.Issuer {
		return nil, fmt.Errorf("oidc: ID token is from issuer %q, not %q", token.Issuer, p.cfg.Issuer)
	}
	if token.Subject == "" {
		return nil, errors.New("oidc: ID token has no subject")
	}
	if !containsString(token.Audience, p.cfg.ClientID) {
		return nil, errors.New("oidc: ID token was not issued for this client")
	}
	// azp names which client a multi-audience token was actually for. Ignoring
	// it would let a token minted for another client, but audienced to include
	// this one, sign somebody in here.
	if azp, ok := claims["azp"].(string); ok && azp != "" && azp != p.cfg.ClientID {
		return nil, errors.New("oidc: ID token was authorized for a different client")
	}
	now := time.Now()
	if token.Expiry.IsZero() || now.After(token.Expiry.Add(clockSkew)) {
		return nil, errors.New("oidc: the ID token has expired")
	}
	if !token.IssuedAt.IsZero() && token.IssuedAt.After(now.Add(clockSkew)) {
		return nil, errors.New("oidc: the ID token was issued in the future")
	}
	// Compared in constant time and required in both directions: a token with
	// no nonce must not pass a login that sent one.
	if subtle.ConstantTimeCompare([]byte(token.Nonce), []byte(nonce)) != 1 {
		return nil, errors.New("oidc: the ID token does not answer this sign-in")
	}

	return token, nil
}

// verifySignature checks sig over signed with key, for the named algorithm.
func verifySignature(alg string, key crypto.PublicKey, signed string, sig []byte) error {
	var hashed []byte
	var cryptoHash crypto.Hash
	switch alg {
	case "RS256", "PS256", "ES256":
		cryptoHash = crypto.SHA256
		hashed = sum(sha256.New(), signed)
	case "RS384", "PS384", "ES384":
		cryptoHash = crypto.SHA384
		hashed = sum(sha512.New384(), signed)
	case "RS512", "PS512", "ES512":
		cryptoHash = crypto.SHA512
		hashed = sum(sha512.New(), signed)
	case "none", "":
		// The unsigned-token attack, named so the log says what was attempted.
		return errors.New("oidc: the ID token is unsigned")
	case "HS256", "HS384", "HS512":
		return errors.New("oidc: symmetric ID token signatures are not accepted")
	default:
		return fmt.Errorf("oidc: unsupported ID token algorithm %q", alg)
	}

	switch k := key.(type) {
	case *rsa.PublicKey:
		if strings.HasPrefix(alg, "PS") {
			if err := rsa.VerifyPSS(k, cryptoHash, hashed, sig, nil); err != nil {
				return errors.New("oidc: the ID token signature does not verify")
			}
			return nil
		}
		if !strings.HasPrefix(alg, "RS") {
			return fmt.Errorf("oidc: algorithm %q does not match an RSA key", alg)
		}
		if err := rsa.VerifyPKCS1v15(k, cryptoHash, hashed, sig); err != nil {
			return errors.New("oidc: the ID token signature does not verify")
		}
		return nil

	case *ecdsa.PublicKey:
		if !strings.HasPrefix(alg, "ES") {
			return fmt.Errorf("oidc: algorithm %q does not match an EC key", alg)
		}
		// A JWS ECDSA signature is r and s, each padded to the curve's byte
		// size — not the ASN.1 form ecdsa.VerifyASN1 reads.
		size := (k.Curve.Params().BitSize + 7) / 8
		if len(sig) != 2*size {
			return errors.New("oidc: the ID token signature is the wrong length for its curve")
		}
		r := new(big.Int).SetBytes(sig[:size])
		s := new(big.Int).SetBytes(sig[size:])
		if !ecdsa.Verify(k, hashed, r, s) {
			return errors.New("oidc: the ID token signature does not verify")
		}
		return nil

	default:
		return errors.New("oidc: unsupported signing key type")
	}
}

func sum(h hash.Hash, s string) []byte {
	h.Write([]byte(s))
	return h.Sum(nil)
}

// audienceOf reads the "aud" claim, which is a string or an array of them.
func audienceOf(v any) []string {
	switch aud := v.(type) {
	case string:
		return []string{aud}
	case []any:
		out := make([]string, 0, len(aud))
		for _, item := range aud {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// timeClaim reads a numeric date. A zero time means the claim was absent or
// unreadable, which every caller here treats as a failure.
func timeClaim(v any) time.Time {
	switch n := v.(type) {
	case float64:
		return time.Unix(int64(n), 0)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return time.Unix(i, 0)
		}
	}
	return time.Time{}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
