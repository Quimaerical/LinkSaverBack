package handlers

import (
	"errors"
	"net/http"

	"linksaverback/internal/services"

	"github.com/gin-gonic/gin"
)

type DirectoryHandler struct {
	dirService *services.DirectoryService
}

func NewDirectoryHandler(dirService *services.DirectoryService) *DirectoryHandler {
	return &DirectoryHandler{dirService: dirService}
}

// Register handles POST /api/v1/directories/register
func (h *DirectoryHandler) Register(c *gin.Context) {
	var input services.RegisterDirectoryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Bad Request",
			"message": err.Error(),
		})
		return
	}

	dir, err := h.dirService.Register(input)
	if err != nil {
		if errors.Is(err, services.ErrInvalidTag) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid Tag",
				"message": "Tag must not be empty or contain invalid path characters",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, dir)
}

// GetAll handles GET /api/v1/directories
func (h *DirectoryHandler) GetAll(c *gin.Context) {
	dirs, err := h.dirService.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, dirs)
}

// UpdateStoragePath handles PUT /api/v1/directories/:tag/storage-path
func (h *DirectoryHandler) UpdateStoragePath(c *gin.Context) {
	tag := c.Param("tag")
	var body struct {
		StoragePath string `json:"storage_path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Bad Request",
			"message": "storage_path is required",
		})
		return
	}

	dir, err := h.dirService.UpdateStoragePath(tag, body.StoragePath)
	if err != nil {
		if errors.Is(err, services.ErrDirectoryNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "Not Found",
				"message": "Directory tag not found",
			})
			return
		}
		if errors.Is(err, services.ErrInvalidPath) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Bad Request",
				"message": "Invalid storage path provided",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, dir)
}
