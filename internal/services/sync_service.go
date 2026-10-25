package services

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"linksaverback/internal/models"

	"gorm.io/gorm"
)

var (
	ErrSaveFileNotFound = errors.New("save file not found")
	ErrPathTraversal    = errors.New("invalid path: path traversal detected")
	ErrEmptyFile        = errors.New("uploaded file is empty")
)

type SyncService struct {
	db *gorm.DB
}

func NewSyncService(db *gorm.DB) *SyncService {
	return &SyncService{db: db}
}

// ManifestFileDTO represents an active file entry in the sync manifest.
type ManifestFileDTO struct {
	RelativePath     string    `json:"relative_path"`
	FileName         string    `json:"file_name"`
	Sha256Hash       string    `json:"sha256_hash"`
	FileSizeBytes    int64     `json:"file_size_bytes"`
	RemoteModifiedAt time.Time `json:"remote_modified_at"`
	Version          int       `json:"version"`
}

// ManifestDTO represents the response payload for a directory manifest.
type ManifestDTO struct {
	Tag         string            `json:"tag"`
	DisplayName string            `json:"display_name"`
	StoragePath string            `json:"storage_path"`
	ServerTime  int64             `json:"server_time"`
	Files       []ManifestFileDTO `json:"files"`
}

// GetManifest returns all active files for a given directory tag.
func (s *SyncService) GetManifest(dir *models.Directory) (*ManifestDTO, error) {
	var files []models.SaveFile
	err := s.db.Where("directory_id = ? AND is_active = ?", dir.ID, true).
		Order("relative_path ASC").
		Find(&files).Error
	if err != nil {
		return nil, err
	}

	manifestFiles := make([]ManifestFileDTO, 0, len(files))
	for _, f := range files {
		manifestFiles = append(manifestFiles, ManifestFileDTO{
			RelativePath:     f.RelativePath,
			FileName:         f.FileName,
			Sha256Hash:       f.Sha256Hash,
			FileSizeBytes:    f.FileSizeBytes,
			RemoteModifiedAt: f.RemoteModifiedAt,
			Version:          f.Version,
		})
	}

	return &ManifestDTO{
		Tag:         dir.Tag,
		DisplayName: dir.DisplayName,
		StoragePath: dir.StoragePath,
		ServerTime:  time.Now().Unix(),
		Files:       manifestFiles,
	}, nil
}

// UploadFileInput contains parameters for processing an uploaded save file.
type UploadFileInput struct {
	Directory        *models.Directory
	RelativePath     string
	RemoteModifiedAt time.Time
	ClientType       string
	Reader           io.Reader
	ExpectedSize     int64
}

// UploadResult contains the result of an upload operation.
type UploadResult struct {
	SaveFile        *models.SaveFile `json:"save_file"`
	CollisionAction string           `json:"collision_action"` // "none", "displaced_previous", "saved_as_conflict"
	Message         string           `json:"message"`
}

