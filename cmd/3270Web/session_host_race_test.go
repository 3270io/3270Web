// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/render"
	"github.com/jnnngs/3270Web/internal/session"
)

// TestScreenContentHandler_ConcurrentHostSwap guards against a data race
// where handlers read s.Host directly (a live *unsynchronized* field read)
// while a reconnect/workflow-load concurrently swaps it under the session
// lock (see resetSessionHost). Before routing all Host access through
// app.sessionHost, running this with -race would report a data race on
// s.Host between this handler goroutine and the swap goroutine below —
// exactly the browser-poller-vs-Play/Debug-reconnect scenario from the
// original review finding.
func TestScreenContentHandler_ConcurrentHostSwap(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mh1, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh1.Connected = true

	app := &App{
		SessionManager: session.NewManager(),
		Renderer:       render.NewHtmlRenderer(),
	}
	sess := app.SessionManager.CreateSession(mh1)

	r := gin.New()
	r.GET("/screen/content", app.ScreenContentHandler)

	var wg sync.WaitGroup

	// Goroutine A: repeatedly hits the handler, mirroring the browser's
	// screen poller reading s.Host on every request.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			req := httptest.NewRequest(http.MethodGet, "/screen/content", nil)
			req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
		}
	}()

	// Goroutine B: repeatedly swaps s.Host under the session lock, mirroring
	// resetSessionHost (called by Play/Debug/reconnect) racing the poller.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			mh, err := host.NewMockHost("")
			if err != nil {
				t.Error(err)
				return
			}
			mh.Connected = true
			withSessionLock(sess, func() {
				sess.Host = mh
			})
		}
	}()

	wg.Wait()
}

// The admin session list reads whether each session is connected. It already
// took the session lock for LastAccess and Playback and then read s.Host
// outside it, which is the same race one screen further from the terminal:
// an administrator loading /admin while somebody reconnects.
func TestAdminSessionView_ConcurrentHostSwap(t *testing.T) {
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	app, sess := newTestAppWithSession(t, mh)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		names := map[string]string{}
		for i := 0; i < 200; i++ {
			app.toAdminSessionView(sess, names, time.Now())
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			next, err := host.NewMockHost("")
			if err != nil {
				t.Error(err)
				return
			}
			next.Connected = true
			withSessionLock(sess, func() {
				sess.Host = next
			})
		}
	}()

	wg.Wait()
}

// ScreenHandler is the page-load twin of ScreenContentHandler and carried the
// identical unlocked read. It renders an HTML template, so it is not driven
// over HTTP here the way its twin is; the guard is that it takes the host
// through the locking helper and decides on that value.
func TestScreenHandlerReadsTheHostUnderTheLock(t *testing.T) {
	src := readSource(t, "cmd/3270Web/main.go")
	at := strings.Index(src, "func (app *App) ScreenHandler(")
	if at < 0 {
		t.Fatal("main.go: ScreenHandler is gone")
	}
	body := src[at : at+1200]
	if !strings.Contains(body, "h := app.sessionHost(s)") || !strings.Contains(body, "if h == nil {") {
		t.Error("main.go: ScreenHandler reads s.Host without the session lock, which races " +
			"resetSessionHost's swap")
	}
}
