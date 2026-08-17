// SPDX-License-Identifier: AGPL-3.0-or-later

package copilot

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// The shared-device case this exists for: one browser, one cookie, two people
// signing in one after the other. The second must not land on the first one's
// AI credentials.
func TestIdentityChangesWithTheAccount(t *testing.T) {
	const cookie = "0123456789abcdef0123456789abcdef"

	alice := identityWith(t, cookie, "user-alice")
	aliceAgain := identityWith(t, cookie, "user-alice")
	bob := identityWith(t, cookie, "user-bob")

	if alice != aliceAgain {
		t.Errorf("the same account in the same browser got two identities (%q then %q); a sign-in would lose its own settings", alice, aliceAgain)
	}
	if alice == bob {
		t.Errorf("two accounts sharing one browser got the same identity %q — the successor inherits the credentials", alice)
	}
	for _, id := range []string{alice, bob} {
		if !identityIDPattern.MatchString(id) {
			t.Errorf("identity %q is used to build a file path and must match the safe pattern", id)
		}
	}
}

// The single-operator deployment has no account to bind to, and its stored
// logins must survive this change untouched — so the key stays the cookie.
func TestIdentityIsTheCookieWithNoAccount(t *testing.T) {
	const cookie = "0123456789abcdef0123456789abcdef"
	if got := identityWith(t, cookie, ""); got != cookie {
		t.Errorf("identity with no account = %q, want the cookie %q", got, cookie)
	}
}

// Two browsers belonging to the same person stay separate, which is the
// pre-existing behaviour: a Copilot login lives in the browser it was made in.
func TestIdentityChangesWithTheBrowser(t *testing.T) {
	a := identityWith(t, "0123456789abcdef0123456789abcdef", "user-alice")
	b := identityWith(t, "fedcba9876543210fedcba9876543210", "user-alice")
	if a == b {
		t.Error("one account in two browsers got one identity")
	}
}

func identityWith(t *testing.T, cookie, owner string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/copilot/status", nil)
	if cookie != "" {
		c.Request.AddCookie(&http.Cookie{Name: identityCookieName, Value: cookie})
	}
	if owner != "" {
		c.Set(OwnerContextKey, owner)
	}
	return Identity(c)
}