// ProcessUpload processes an uploaded file with atomic write and collision resolution.
func (s *SyncService) ProcessUpload(input UploadFileInput) (*UploadResult, error) {
	cleanRelPath, err := SanitizeRelativePath(input.RelativePath)
	if err != nil {
		s.logSync(input.Directory.ID, input.ClientType, "upload", "failed")
		return nil, err
	}

	fileName := filepath.Base(cleanRelPath)
	destDir := filepath.Join(input.Directory.StoragePath, filepath.Dir(cleanRelPath))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		s.logSync(input.Directory.ID, input.ClientType, "upload", "failed")
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	// 1. Write atomically to temporary file
	tempFile, err := os.CreateTemp(input.Directory.StoragePath, ".linksaver_upload_*")
	if err != nil {
		s.logSync(input.Directory.ID, input.ClientType, "upload", "failed")
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		// Clean up temp file if it still exists
		if _, err := os.Stat(tempPath); err == nil {
			_ = os.Remove(tempPath)
		}
	}()

	hasher := sha256.New()
	multiWriter := io.MultiWriter(tempFile, hasher)

	writtenBytes, err := io.Copy(multiWriter, input.Reader)
	if err != nil {
		_ = tempFile.Close()
		s.logSync(input.Directory.ID, input.ClientType, "upload", "failed")
		return nil, fmt.Errorf("failed writing upload stream: %w", err)
	}
	_ = tempFile.Close()

	if writtenBytes == 0 && input.ExpectedSize > 0 {
		s.logSync(input.Directory.ID, input.ClientType, "upload", "failed")
		return nil, ErrEmptyFile
	}

	sha256Hex := hex.EncodeToString(hasher.Sum(nil))

	// 2. Query for existing active file with the same relative path
	var existingActive models.SaveFile
	err = s.db.Where("directory_id = ? AND relative_path = ? AND is_active = ?",
		input.Directory.ID, cleanRelPath, true).First(&existingActive).Error

	now := time.Now()
	remoteMod := input.RemoteModifiedAt
	if remoteMod.IsZero() {
		remoteMod = now
	}

	result := &UploadResult{}

	// CASE A: No previous active file exists with this relative path
	if errors.Is(err, gorm.ErrRecordNotFound) {
		targetFinalPath := filepath.Join(input.Directory.StoragePath, cleanRelPath)
		if err := os.Rename(tempPath, targetFinalPath); err != nil {
			s.logSync(input.Directory.ID, input.ClientType, "upload", "failed")
			return nil, fmt.Errorf("failed to move temp file to target: %w", err)
		}

		newSave := models.SaveFile{
			DirectoryID:      input.Directory.ID,
			RelativePath:     cleanRelPath,
			FileName:         fileName,
			OriginalName:     fileName,
			Sha256Hash:       sha256Hex,
			FileSizeBytes:    writtenBytes,
			IsActive:         true,
			ConflictSuffix:   "",
			RemoteModifiedAt: remoteMod,
			Version:          1,
		}

		if err := s.db.Create(&newSave).Error; err != nil {
			s.logSync(input.Directory.ID, input.ClientType, "upload", "failed")
			return nil, err
		}

		s.logSync(input.Directory.ID, input.ClientType, "upload", "success")
		result.SaveFile = &newSave
		result.CollisionAction = "none"
		result.Message = "File uploaded successfully"
		return result, nil
	} else if err != nil {
		s.logSync(input.Directory.ID, input.ClientType, "upload", "failed")
		return nil, err
	}

	// CASE B: Exact same SHA-256 hash (same content)
	if existingActive.Sha256Hash == sha256Hex {
		// Update timestamps without duplicating or creating conflicts
		existingActive.RemoteModifiedAt = remoteMod
		existingActive.UpdatedAt = now
		_ = s.db.Save(&existingActive)
		_ = os.Remove(tempPath)

		s.logSync(input.Directory.ID, input.ClientType, "upload", "success")
		result.SaveFile = &existingActive
		result.CollisionAction = "none"
		result.Message = "Identical file content already up to date"
		return result, nil
	}

	// CASE C: Collision detected (Different hash with same relative_path)
	// Apply Save Collision Strategy:
	// Prioritize larger byte size as active.
	// If sizes are identical, prioritize newer remote_modified_at.
	incomingWins := writtenBytes > existingActive.FileSizeBytes ||
		(writtenBytes == existingActive.FileSizeBytes && remoteMod.After(existingActive.RemoteModifiedAt))

	nextSuffix, nextSuffixNum := s.findNextConflictSuffix(input.Directory.ID, existingActive.OriginalName)

	ext := filepath.Ext(existingActive.OriginalName)
	base := strings.TrimSuffix(existingActive.OriginalName, ext)
	suffixedFileName := fmt.Sprintf("%s %s%s", base, nextSuffix, ext)

	dirRel := filepath.Dir(cleanRelPath)
	suffixedRelPath := suffixedFileName
	if dirRel != "." && dirRel != "" {
		suffixedRelPath = filepath.Join(dirRel, suffixedFileName)
	}

	if incomingWins {
		// Displace existing active file to suffixed backup
		existingPhysicalPath := filepath.Join(input.Directory.StoragePath, existingActive.RelativePath)
		suffixedPhysicalPath := filepath.Join(input.Directory.StoragePath, suffixedRelPath)

		if err := os.Rename(existingPhysicalPath, suffixedPhysicalPath); err != nil {
			s.logSync(input.Directory.ID, input.ClientType, "upload", "failed")
			return nil, fmt.Errorf("failed to rename existing file to suffixed path: %w", err)
		}

		// Update existing record to inactive
		existingActive.IsActive = false
		existingActive.FileName = suffixedFileName
		existingActive.RelativePath = suffixedRelPath
		existingActive.ConflictSuffix = nextSuffix
		if err := s.db.Save(&existingActive).Error; err != nil {
			s.logSync(input.Directory.ID, input.ClientType, "upload", "failed")
			return nil, err
		}

		// Move incoming temp file to target original path
		targetOriginalPath := filepath.Join(input.Directory.StoragePath, cleanRelPath)
		if err := os.Rename(tempPath, targetOriginalPath); err != nil {
			s.logSync(input.Directory.ID, input.ClientType, "upload", "failed")
			return nil, fmt.Errorf("failed to move incoming file to active path: %w", err)
		}

		// Insert incoming as new active record
		newSave := models.SaveFile{
			DirectoryID:      input.Directory.ID,
			RelativePath:     cleanRelPath,
			FileName:         fileName,
			OriginalName:     existingActive.OriginalName,
			Sha256Hash:       sha256Hex,
			FileSizeBytes:    writtenBytes,
			IsActive:         true,
			ConflictSuffix:   "",
			RemoteModifiedAt: remoteMod,
			Version:          existingActive.Version + 1,
		}
		if err := s.db.Create(&newSave).Error; err != nil {
			s.logSync(input.Directory.ID, input.ClientType, "upload", "failed")
			return nil, err
		}

		s.logSync(input.Directory.ID, input.ClientType, "conflict_renamed", "success")
		s.logSync(input.Directory.ID, input.ClientType, "upload", "success")

		result.SaveFile = &newSave
		result.CollisionAction = "displaced_previous"
		result.Message = fmt.Sprintf("Collision resolved: larger incoming save active; previous save renamed to '%s'", suffixedFileName)
		return result, nil
	}

	// Incoming has smaller size (or older timestamp on tie):
	// Incoming is saved directly as suffixed inactive conflict save
	suffixedPhysicalPath := filepath.Join(input.Directory.StoragePath, suffixedRelPath)
	if err := os.Rename(tempPath, suffixedPhysicalPath); err != nil {
		s.logSync(input.Directory.ID, input.ClientType, "upload", "failed")
		return nil, fmt.Errorf("failed to save incoming file to conflict path: %w", err)
	}

	newSave := models.SaveFile{
		DirectoryID:      input.Directory.ID,
		RelativePath:     suffixedRelPath,
		FileName:         suffixedFileName,
		OriginalName:     existingActive.OriginalName,
		Sha256Hash:       sha256Hex,
		FileSizeBytes:    writtenBytes,
		IsActive:         false,
		ConflictSuffix:   nextSuffix,
		RemoteModifiedAt: remoteMod,
		Version:          existingActive.Version + 1,
	}
	if err := s.db.Create(&newSave).Error; err != nil {
		s.logSync(input.Directory.ID, input.ClientType, "upload", "failed")
		return nil, err
	}

	s.logSync(input.Directory.ID, input.ClientType, "conflict_renamed", "success")
	s.logSync(input.Directory.ID, input.ClientType, "upload", "success")

	result.SaveFile = &newSave
	result.CollisionAction = "saved_as_conflict"
	result.Message = fmt.Sprintf("Collision resolved: existing larger save kept active; incoming save stored as '%s'", suffixedFileName)
	_ = nextSuffixNum
	return result, nil
}

