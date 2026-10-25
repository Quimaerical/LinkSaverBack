package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"linksaverback/internal/services"

	"github.com/gin-gonic/gin"
)

type SyncHandler struct {
	dirService  *services.DirectoryService
	syncService *services.SyncService
}

func NewSyncHandler(dirService *services.DirectoryService, syncService *services.SyncService) *SyncHandler {
	return &SyncHandler{
		dirService:  dirService,
		syncService: syncService,
	}
}

// GetManifest handles GET /api/v1/sync/manifest/:tag
func (h *SyncHandler) GetManifest(c *gin.Context) {
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

	manifest, err := h.syncService.GetManifest(dir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, manifest)
}

// Upload handles POST /api/v1/sync/upload/:tag
func (h *SyncHandler) Upload(c *gin.Context) {
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

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Bad Request",
			"message": "Missing file form field in multipart request",
		})
		return
	}

	fileReader, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": "Failed to read uploaded file payload",
		})
		return
	}
	defer fileReader.Close()

	relPath := c.PostForm("relative_path")
	if relPath == "" {
		relPath = fileHeader.Filename
	}

	clientType := c.PostForm("client_type")
	if clientType == "" {
		clientType = c.GetHeader("X-Client-Type")
		if clientType == "" {
			clientType = "Generic"
		}
	}

	remoteMod := parseRemoteModifiedAt(c.PostForm("remote_modified_at"))

	result, err := h.syncService.ProcessUpload(services.UploadFileInput{
		Directory:        dir,
		RelativePath:     relPath,
		RemoteModifiedAt: remoteMod,
		ClientType:       clientType,
		Reader:           fileReader,
		ExpectedSize:     fileHeader.Size,
	})
	if err != nil {
		if errors.Is(err, services.ErrPathTraversal) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Bad Request",
				"message": "Invalid relative path",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// Download handles GET /api/v1/sync/download/:tag?path=...
func (h *SyncHandler) Download(c *gin.Context) {
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

	reqPath := c.Query("path")
	if reqPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Bad Request",
			"message": "Query parameter 'path' is required",
		})
		return
	}

	clientType := c.GetHeader("X-Client-Type")
	if clientType == "" {
		clientType = "Generic"
	}

	physicalPath, saveFile, err := h.syncService.DownloadFile(dir, reqPath, clientType)
	if err != nil {
		if errors.Is(err, services.ErrSaveFileNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "Not Found",
				"message": "Requested save file was not found",
			})
			return
		}
		if errors.Is(err, services.ErrPathTraversal) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Bad Request",
				"message": "Invalid file path",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	c.Header("Content-Disposition", "attachment; filename=\""+saveFile.FileName+"\"")
	c.Header("Content-Type", "application/octet-stream")
	c.Header("X-Sha256", saveFile.Sha256Hash)
	c.File(physicalPath)
}

func parseRemoteModifiedAt(val string) time.Time {
	if val == "" {
		return time.Now()
	}
	// Try parsing unix timestamp (seconds)
	if sec, err := strconv.ParseInt(val, 10, 64); err == nil {
		return time.Unix(sec, 0)
	}
	// Try parsing RFC3339
	if t, err := time.Parse(time.RFC3339, val); err == nil {
		return t
	}
	return time.Now()
}
