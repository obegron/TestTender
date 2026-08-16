package libpod

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/obegron/testtender/internal/config"
	"github.com/obegron/testtender/internal/server/routes/common"
)

// Version - get version.
// https://docs.podman.io/en/latest/_static/api.html?version=v4.2#tag/system/operation/SystemVersionLibpod
// GET "/libpod/version"
func Version(cr *common.ContextRouter, c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"GitCommit": config.Build,
		"Os":        config.OS,
		"Version":   config.Version,
	})
}

// Ping - dummy endpoint you can use to test if the server is accessible.
// https://docs.podman.io/en/latest/_static/api.html?version=v4.2#tag/system/operation/SystemPing
// GET "/libpod/_ping"
func Ping(cr *common.ContextRouter, c *gin.Context) {
	c.String(http.StatusOK, "OK")
}