// DownloadFile locates a file and returns its physical path on disk.
func (s *SyncService) DownloadFile(dir *models.Directory, reqPath, clientType string) (string, *models.SaveFile, error) {
	cleanRelPath, err := SanitizeRelativePath(reqPath)
	if err != nil {
		s.logSync(dir.ID, clientType, "download", "failed")
		return "", nil, err
	}

	var saveFile models.SaveFile
	err = s.db.Where("directory_id = ? AND relative_path = ?", dir.ID, cleanRelPath).First(&saveFile).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.logSync(dir.ID, clientType, "download", "failed")
			return "", nil, ErrSaveFileNotFound
		}
		s.logSync(dir.ID, clientType, "download", "failed")
		return "", nil, err
	}

	fullPath := filepath.Join(dir.StoragePath, cleanRelPath)
	if _, err := os.Stat(fullPath); err != nil {
		s.logSync(dir.ID, clientType, "download", "failed")
		return "", nil, fmt.Errorf("file missing on disk: %w", err)
	}

	s.logSync(dir.ID, clientType, "download", "success")
	return fullPath, &saveFile, nil
}

// GetVaultHistory returns the complete version and collision history for a directory.
func (s *SyncService) GetVaultHistory(dir *models.Directory) ([]models.SaveFile, error) {
	var files []models.SaveFile
	err := s.db.Where("directory_id = ?", dir.ID).
		Order("original_name ASC, version DESC, created_at DESC").
		Find(&files).Error
	if err != nil {
		return nil, err
	}
	return files, nil
}

