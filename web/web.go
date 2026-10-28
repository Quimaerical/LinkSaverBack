package web

import (
	"embed"
	"html/template"
	"net/http"

	"linksaverback/internal/config"
	"linksaverback/internal/services"

	"github.com/gin-gonic/gin"
)

//go:embed templates/* static/*
var contentFS embed.FS

type WebHandler struct {
	cfg         *config.Config
	dirService  *services.DirectoryService
	syncService *services.SyncService
	tmpl        *template.Template
}

func NewWebHandler(cfg *config.Config, dirService *services.DirectoryService, syncService *services.SyncService) (*WebHandler, error) {
	tmpl, err := template.ParseFS(contentFS, "templates/index.html")
	if err != nil {
		return nil, err
	}
	return &WebHandler{
		cfg:         cfg,
		dirService:  dirService,
		syncService: syncService,
		tmpl:        tmpl,
	}, nil
}

// StaticFS returns the embedded filesystem for static assets.
func (h *WebHandler) StaticFS() http.FileSystem {
	return http.FS(contentFS)
}

// Index renders the unified LinkSaver admin dashboard.
func (h *WebHandler) Index(c *gin.Context) {
	dirs, _ := h.dirService.GetAll()
	logs, _ := h.syncService.GetRecentLogs(30)

	data := gin.H{
		"Version":     h.cfg.Version,
		"DataDir":     h.cfg.DataDir,
		"Directories": dirs,
		"Logs":        logs,
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.Execute(c.Writer, data); err != nil {
		c.String(http.StatusInternalServerError, "Failed to render template: %v", err)
	}
}
