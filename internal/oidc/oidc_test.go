package oidc

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// stubIDP is an identity provider that does what a real one does and can be
// asked to do what a hostile one would.
type stubIDP struct {
	server *httptest.Server
	rsaKey *rsa.PrivateKey
	ecKey  *ecdsa.PrivateKey
	// Overridable so a test can make the provider misbehave.
	issuerOverride string
	// lastForm records what the client posted to the token endpoint.
	lastForm url.Values
	// idToken is what the token endpoint hands back. Built by the test.
	idToken string
	// tokenError makes the token endpoint refuse.
	tokenError string
}

func newStubIDP(t *testing.T) *stubIDP {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	idp := &stubIDP{rsaKey: rsaKey, ecKey: ecKey}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		issuer := idp.issuer()
		if idp.issuerOverride != "" {
			issuer = idp.issuerOverride
		}
		writeJSON(w, map[string]any{
			"issuer":                 issuer,
			"authorization_endpoint": idp.issuer() + "/authorize",
			"token_endpoint":         idp.issuer() + "/token",
			"jwks_uri":               idp.issuer() + "/keys",
			"end_session_endpoint":   idp.issuer() + "/logout",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"keys": []any{
			map[string]any{
				"kty": "RSA", "kid": "rsa-1", "use": "sig", "alg": "RS256",
				"n": b64(rsaKey.N.Bytes()),
				"e": b64(big.NewInt(int64(rsaKey.E)).Bytes()),
			},
			map[string]any{
				"kty": "EC", "kid": "ec-1", "use": "sig", "alg": "ES256", "crv": "P-256",
				"x": b64(ecKey.X.Bytes()), "y": b64(ecKey.Y.Bytes()),
			},
		}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		idp.lastForm = r.PostForm
		if idp.tokenError != "" {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{"error": idp.tokenError, "error_description": "refused by the test"})
			return
		}
		writeJSON(w, map[string]any{"id_token": idp.idToken, "token_type": "Bearer"})
	})

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (i *stubIDP) issuer() string { return i.server.URL }

// sign builds an ID token. alg picks the key; claims are whatever the test
// wants, including nonsense.
func (i *stubIDP) sign(t *testing.T, alg, kid string, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": alg, "typ": "JWT"}
	if kid != "" {
		header["kid"] = kid
	}
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)
	signing := b64(headerJSON) + "." + b64(claimsJSON)

	var sig []byte
	sum := sha256.Sum256([]byte(signing))
	switch alg {
	case "RS256":
		var err error
		sig, err = rsa.SignPKCS1v15(rand.Reader, i.rsaKey, crypto.SHA256, sum[:])
		if err != nil {
			t.Fatal(err)
		}
	case "ES256":
		r, s, err := ecdsa.Sign(rand.Reader, i.ecKey, sum[:])
		if err != nil {
			t.Fatal(err)
		}
		sig = append(leftPad(r.Bytes(), 32), leftPad(s.Bytes(), 32)...)
	case "none":
		sig = nil
	default:
		t.Fatalf("stub cannot sign %q", alg)
	}
	return signing + "." + b64(sig)
}

