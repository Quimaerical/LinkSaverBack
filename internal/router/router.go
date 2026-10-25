package router

import (
	"linksaverback/internal/config"
	"linksaverback/internal/handlers"
	"linksaverback/internal/middleware"
	"linksaverback/internal/services"
	"linksaverback/web"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupRouter initializes Gin engine with all routes, middleware and handlers.
func SetupRouter(cfg *config.Config, db *gorm.DB) (*gin.Engine, error) {
	// Initialize services
	dirService := services.NewDirectoryService(db, cfg)
	syncService := services.NewSyncService(db)

	// Initialize handlers
	pingHandler := handlers.NewPingHandler(cfg)
	dirHandler := handlers.NewDirectoryHandler(dirService)
	syncHandler := handlers.NewSyncHandler(dirService, syncService)
	vaultHandler := handlers.NewVaultHandler(dirService, syncService)

	webHandler, err := web.NewWebHandler(cfg, dirService, syncService)
	if err != nil {
		return nil, err
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), middleware.CORSMiddleware())

	// Web UI Dashboard
	r.GET("/", webHandler.Index)

	// API v1
	v1 := r.Group("/api/v1")
	{
		// Ping & Health (unauthenticated)
		v1.GET("/ping", pingHandler.Ping)

		// Protected or open according to config.AuthToken
		apiGroup := v1.Group("")
		apiGroup.Use(middleware.AuthMiddleware(cfg))
		{
			// Directory management
			apiGroup.POST("/directories/register", dirHandler.Register)
			apiGroup.GET("/directories", dirHandler.GetAll)
			apiGroup.PUT("/directories/:tag/storage-path", dirHandler.UpdateStoragePath)

			// Sync engine
			apiGroup.GET("/sync/manifest/:tag", syncHandler.GetManifest)
			apiGroup.POST("/sync/upload/:tag", syncHandler.Upload)
			apiGroup.GET("/sync/download/:tag", syncHandler.Download)

			// Save Vault & History
			apiGroup.GET("/vault/:tag/history", vaultHandler.History)
			apiGroup.POST("/vault/:tag/restore", vaultHandler.Restore)

			// Audit Logs
			apiGroup.GET("/logs", vaultHandler.Logs)
		}
	}

	return r, nil
}
