package middleware

import (
	"net/http"
	"strings"

	"linksaverback/internal/config"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware validates the Bearer token if LINK_SAVER_AUTH_TOKEN is configured.
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// If no auth token is configured, skip verification
		if cfg.AuthToken == "" {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// Also check query param or header auth_token
			authHeader = c.GetHeader("X-Auth-Token")
			if authHeader == "" {
				authHeader = c.Query("auth_token")
			}
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		token = strings.TrimSpace(token)

		if token != cfg.AuthToken {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "Unauthorized",
				"message": "Invalid or missing authentication token",
			})
			return
		}

		c.Next()
	}
}

// CORSMiddleware configures cross-origin request headers.
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, X-Auth-Token")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
