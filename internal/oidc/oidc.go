// SPDX-License-Identifier: AGPL-3.0-or-later

// Package oidc signs people in through an OpenID Connect identity provider.
//
// It is a client for the authorization-code flow with PKCE, and nothing else.
// No implicit flow, no hybrid flow, no client-side tokens: the browser only
// ever carries a one-time code, and the token exchange happens between this
// server and the provider.
//
// It is written against the standard library on purpose. 3270Web ships as one
// binary onto networks that are frequently isolated, and a dependency tree for
// a protocol that is a few HTTP requests and a signature check is a cost paid
// by every deployment, including the ones that will never turn this on.
//
// What the provider is trusted for is narrow: it says who somebody is. What
// they may do here is decided by internal/authz from an account this server
// keeps, so a compromised or misconfigured provider cannot grant a role that
// nobody mapped to it.
package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// discoveryPath is where a provider publishes its metadata, relative to the
// issuer. Fixed by the specification.
const discoveryPath = "/.well-known/openid-configuration"

// maxResponseBytes bounds every document read from the provider.
//
// A discovery document and a key set are a few kilobytes. Reading them
// unbounded would let a provider — or anything that can answer as one — decide
// how much memory this server allocates.
const maxResponseBytes = 1 << 20

// httpTimeout bounds a single call to the provider. Sign-in is a person
// waiting at a form, so failing is better than hanging.
const httpTimeout = 15 * time.Second

// Config is what an operator has to supply.
type Config struct {
	// Issuer is the provider's base URL, exactly as it appears in the "iss"
	// claim. Discovery is read from Issuer + /.well-known/openid-configuration.
	Issuer string
	// ClientID and ClientSecret identify this deployment to the provider.
	// A confidential client is assumed: the secret never reaches a browser.
	ClientID     string
	ClientSecret string
	// RedirectURL is this server's callback, registered with the provider.
	RedirectURL string
	// Scopes requested in addition to "openid".
	Scopes []string
	// HTTPClient is overridable for tests. Nil means a client with a timeout.
	HTTPClient *http.Client
}

// Metadata is the subset of the discovery document this client uses.
type Metadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	EndSessionEndpoint    string   `json:"end_session_endpoint"`
	SigningAlgs           []string `json:"id_token_signing_alg_values_supported"`
	CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
}

// Provider is a configured, discovered identity provider.
type Provider struct {
	cfg  Config
	http *http.Client

	mu   sync.Mutex
	meta *Metadata
	keys *keySet
}

// New returns a provider. Discovery is deferred to first use rather than done
// here, so an unreachable provider does not stop the server from starting —
// the instance still has to serve its local break-glass sign-in, which is the
// one credential that works when the provider is down.
func New(cfg Config) (*Provider, error) {
	issuer := strings.TrimSpace(cfg.Issuer)
	if issuer == "" {
		return nil, errors.New("oidc: no issuer configured")
	}
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("oidc: issuer %q is not an absolute URL", cfg.Issuer)
	}
	if u.Scheme != "https" && !isLoopback(u.Hostname()) {
		// Bearer-equivalent tokens cross this connection. Plain HTTP is allowed
		// only against a loopback address, which is what a local test provider
		// is, and never against anything reachable from elsewhere.
		return nil, fmt.Errorf("oidc: issuer %q must be https", cfg.Issuer)
	}
	cfg.Issuer = strings.TrimRight(issuer, "/")

	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, errors.New("oidc: no client ID configured")
	}
	if strings.TrimSpace(cfg.RedirectURL) == "" {
		return nil, errors.New("oidc: no redirect URL configured")
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	return &Provider{cfg: cfg, http: client}, nil
}

