package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/h3270/h3270-go/internal/config"
	"github.com/h3270/h3270-go/internal/host"
	"github.com/h3270/h3270-go/internal/render"
	"github.com/h3270/h3270-go/internal/session"
)

type App struct {
	SessionManager *session.Manager
	Renderer       render.Renderer
	Config         *config.Config
}

func main() {
	cfg, err := config.Load("webapp/WEB-INF/h3270-config.xml")
	if err != nil {
		log.Printf("Warning: Could not load config: %v", err)
		cfg = &config.Config{ExecPath: "/usr/local/bin"}
	}

	app := &App{
		SessionManager: session.NewManager(),
		Renderer:       render.NewHtmlRenderer(),
		Config:         cfg,
	}

	r := gin.Default()
	r.LoadHTMLGlob("web/templates/*")
	r.Static("/static", "web/static")

	r.GET("/", app.HomeHandler)
	r.POST("/connect", app.ConnectHandler)
	r.GET("/screen", app.ScreenHandler)
	r.POST("/submit", app.SubmitHandler)

	// Disconnect handler
	r.GET("/disconnect", app.DisconnectHandler)

	log.Println("Starting server on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}

func (app *App) HomeHandler(c *gin.Context) {
	// Check session
	if s := app.getSession(c); s != nil {
		c.Redirect(http.StatusFound, "/screen")
		return
	}
	c.HTML(http.StatusOK, "connect.html", gin.H{})
}

func (app *App) ConnectHandler(c *gin.Context) {
	hostname := c.PostForm("hostname")

	var h host.Host
	var err error

	// Determine host type
	if hostname == "mock" || hostname == "demo" {
		h, err = host.NewMockHost("webapp/WEB-INF/dump/advantis.dump")
	} else {
		// Real host
		execPath := "./s3270-bin/s3270-linux"
		if app.Config.ExecPath != "" && app.Config.ExecPath != "/usr/local/bin" {
			execPath = app.Config.ExecPath + "/s3270"
		}

		args := []string{}
		if app.Config.S3270Options.Model != "" {
			args = append(args, "-model", app.Config.S3270Options.Model)
		}
		// Add other options if needed
		args = append(args, hostname)

		h = host.NewS3270(execPath, args...)
	}

	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("Failed to create host: %v", err)})
		return
	}

	if err := h.Start(); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("Failed to start host connection: %v", err)})
		return
	}

	sess := app.SessionManager.CreateSession(h)
	// Set cookie
	c.SetCookie("h3270_session", sess.ID, 3600, "/", "", false, true)
	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) ScreenHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}

	if err := s.Host.UpdateScreen(); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("Update screen failed: %v", err)})
		return
	}

	screen := s.Host.GetScreen()
	rendered := app.Renderer.Render(screen, "/submit", s.ID)

	c.HTML(http.StatusOK, "screen.html", gin.H{
		"ScreenContent": template.HTML(rendered),
		"SessionID":     s.ID,
	})
}

func (app *App) SubmitHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.Redirect(http.StatusFound, "/")
		return
	}

	key := c.PostForm("key")

	// 1. Update fields from form data
	app.updateFields(c, s)

	// 2. Submit changes to host
	if err := s.Host.SubmitScreen(); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("Submit failed: %v", err)})
		return
	}

	// 3. Send action key
	actionKey := "Enter"
	if key != "" {
		actionKey = key
	}

	if err := s.Host.SendKey(actionKey); err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"Error": fmt.Sprintf("SendKey failed: %v", err)})
		return
	}

	c.Redirect(http.StatusFound, "/screen")
}

func (app *App) DisconnectHandler(c *gin.Context) {
	if s := app.getSession(c); s != nil {
		app.SessionManager.RemoveSession(s.ID)
	}
	c.SetCookie("h3270_session", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/")
}

func (app *App) updateFields(c *gin.Context, s *session.Session) {
	screen := s.Host.GetScreen()
	for _, f := range screen.Fields {
		if !f.IsProtected() {
			fieldName := fmt.Sprintf("field_%d_%d", f.StartX, f.StartY)

			if f.IsMultiline() {
				var parts []string
				for i := 0; i < f.Height(); i++ {
					val := c.PostForm(fmt.Sprintf("%s_%d", fieldName, i))
					// Java trimmed spaces? We should check if Gin returns empty string for missing fields.
					// If field is missing, it might mean user didn't change it or browser didn't send it?
					// Input type text sends empty string if empty.
					parts = append(parts, val)
				}
				f.SetValue(strings.Join(parts, "\n"))
			} else {
				val := c.PostForm(fieldName)
				f.SetValue(val)
			}
		}
	}
}

func (app *App) getSession(c *gin.Context) *session.Session {
	id, err := c.Cookie("h3270_session")
	if err != nil {
		return nil
	}
	s, ok := app.SessionManager.GetSession(id)
	if !ok {
		return nil
	}
	return s
}
