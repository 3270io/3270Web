// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// HealthHandler is a readiness probe used by the Docker HEALTHCHECK and by
// orchestrators (Kubernetes, Compose). Beyond basic liveness, it verifies the
// configured s3270 binary actually resolves to an existing file — without
// this, an orchestrator would keep routing traffic to a container that can
// never start a host session. It deliberately just stats the binary rather
// than spawning it, so the check stays fast and side-effect-free.
func (app *App) HealthHandler(c *gin.Context) {
	execPath := ""
	if app.Config != nil {
		execPath = app.Config.ExecPath
	}
	resolved := resolveS3270Path(execPath)
	if !fileExists(resolved) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "unavailable",
			"version": appVersion,
			"error":   fmt.Sprintf("s3270 binary not found at %q", resolved),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "version": appVersion})
}