// Issuer reports the configured issuer.
func (p *Provider) Issuer() string { return p.cfg.Issuer }

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// Metadata returns the discovery document, fetching it once.
//
// Cached for the life of the process. Endpoints do not move, and re-reading
// the document on every sign-in would put the provider's availability in front
// of a page that is already waiting on it.
func (p *Provider) Metadata(ctx context.Context) (*Metadata, error) {
	p.mu.Lock()
	meta := p.meta
	p.mu.Unlock()
	if meta != nil {
		return meta, nil
	}

	var fetched Metadata
	if err := p.getJSON(ctx, p.cfg.Issuer+discoveryPath, &fetched); err != nil {
		return nil, fmt.Errorf("oidc: read provider metadata: %w", err)
	}
	// The document says who it is for, and it has to be the issuer configured
	// here. Without this check a redirect to somebody else's discovery document
	// would silently repoint sign-in at a provider the operator never named.
	if strings.TrimRight(fetched.Issuer, "/") != p.cfg.Issuer {
		return nil, fmt.Errorf("oidc: provider metadata is for issuer %q, not %q", fetched.Issuer, p.cfg.Issuer)
	}
	if fetched.AuthorizationEndpoint == "" || fetched.TokenEndpoint == "" || fetched.JWKSURI == "" {
		return nil, errors.New("oidc: provider metadata is missing an endpoint this client needs")
	}

	p.mu.Lock()
	p.meta = &fetched
	p.mu.Unlock()
	return &fetched, nil
}

// AuthCodeURL builds the URL to send the browser to.
//
// state and nonce are the caller's to generate and to remember: state is
// checked when the browser comes back, and nonce is checked inside the ID
// token. verifier is the PKCE secret, which never leaves this server — only
// its hash goes to the provider.
func (p *Provider) AuthCodeURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	meta, err := p.Metadata(ctx)
	if err != nil {
		return "", err
	}
	endpoint, err := url.Parse(meta.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("oidc: provider authorization endpoint %q: %w", meta.AuthorizationEndpoint, err)
	}

	scopes := append([]string{"openid"}, p.cfg.Scopes...)
	q := endpoint.Query()
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", p.cfg.RedirectURL)
	q.Set("scope", strings.Join(dedupe(scopes), " "))
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", CodeChallenge(verifier))
	q.Set("code_challenge_method", "S256")
	endpoint.RawQuery = q.Encode()
	return endpoint.String(), nil
}

// tokenResponse is the token endpoint's reply.
type tokenResponse struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

// Exchange trades the authorization code for an ID token and verifies it.
//
// Verification is not a separate step a caller can forget: an unverified ID
// token is a string anybody can write, so this function only ever returns one
// whose signature, issuer, audience, expiry and nonce have been checked.
func (p *Provider) Exchange(ctx context.Context, code, verifier, nonce string) (*IDToken, error) {
	meta, err := p.Metadata(ctx)
	if err != nil {
		return nil, err
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {p.cfg.RedirectURL},
		"client_id":     {p.cfg.ClientID},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meta.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oidc: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	// Client authentication in the header rather than the body: it is the form
	// every provider accepts, and it keeps the secret out of any logging that
	// records request bodies.
	if p.cfg.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(p.cfg.ClientID), url.QueryEscape(p.cfg.ClientSecret))
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: token request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("oidc: read token response: %w", err)
	}

	var token tokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("oidc: token response was not JSON (status %d)", resp.StatusCode)
	}
	if token.Error != "" {
		// The provider's own words, which is what an operator needs to tell a
		// wrong secret from a wrong redirect URI.
		return nil, fmt.Errorf("oidc: provider refused the code: %s %s", token.Error, token.ErrorDesc)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: token endpoint returned %d", resp.StatusCode)
	}
	if token.IDToken == "" {
		return nil, errors.New("oidc: the provider returned no ID token")
	}

	return p.VerifyIDToken(ctx, token.IDToken, nonce)
}

// EndSessionURL returns where to send a browser to end the session at the
// provider, or "" when the provider does not publish one.
//
// Signing out of 3270Web always ends the login here. Whether it also ends it at
// the provider is the operator's call — on a shared machine it is what "sign
// out" has to mean, and on a personal one it signs the person out of
// everything else too.
func (p *Provider) EndSessionURL(ctx context.Context, idTokenHint, postLogoutRedirect string) string {
	meta, err := p.Metadata(ctx)
	if err != nil || meta.EndSessionEndpoint == "" {
		return ""
	}
	endpoint, err := url.Parse(meta.EndSessionEndpoint)
	if err != nil {
		return ""
	}
	q := endpoint.Query()
	if idTokenHint != "" {
		q.Set("id_token_hint", idTokenHint)
	}
	if postLogoutRedirect != "" {
		q.Set("post_logout_redirect_uri", postLogoutRedirect)
	}
	q.Set("client_id", p.cfg.ClientID)
	endpoint.RawQuery = q.Encode()
	return endpoint.String()
}

// getJSON reads a JSON document from the provider into out.
func (p *Provider) getJSON(ctx context.Context, target string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %d", target, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
