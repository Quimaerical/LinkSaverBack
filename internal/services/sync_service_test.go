package services_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"linksaverback/internal/config"
	"linksaverback/internal/database"
	"linksaverback/internal/services"
)

func setupTestEnvironment(t *testing.T) (*services.DirectoryService, *services.SyncService, *config.Config, func()) {
	tmpDir, err := os.MkdirTemp("", "linksaver_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	cfg := &config.Config{
		Port:         "8080",
		DataDir:      filepath.Join(tmpDir, "saves"),
		DatabasePath: filepath.Join(tmpDir, "test.db"),
		Version:      "1.0.0",
	}
	if err := cfg.EnsureDirectories(); err != nil {
		t.Fatalf("Failed to ensure test dirs: %v", err)
	}

	db, err := database.InitDB(cfg.DatabasePath)
	if err != nil {
		t.Fatalf("Failed to init test db: %v", err)
	}

	dirService := services.NewDirectoryService(db, cfg)
	syncService := services.NewSyncService(db)

	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	return dirService, syncService, cfg, cleanup
}

func TestDirectoryRegistrationAndStoragePath(t *testing.T) {
	dirService, _, _, cleanup := setupTestEnvironment(t)
	defer cleanup()

	// 1. Register new tag
	dir, err := dirService.Register(services.RegisterDirectoryInput{
		Tag:         "retroarch-cores",
		DisplayName: "RetroArch Cores",
		Description: "SNES and GBA saves",
	})
	if err != nil {
		t.Fatalf("Unexpected error registering directory: %v", err)
	}

	if dir.Tag != "retroarch-cores" {
		t.Errorf("Expected tag 'retroarch-cores', got '%s'", dir.Tag)
	}

	// Verify directory exists on disk
	if _, err := os.Stat(dir.StoragePath); os.IsNotExist(err) {
		t.Errorf("Expected storage directory on disk at %s", dir.StoragePath)
	}

	// 2. Register same tag again (idempotent)
	dir2, err := dirService.Register(services.RegisterDirectoryInput{
		Tag:         "retroarch-cores",
		DisplayName: "RetroArch Cores",
	})
	if err != nil || dir2.ID != dir.ID {
		t.Fatalf("Expected idempotent return of existing directory, got ID %d vs %d", dir2.ID, dir.ID)
	}

	// 3. Update storage path
	newStorage := filepath.Join(os.TempDir(), "custom_retroarch_saves")
	updatedDir, err := dirService.UpdateStoragePath("retroarch-cores", newStorage)
	if err != nil {
		t.Fatalf("Failed to update storage path: %v", err)
	}
	if updatedDir.StoragePath != newStorage {
		t.Errorf("Expected updated storage path %s, got %s", newStorage, updatedDir.StoragePath)
	}
	_ = os.RemoveAll(newStorage)
}

