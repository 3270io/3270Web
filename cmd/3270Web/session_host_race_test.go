package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

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
