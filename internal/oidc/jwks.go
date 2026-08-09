package oidc

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// minKeyRefetchInterval bounds how often an unknown key ID causes a fetch.
//
// Providers rotate keys, so an unknown ID is a normal event and has to trigger
// a refetch. Without a floor, a stream of tokens naming keys that do not exist
// would be a way to make this server hammer the provider on demand.
const minKeyRefetchInterval = time.Minute

// jwk is one key from the provider's JWKS document.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	// RSA
	N string `json:"n"`
	E string `json:"e"`
	// EC
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

// keySet is the provider's signing keys, as fetched.
type keySet struct {
	keys      map[string]crypto.PublicKey
	fetchedAt time.Time
}

// publicKey converts one JWK into a key this client can verify with.
//
// Only RSA and EC are handled. A symmetric key would mean the client secret is
// the signing key, which makes the signature prove nothing that the token
// request had not already established, and octet keys are refused rather than
// silently accepted as a weaker check.
func (k jwk) publicKey() (crypto.PublicKey, error) {
	switch k.Kty {
	case "RSA":
		n, err := base64URLToInt(k.N)
		if err != nil {
			return nil, fmt.Errorf("modulus: %w", err)
		}
		e, err := base64URLToInt(k.E)
		if err != nil {
			return nil, fmt.Errorf("exponent: %w", err)
		}
		if !e.IsInt64() || e.Int64() < 3 {
			return nil, errors.New("implausible RSA exponent")
		}
		if n.BitLen() < 2048 {
			return nil, fmt.Errorf("RSA key is %d bits, below the 2048-bit floor", n.BitLen())
		}
		return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil

	case "EC":
		var curve elliptic.Curve
		switch k.Crv {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return nil, fmt.Errorf("unsupported curve %q", k.Crv)
		}
		x, err := base64URLToInt(k.X)
		if err != nil {
			return nil, fmt.Errorf("x coordinate: %w", err)
		}
		y, err := base64URLToInt(k.Y)
		if err != nil {
			return nil, fmt.Errorf("y coordinate: %w", err)
		}
		if !curve.IsOnCurve(x, y) {
			return nil, errors.New("point is not on the curve")
		}
		return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil

	default:
		return nil, fmt.Errorf("unsupported key type %q", k.Kty)
	}
}

func base64URLToInt(s string) (*big.Int, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, errors.New("empty value")
	}
	return new(big.Int).SetBytes(b), nil
}

// signingKey returns the key named by kid, fetching the key set if this is the
// first token or if kid is one the cached set does not have.
//
// An empty kid is served only when the provider publishes exactly one key.
// Guessing which of several keys a token was signed with, by trying each until
// one verifies, would mean a key published for another purpose could validate
// an ID token.
func (p *Provider) signingKey(ctx context.Context, kid string) (crypto.PublicKey, error) {
	p.mu.Lock()
	set := p.keys
	p.mu.Unlock()

	if key, ok := lookupKey(set, kid); ok {
		return key, nil
	}
	if set != nil && time.Since(set.fetchedAt) < minKeyRefetchInterval {
		return nil, fmt.Errorf("oidc: no signing key %q, and the key set was read moments ago", kid)
	}

	fetched, err := p.fetchKeys(ctx)
	if err != nil {
		return nil, err
	}
	if key, ok := lookupKey(fetched, kid); ok {
		return key, nil
	}
	return nil, fmt.Errorf("oidc: the provider publishes no signing key %q", kid)
}

func lookupKey(set *keySet, kid string) (crypto.PublicKey, bool) {
	if set == nil || len(set.keys) == 0 {
		return nil, false
	}
	if kid != "" {
		key, ok := set.keys[kid]
		return key, ok
	}
	if len(set.keys) != 1 {
		return nil, false
	}
	for _, key := range set.keys {
		return key, true
	}
	return nil, false
}

// fetchKeys reads the JWKS document and caches what it can use.
func (p *Provider) fetchKeys(ctx context.Context) (*keySet, error) {
	meta, err := p.Metadata(ctx)
	if err != nil {
		return nil, err
	}
	var doc jwksDocument
	if err := p.getJSON(ctx, meta.JWKSURI, &doc); err != nil {
		return nil, fmt.Errorf("oidc: read signing keys: %w", err)
	}

	set := &keySet{keys: make(map[string]crypto.PublicKey, len(doc.Keys)), fetchedAt: time.Now()}
	for _, k := range doc.Keys {
		// "enc" keys are for encryption, not signatures. Loading one would put
		// a key in the set that no ID token should ever be verified against.
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		key, err := k.publicKey()
		if err != nil {
			// One unusable key does not spoil the set: providers publish keys
			// for algorithms a given client does not implement.
			continue
		}
		set.keys[k.Kid] = key
	}
	if len(set.keys) == 0 {
		return nil, errors.New("oidc: the provider published no usable signing keys")
	}

	p.mu.Lock()
	p.keys = set
	p.mu.Unlock()
	return set, nil
}