func TestSaveCollisionStrategy(t *testing.T) {
	dirService, syncService, _, cleanup := setupTestEnvironment(t)
	defer cleanup()

	dir, err := dirService.Register(services.RegisterDirectoryInput{
		Tag:         "zelda-saves",
		DisplayName: "Zelda Saves",
	})
	if err != nil {
		t.Fatalf("Failed to register directory: %v", err)
	}

	// 1. Initial Upload: Moderate size (100 bytes)
	v1Content := strings.Repeat("A", 100)
	res1, err := syncService.ProcessUpload(services.UploadFileInput{
		Directory:        dir,
		RelativePath:     "zelda.sav",
		RemoteModifiedAt: time.Now().Add(-1 * time.Hour),
		ClientType:       "Switch",
		Reader:           bytes.NewReader([]byte(v1Content)),
		ExpectedSize:     int64(len(v1Content)),
	})
	if err != nil {
		t.Fatalf("Failed to upload v1: %v", err)
	}
	if !res1.SaveFile.IsActive || res1.SaveFile.FileName != "zelda.sav" {
		t.Errorf("Expected v1 to be active zelda.sav, got is_active=%v, file_name=%s", res1.SaveFile.IsActive, res1.SaveFile.FileName)
	}

	// Verify manifest
	manifest, err := syncService.GetManifest(dir)
	if err != nil || len(manifest.Files) != 1 {
		t.Fatalf("Expected 1 file in manifest, got %d", len(manifest.Files))
	}
	if manifest.Files[0].FileSizeBytes != 100 {
		t.Errorf("Expected size 100 in manifest, got %d", manifest.Files[0].FileSizeBytes)
	}

	// 2. Upload identical content: should not create duplicate or conflict
	resIdentical, err := syncService.ProcessUpload(services.UploadFileInput{
		Directory:        dir,
		RelativePath:     "zelda.sav",
		RemoteModifiedAt: time.Now(),
		ClientType:       "Desktop",
		Reader:           bytes.NewReader([]byte(v1Content)),
		ExpectedSize:     int64(len(v1Content)),
	})
	if err != nil {
		t.Fatalf("Failed to upload identical content: %v", err)
	}
	if resIdentical.CollisionAction != "none" {
		t.Errorf("Expected collision action 'none' for identical content, got '%s'", resIdentical.CollisionAction)
	}

	// 3. Collision: Upload SMALLER file (50 bytes)
	// Active should remain v1 (100 bytes), smaller incoming becomes "zelda (1).sav"
	v2Smaller := strings.Repeat("B", 50)
	resSmaller, err := syncService.ProcessUpload(services.UploadFileInput{
		Directory:        dir,
		RelativePath:     "zelda.sav",
		RemoteModifiedAt: time.Now(),
		ClientType:       "Android",
		Reader:           bytes.NewReader([]byte(v2Smaller)),
		ExpectedSize:     int64(len(v2Smaller)),
	})
	if err != nil {
		t.Fatalf("Failed to upload smaller collision file: %v", err)
	}
	if resSmaller.CollisionAction != "saved_as_conflict" {
		t.Errorf("Expected collision action 'saved_as_conflict', got '%s'", resSmaller.CollisionAction)
	}
	if resSmaller.SaveFile.IsActive {
		t.Errorf("Expected smaller file to be inactive")
	}
	if resSmaller.SaveFile.FileName != "zelda (1).sav" {
		t.Errorf("Expected smaller file to be named 'zelda (1).sav', got '%s'", resSmaller.SaveFile.FileName)
	}

	// Active file in manifest must still be the 100-byte file
	manifest2, _ := syncService.GetManifest(dir)
	if len(manifest2.Files) != 1 || manifest2.Files[0].FileSizeBytes != 100 {
		t.Errorf("Manifest must still show active 100-byte file, got %v", manifest2.Files)
	}

	// 4. Collision: Upload LARGER file (200 bytes)
	// Incoming larger file becomes active "zelda.sav", previous active is displaced to "zelda (2).sav"
	v3Larger := strings.Repeat("C", 200)
	resLarger, err := syncService.ProcessUpload(services.UploadFileInput{
		Directory:        dir,
		RelativePath:     "zelda.sav",
		RemoteModifiedAt: time.Now(),
		ClientType:       "Desktop",
		Reader:           bytes.NewReader([]byte(v3Larger)),
		ExpectedSize:     int64(len(v3Larger)),
	})
	if err != nil {
		t.Fatalf("Failed to upload larger collision file: %v", err)
	}
	if resLarger.CollisionAction != "displaced_previous" {
		t.Errorf("Expected collision action 'displaced_previous', got '%s'", resLarger.CollisionAction)
	}
	if !resLarger.SaveFile.IsActive || resLarger.SaveFile.FileName != "zelda.sav" {
		t.Errorf("Expected larger file to be active 'zelda.sav', got %v, %s", resLarger.SaveFile.IsActive, resLarger.SaveFile.FileName)
	}

	// 5. Inspect Save Vault
	vaultFiles, err := syncService.GetVaultHistory(dir)
	if err != nil {
		t.Fatalf("Failed to get vault history: %v", err)
	}
	// We should have 3 save files in total in the vault
	if len(vaultFiles) != 3 {
		t.Fatalf("Expected 3 files in vault history, got %d", len(vaultFiles))
	}

	var activeCount int
	var inactiveID uint
	for _, f := range vaultFiles {
		if f.IsActive {
			activeCount++
		} else {
			inactiveID = f.ID
		}
	}
	if activeCount != 1 {
		t.Errorf("Expected exactly 1 active file in vault, got %d", activeCount)
	}

	// 6. Test Vault Restore (Rollback)
	restored, err := syncService.RestoreSaveFile(dir, inactiveID, "WebUI")
	if err != nil {
		t.Fatalf("Failed to restore save file: %v", err)
	}
	if !restored.IsActive || restored.FileName != "zelda.sav" {
		t.Errorf("Restored file must now be active with original name 'zelda.sav'")
	}
}

func TestPathTraversalProtection(t *testing.T) {
	_, err := services.SanitizeRelativePath("../../etc/passwd")
	if err == nil {
		t.Errorf("Expected error for path traversal ../../etc/passwd")
	}

	_, err = services.SanitizeRelativePath("/absolute/path/file.sav")
	if err != nil {
		t.Errorf("Absolute-like path should be stripped of leading slash: %v", err)
	}

	clean, err := services.SanitizeRelativePath("subfolder/deep/game.sav")
	if err != nil || clean != "subfolder/deep/game.sav" {
		t.Errorf("Expected clean subfolder path, got %s, err=%v", clean, err)
	}
}
