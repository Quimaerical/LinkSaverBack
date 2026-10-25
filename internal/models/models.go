package models

import (
	"time"
)

// Directory represents a logical save directory mapped across multiple clients.
type Directory struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Tag         string     `gorm:"uniqueIndex;not null;size:255" json:"tag"`
	DisplayName string     `gorm:"not null;size:255" json:"display_name"`
	Description string     `gorm:"type:text" json:"description"`
	StoragePath string     `gorm:"not null;type:text" json:"storage_path"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	SaveFiles   []SaveFile `gorm:"foreignKey:DirectoryID;constraint:OnDelete:CASCADE" json:"save_files,omitempty"`
	SyncLogs    []SyncLog  `gorm:"foreignKey:DirectoryID;constraint:OnDelete:CASCADE" json:"sync_logs,omitempty"`
}

// SaveFile represents an individual game save file under a directory tag.
type SaveFile struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	DirectoryID      uint      `gorm:"not null;index" json:"directory_id"`
	RelativePath     string    `gorm:"not null;type:text" json:"relative_path"`
	FileName         string    `gorm:"not null;size:255" json:"file_name"`
	OriginalName     string    `gorm:"not null;size:255" json:"original_name"`
	Sha256Hash       string    `gorm:"size:64;not null" json:"sha256_hash"`
	FileSizeBytes    int64     `gorm:"not null" json:"file_size_bytes"`
	IsActive         bool      `gorm:"index" json:"is_active"`
	ConflictSuffix   string    `gorm:"size:50" json:"conflict_suffix"`
	RemoteModifiedAt time.Time `gorm:"not null" json:"remote_modified_at"`
	Version          int       `gorm:"default:1" json:"version"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// SyncLog registers audit logs for synchronization operations.
type SyncLog struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	DirectoryID uint      `gorm:"not null;index" json:"directory_id"`
	ClientType  string    `gorm:"not null;size:50" json:"client_type"` // Switch, Desktop, Android
	Action      string    `gorm:"not null;size:50" json:"action"`      // upload, download, conflict_renamed, restore
	Status      string    `gorm:"not null;size:50" json:"status"`      // success, failed
	Timestamp   time.Time `gorm:"autoCreateTime" json:"timestamp"`
}
