package handlers

import (
	"net/http"
	"time"

	"linksaverback/internal/config"

	"github.com/gin-gonic/gin"
)

type PingHandler struct {
	cfg *config.Config
}

func NewPingHandler(cfg *config.Config) *PingHandler {
	return &PingHandler{cfg: cfg}
}

// Ping handles GET /api/v1/ping
func (h *PingHandler) Ping(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"server_time": time.Now().Unix(),
		"version":     h.cfg.Version,
	})
}
