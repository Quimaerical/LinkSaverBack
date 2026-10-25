package database

import (
	"fmt"
	"log"

	"linksaverback/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitDB initializes the SQLite database, configures pragmas and runs migrations.
func InitDB(dbPath string) (*gorm.DB, error) {
	// Add pragmas for WAL mode and foreign keys to connection string
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON", dbPath)

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Auto-migrate tables
	if err := db.AutoMigrate(
		&models.Directory{},
		&models.SaveFile{},
		&models.SyncLog{},
	); err != nil {
		return nil, fmt.Errorf("failed to run database auto-migration: %w", err)
	}

	log.Printf("[DB] Database initialized successfully at: %s", dbPath)
	return db, nil
}
