package handlers

import (
	"errors"
	"net/http"

	"linksaverback/internal/services"

	"github.com/gin-gonic/gin"
)

type VaultHandler struct {
	dirService  *services.DirectoryService
	syncService *services.SyncService
}

func NewVaultHandler(dirService *services.DirectoryService, syncService *services.SyncService) *VaultHandler {
	return &VaultHandler{
		dirService:  dirService,
		syncService: syncService,
	}
}

// History handles GET /api/v1/vault/:tag/history
func (h *VaultHandler) History(c *gin.Context) {
	tag := c.Param("tag")
	dir, err := h.dirService.GetByTag(tag)
	if err != nil {
		if errors.Is(err, services.ErrDirectoryNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "Not Found",
				"message": "Directory tag not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	history, err := h.syncService.GetVaultHistory(dir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tag":          dir.Tag,
		"display_name": dir.DisplayName,
		"saves":        history,
	})
}

// Restore handles POST /api/v1/vault/:tag/restore
func (h *VaultHandler) Restore(c *gin.Context) {
	tag := c.Param("tag")
	dir, err := h.dirService.GetByTag(tag)
	if err != nil {
		if errors.Is(err, services.ErrDirectoryNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "Not Found",
				"message": "Directory tag not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	var body struct {
		FileID uint `json:"file_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Bad Request",
			"message": "file_id is required",
		})
		return
	}

	clientType := c.GetHeader("X-Client-Type")
	if clientType == "" {
		clientType = "WebUI"
	}

	restored, err := h.syncService.RestoreSaveFile(dir, body.FileID, clientType)
	if err != nil {
		if errors.Is(err, services.ErrSaveFileNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "Not Found",
				"message": "Save file not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Save file successfully promoted to active",
		"restored": restored,
	})
}

// Logs handles GET /api/v1/logs
func (h *VaultHandler) Logs(c *gin.Context) {
	logs, err := h.syncService.GetRecentLogs(50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, logs)
}