func leftPad(b []byte, size int) []byte {
	if len(b) >= size {
		return b
	}
	out := make([]byte, size)
	copy(out[size-len(b):], b)
	return out
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

const (
	testClientID = "3270web-test"
	testNonce    = "nonce-value"
)

func newTestProvider(t *testing.T, idp *stubIDP) *Provider {
	t.Helper()
	p, err := New(Config{
		Issuer:       idp.issuer(),
		ClientID:     testClientID,
		ClientSecret: "shhh",
		RedirectURL:  "https://3270web.example/auth/callback",
		Scopes:       []string{"profile", "email"},
		HTTPClient:   idp.server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// goodClaims is a token that should be accepted, before a test spoils one field.
func goodClaims(idp *stubIDP) map[string]any {
	return map[string]any{
		"iss":                idp.issuer(),
		"sub":                "subject-123",
		"aud":                testClientID,
		"exp":                float64(time.Now().Add(time.Hour).Unix()),
		"iat":                float64(time.Now().Unix()),
		"nonce":              testNonce,
		"preferred_username": "alice",
		"groups":             []any{"mainframe-ops", "everyone"},
	}
}

func TestSignInReadsTheIdentityAndItsGroups(t *testing.T) {
	idp := newStubIDP(t)
	p := newTestProvider(t, idp)
	idp.idToken = idp.sign(t, "RS256", "rsa-1", goodClaims(idp))

	token, err := p.Exchange(context.Background(), "the-code", "the-verifier", testNonce)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if token.Subject != "subject-123" {
		t.Errorf("Subject = %q", token.Subject)
	}
	if got := token.Claim("preferred_username"); got != "alice" {
		t.Errorf("username claim = %q", got)
	}
	if got := token.ClaimList("groups"); len(got) != 2 || got[0] != "mainframe-ops" {
		t.Errorf("groups = %v", got)
	}

	// PKCE, not just a redirect: the verifier has to reach the token endpoint,
	// or the code alone would be enough for whoever intercepted it.
	if got := idp.lastForm.Get("code_verifier"); got != "the-verifier" {
		t.Errorf("code_verifier sent as %q", got)
	}
	if got := idp.lastForm.Get("grant_type"); got != "authorization_code" {
		t.Errorf("grant_type = %q", got)
	}
}

func TestSignInAcceptsAnECDSASignature(t *testing.T) {
	idp := newStubIDP(t)
	p := newTestProvider(t, idp)
	idp.idToken = idp.sign(t, "ES256", "ec-1", goodClaims(idp))

	if _, err := p.Exchange(context.Background(), "code", "verifier", testNonce); err != nil {
		t.Fatalf("an ES256 token was refused: %v", err)
	}
}

// Each of these is a token that must not sign anybody in. They are the reason
// this package exists rather than a call to the token endpoint and a JSON
// decode of whatever came back.
func TestTokensThatMustBeRefused(t *testing.T) {
	cases := []struct {
		name  string
		alg   string
		kid   string
		spoil func(map[string]any)
		want  string
	}{
		{
			name: "unsigned",
			alg:  "none", kid: "rsa-1",
			want: "unsigned",
		},
		{
			name: "for another audience",
			alg:  "RS256", kid: "rsa-1",
			spoil: func(c map[string]any) { c["aud"] = "some-other-client" },
			want:  "not issued for this client",
		},
		{
			name: "authorized for another client",
			alg:  "RS256", kid: "rsa-1",
			spoil: func(c map[string]any) { c["azp"] = "some-other-client" },
			want:  "authorized for a different client",
		},
		{
			name: "expired",
			alg:  "RS256", kid: "rsa-1",
			spoil: func(c map[string]any) { c["exp"] = float64(time.Now().Add(-time.Hour).Unix()) },
			want:  "expired",
		},
		{
			name: "no expiry at all",
			alg:  "RS256", kid: "rsa-1",
			spoil: func(c map[string]any) { delete(c, "exp") },
			want:  "expired",
		},
		{
			name: "replayed from another sign-in",
			alg:  "RS256", kid: "rsa-1",
			spoil: func(c map[string]any) { c["nonce"] = "a-different-sign-in" },
			want:  "does not answer this sign-in",
		},
		{
			name: "no nonce",
			alg:  "RS256", kid: "rsa-1",
			spoil: func(c map[string]any) { delete(c, "nonce") },
			want:  "does not answer this sign-in",
		},
		{
			name: "from another issuer",
			alg:  "RS256", kid: "rsa-1",
			spoil: func(c map[string]any) { c["iss"] = "https://somewhere.else" },
			want:  "not",
		},
		{
			name: "no subject",
			alg:  "RS256", kid: "rsa-1",
			spoil: func(c map[string]any) { delete(c, "sub") },
			want:  "no subject",
		},
		{
			name: "signed with a key the provider does not publish",
			alg:  "RS256", kid: "rsa-does-not-exist",
			want: "no signing key",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idp := newStubIDP(t)
			p := newTestProvider(t, idp)
			claims := goodClaims(idp)
			if tc.spoil != nil {
				tc.spoil(claims)
			}
			idp.idToken = idp.sign(t, tc.alg, tc.kid, claims)

			_, err := p.Exchange(context.Background(), "code", "verifier", testNonce)
			if err == nil {
				t.Fatal("the token was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// A token whose payload is edited after signing keeps a valid-looking
// structure. It must fail on the signature, not on a claim.
func TestATamperedPayloadFailsOnTheSignature(t *testing.T) {
	idp := newStubIDP(t)
	p := newTestProvider(t, idp)

	claims := goodClaims(idp)
	claims["sub"] = "the-real-user"
	signed := idp.sign(t, "RS256", "rsa-1", claims)

	parts := strings.Split(signed, ".")
	claims["sub"] = "somebody-else"
	edited, _ := json.Marshal(claims)
	parts[1] = b64(edited)
	idp.idToken = strings.Join(parts, ".")

	_, err := p.Exchange(context.Background(), "code", "verifier", testNonce)
	if err == nil || !strings.Contains(err.Error(), "does not verify") {
		t.Fatalf("err = %v, want a signature failure", err)
	}
}

// Discovery says which issuer it describes. A document that names a different
// one is a redirect somewhere the operator did not configure.
func TestDiscoveryMustNameTheConfiguredIssuer(t *testing.T) {
	idp := newStubIDP(t)
	idp.issuerOverride = "https://not-the-configured-issuer.example"
	p := newTestProvider(t, idp)

	if _, err := p.Metadata(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "is for issuer") {
		t.Fatalf("err = %v, want the mismatch to be refused", err)
	}
}

func TestTheAuthorizationURLCarriesPKCEAndTheNonce(t *testing.T) {
	idp := newStubIDP(t)
	p := newTestProvider(t, idp)

	raw, err := p.AuthCodeURL(context.Background(), "state-value", testNonce, "verifier-value")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	for field, want := range map[string]string{
		"response_type":         "code",
		"client_id":             testClientID,
		"state":                 "state-value",
		"nonce":                 testNonce,
		"code_challenge_method": "S256",
		"code_challenge":        CodeChallenge("verifier-value"),
	} {
		if got := q.Get(field); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	// The verifier itself must not be in a URL the browser holds — that is the
	// whole difference between S256 and plain.
	if strings.Contains(raw, "verifier-value") {
		t.Error("the PKCE verifier reached the browser")
	}
	if scope := q.Get("scope"); !strings.Contains(scope, "openid") {
		t.Errorf("scope = %q, want openid included", scope)
	}
}

// A provider that refuses the code says why, and the operator needs to see it:
// a wrong client secret and a wrong redirect URI look identical otherwise.
func TestTheProvidersRefusalIsReported(t *testing.T) {
	idp := newStubIDP(t)
	p := newTestProvider(t, idp)
	idp.tokenError = "invalid_client"

	_, err := p.Exchange(context.Background(), "code", "verifier", testNonce)
	if err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Fatalf("err = %v, want the provider's own reason", err)
	}
}

func TestAnIssuerMustBeHTTPS(t *testing.T) {
	_, err := New(Config{
		Issuer:      "http://idp.example",
		ClientID:    "id",
		RedirectURL: "https://3270web.example/cb",
	})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("err = %v, want plain HTTP refused", err)
	}
	// Loopback is the exception, so a provider running on the same machine for
	// a trial does not require a certificate.
	if _, err := New(Config{
		Issuer:      "http://localhost:9000",
		ClientID:    "id",
		RedirectURL: "https://3270web.example/cb",
	}); err != nil {
		t.Errorf("loopback issuer refused: %v", err)
	}
}
