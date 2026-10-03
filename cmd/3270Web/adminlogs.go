// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"os"
)

func (app *App) AdminLogsPageHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "admin-logs.html", gin.H{"Enabled": os.Getenv("ALLOW_LOG_ACCESS") == "true"})
}
