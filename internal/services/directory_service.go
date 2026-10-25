package services

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"linksaverback/internal/config"
	"linksaverback/internal/models"

	"gorm.io/gorm"
)

var (
	ErrDirectoryNotFound = errors.New("directory tag not found")
	ErrInvalidTag        = errors.New("invalid directory tag")
	ErrInvalidPath       = errors.New("invalid storage path")
)

type DirectoryService struct {
	db  *gorm.DB
	cfg *config.Config
}

func NewDirectoryService(db *gorm.DB, cfg *config.Config) *DirectoryService {
	return &DirectoryService{db: db, cfg: cfg}
}

type RegisterDirectoryInput struct {
	Tag         string `json:"tag" binding:"required"`
	DisplayName string `json:"display_name" binding:"required"`
	Description string `json:"description"`
}

// Register ensures that a directory with the given tag exists.
func (s *DirectoryService) Register(input RegisterDirectoryInput) (*models.Directory, error) {
	tag := strings.TrimSpace(input.Tag)
	if tag == "" || strings.Contains(tag, "..") || strings.ContainsAny(tag, `/\:*?"<>|`) {
		return nil, ErrInvalidTag
	}

	var dir models.Directory
	err := s.db.Where("tag = ?", tag).First(&dir).Error
	if err == nil {
		// Already exists
		return &dir, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	storagePath := filepath.Join(s.cfg.DataDir, tag)
	if err := os.MkdirAll(storagePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory on disk: %w", err)
	}

	dir = models.Directory{
		Tag:         tag,
		DisplayName: input.DisplayName,
		Description: input.Description,
		StoragePath: storagePath,
	}

	if err := s.db.Create(&dir).Error; err != nil {
		return nil, fmt.Errorf("failed to register directory in database: %w", err)
	}

	return &dir, nil
}

// GetAll returns all registered directories.
func (s *DirectoryService) GetAll() ([]models.Directory, error) {
	var dirs []models.Directory
	if err := s.db.Order("tag ASC").Find(&dirs).Error; err != nil {
		return nil, err
	}
	return dirs, nil
}

// GetByTag finds a directory by its tag.
func (s *DirectoryService) GetByTag(tag string) (*models.Directory, error) {
	var dir models.Directory
	if err := s.db.Where("tag = ?", tag).First(&dir).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDirectoryNotFound
		}
		return nil, err
	}
	return &dir, nil
}

// UpdateStoragePath updates physical storage path on disk and migrates existing files.
func (s *DirectoryService) UpdateStoragePath(tag, newPath string) (*models.Directory, error) {
	dir, err := s.GetByTag(tag)
	if err != nil {
		return nil, err
	}

	cleanNewPath := filepath.Clean(strings.TrimSpace(newPath))
	if cleanNewPath == "" || cleanNewPath == "." {
		return nil, ErrInvalidPath
	}

	oldPath := dir.StoragePath
	if cleanNewPath == oldPath {
		return dir, nil
	}

	if err := os.MkdirAll(cleanNewPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create new storage path: %w", err)
	}

	// Move existing files if oldPath exists
	if _, err := os.Stat(oldPath); err == nil {
		if err := copyDirectoryContents(oldPath, cleanNewPath); err != nil {
			return nil, fmt.Errorf("failed to migrate files to new storage path: %w", err)
		}
	}

	dir.StoragePath = cleanNewPath
	if err := s.db.Save(dir).Error; err != nil {
		return nil, fmt.Errorf("failed to update storage path in database: %w", err)
	}

	return dir, nil
}

func copyDirectoryContents(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())

		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				return err
			}
			if err := copyDirectoryContents(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
