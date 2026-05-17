package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// PrintScreenHandler returns the current screen rendered by s3270's PrintText
// action so the browser can open it in a printable view. The format is taken
// from the "format" query parameter and defaults to "html". Allowed values:
// "html", "rtf", "string".
func (app *App) PrintScreenHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	format := c.DefaultQuery("format", "html")
	switch format {
	case "html", "rtf", "string":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "format must be one of html, rtf, string"})
		return
	}

	content, err := s.Host.PrintText(format)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"format":  format,
		"content": content,
	})
}