// RestoreSaveFile promotes an inactive or suffixed backup to become the active file.
func (s *SyncService) RestoreSaveFile(dir *models.Directory, fileID uint, clientType string) (*models.SaveFile, error) {
	var targetFile models.SaveFile
	if err := s.db.Where("id = ? AND directory_id = ?", fileID, dir.ID).First(&targetFile).Error; err != nil {
		s.logSync(dir.ID, clientType, "restore", "failed")
		return nil, ErrSaveFileNotFound
	}

	if targetFile.IsActive {
		// Already active
		return &targetFile, nil
	}

	// Find the currently active save file for this original_name
	var currentActive models.SaveFile
	err := s.db.Where("directory_id = ? AND original_name = ? AND is_active = ?",
		dir.ID, targetFile.OriginalName, true).First(&currentActive).Error

	targetPhysicalPath := filepath.Join(dir.StoragePath, targetFile.RelativePath)
	if _, err := os.Stat(targetPhysicalPath); err != nil {
		s.logSync(dir.ID, clientType, "restore", "failed")
		return nil, fmt.Errorf("target file missing on disk: %w", err)
	}

	dirRel := filepath.Dir(targetFile.RelativePath)
	originalRelPath := targetFile.OriginalName
	if dirRel != "." && dirRel != "" {
		originalRelPath = filepath.Join(dirRel, targetFile.OriginalName)
	}
	originalPhysicalPath := filepath.Join(dir.StoragePath, originalRelPath)

	if err == nil {
		// Demote currently active file to next available conflict suffix
		nextSuffix, _ := s.findNextConflictSuffix(dir.ID, currentActive.OriginalName)
		ext := filepath.Ext(currentActive.OriginalName)
		base := strings.TrimSuffix(currentActive.OriginalName, ext)
		demotedFileName := fmt.Sprintf("%s %s%s", base, nextSuffix, ext)

		demotedRelPath := demotedFileName
		if dirRel != "." && dirRel != "" {
			demotedRelPath = filepath.Join(dirRel, demotedFileName)
		}
		demotedPhysicalPath := filepath.Join(dir.StoragePath, demotedRelPath)

		// Rename current active to demoted physical path
		if err := os.Rename(originalPhysicalPath, demotedPhysicalPath); err != nil {
			s.logSync(dir.ID, clientType, "restore", "failed")
			return nil, fmt.Errorf("failed to rename current active file: %w", err)
		}

		currentActive.IsActive = false
		currentActive.FileName = demotedFileName
		currentActive.RelativePath = demotedRelPath
		currentActive.ConflictSuffix = nextSuffix
		_ = s.db.Save(&currentActive)
	}

	// Rename target file to original physical path
	if err := os.Rename(targetPhysicalPath, originalPhysicalPath); err != nil {
		s.logSync(dir.ID, clientType, "restore", "failed")
		return nil, fmt.Errorf("failed to move restored file to active path: %w", err)
	}

	// Promote target file
	targetFile.IsActive = true
	targetFile.FileName = targetFile.OriginalName
	targetFile.RelativePath = originalRelPath
	targetFile.ConflictSuffix = ""
	targetFile.Version++
	targetFile.UpdatedAt = time.Now()

	if err := s.db.Save(&targetFile).Error; err != nil {
		s.logSync(dir.ID, clientType, "restore", "failed")
		return nil, err
	}

	s.logSync(dir.ID, clientType, "restore", "success")
	return &targetFile, nil
}

// GetRecentLogs retrieves the most recent sync logs.
func (s *SyncService) GetRecentLogs(limit int) ([]models.SyncLog, error) {
	if limit <= 0 {
		limit = 50
	}
	var logs []models.SyncLog
	err := s.db.Order("timestamp DESC").Limit(limit).Find(&logs).Error
	return logs, err
}

func (s *SyncService) findNextConflictSuffix(directoryID uint, originalName string) (string, int) {
	var files []models.SaveFile
	_ = s.db.Where("directory_id = ? AND original_name = ?", directoryID, originalName).
		Find(&files).Error

	maxNum := 0
	for _, f := range files {
		if f.ConflictSuffix != "" {
			var num int
			if n, _ := fmt.Sscanf(f.ConflictSuffix, "(%d)", &num); n == 1 {
				if num > maxNum {
					maxNum = num
				}
			}
		}
	}
	nextNum := maxNum + 1
	return fmt.Sprintf("(%d)", nextNum), nextNum
}

func (s *SyncService) logSync(dirID uint, clientType, action, status string) {
	if clientType == "" {
		clientType = "Unknown"
	}
	logEntry := models.SyncLog{
		DirectoryID: dirID,
		ClientType:  clientType,
		Action:      action,
		Status:      status,
		Timestamp:   time.Now(),
	}
	_ = s.db.Create(&logEntry)
}

// SanitizeRelativePath prevents directory traversal attacks and normalizes paths.
func SanitizeRelativePath(p string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(p))
	// Convert Windows separators to standard slash
	clean = filepath.ToSlash(clean)
	clean = strings.TrimPrefix(clean, "/")

	if clean == "" || clean == "." {
		return "", ErrPathTraversal
	}
	if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || clean == ".." {
		return "", ErrPathTraversal
	}
	return clean, nil
}
