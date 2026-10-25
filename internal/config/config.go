package config

import (
	"os"
	"path/filepath"
)

// Config holds the server runtime configuration.
type Config struct {
	Port         string
	DataDir      string
	DatabasePath string
	AuthToken    string
	Version      string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	port := getEnv("PORT", "8080")
	dataDir := getEnv("SAVESYNC_DATA_DIR", "")
	if dataDir == "" {
		dataDir = getEnv("LINK_SAVER_DATA_DIR", "./storage/saves")
	}

	dbPath := getEnv("DATABASE_PATH", "./storage/linksaver.db")
	authToken := getEnv("LINK_SAVER_AUTH_TOKEN", "")

	return &Config{
		Port:         port,
		DataDir:      filepath.Clean(dataDir),
		DatabasePath: filepath.Clean(dbPath),
		AuthToken:    authToken,
		Version:      "1.0.0",
	}
}

// EnsureDirectories ensures that storage and database directories exist.
func (c *Config) EnsureDirectories() error {
	if err := os.MkdirAll(c.DataDir, 0755); err != nil {
		return err
	}
	dbDir := filepath.Dir(c.DatabasePath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return err
	}
	return nil
}

func getEnv(key, defaultVal string) string {
	if val, exists := os.LookupEnv(key); exists && val != "" {
		return val
	}
	return defaultVal
}
