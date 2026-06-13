package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HealthHandler is a lightweight liveness probe used by the Docker HEALTHCHECK
// and by orchestrators (Kubernetes, Compose). It intentionally performs no
// session or s3270 work so it stays fast and dependency-free.
func (app *App) HealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "version": appVersion})
}
