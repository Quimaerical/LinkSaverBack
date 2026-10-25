package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"linksaverback/internal/config"
	"linksaverback/internal/database"
	"linksaverback/internal/router"
	"linksaverback/internal/services"
)

func setupTestRouter(t *testing.T) (*http.Handler, func()) {
	tmpDir, err := os.MkdirTemp("", "linksaver_handler_test_*")
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
		t.Fatalf("Failed to init db: %v", err)
	}

	engine, err := router.SetupRouter(cfg, db)
	if err != nil {
		t.Fatalf("Failed to setup router: %v", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	h := http.Handler(engine)
	return &h, cleanup
}

func TestPingEndpoint(t *testing.T) {
	handler, cleanup := setupTestRouter(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/ping", nil)
	(*handler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", w.Code)
	}

	var res map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("Failed to parse json: %v", err)
	}

	if res["status"] != "ok" || res["version"] != "1.0.0" {
		t.Errorf("Unexpected ping response: %v", res)
	}
}

func TestWebUIIndex(t *testing.T) {
	handler, cleanup := setupTestRouter(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	(*handler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for Web UI, got %d", w.Code)
	}

	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte("LinkSaver")) {
		t.Errorf("Expected Web UI HTML to contain 'LinkSaver'")
	}
}

func TestFullSyncFlow(t *testing.T) {
	handler, cleanup := setupTestRouter(t)
	defer cleanup()

	// 1. Register Directory
	regBody, _ := json.Marshal(map[string]string{
		"tag":          "metroid-prime",
		"display_name": "Metroid Prime Saves",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/directories/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	(*handler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK registering directory, got %d: %s", w.Code, w.Body.String())
	}

	// 2. Upload a file via multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "metroid.sav")
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	_, _ = io.WriteString(part, "METROID_PRIME_SAVE_DATA_V1")
	_ = writer.WriteField("relative_path", "metroid.sav")
	_ = writer.WriteField("client_type", "Switch")
	_ = writer.Close()

	wUpload := httptest.NewRecorder()
	reqUpload, _ := http.NewRequest("POST", "/api/v1/sync/upload/metroid-prime", body)
	reqUpload.Header.Set("Content-Type", writer.FormDataContentType())
	(*handler).ServeHTTP(wUpload, reqUpload)

	if wUpload.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK uploading, got %d: %s", wUpload.Code, wUpload.Body.String())
	}

	// 3. Get Manifest
	wManifest := httptest.NewRecorder()
	reqManifest, _ := http.NewRequest("GET", "/api/v1/sync/manifest/metroid-prime", nil)
	(*handler).ServeHTTP(wManifest, reqManifest)

	if wManifest.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK getting manifest, got %d: %s", wManifest.Code, wManifest.Body.String())
	}

	var manifest services.ManifestDTO
	_ = json.Unmarshal(wManifest.Body.Bytes(), &manifest)
	if len(manifest.Files) != 1 || manifest.Files[0].FileName != "metroid.sav" {
		t.Errorf("Unexpected manifest response: %v", manifest)
	}

	// 4. Download file
	wDownload := httptest.NewRecorder()
	reqDownload, _ := http.NewRequest("GET", "/api/v1/sync/download/metroid-prime?path=metroid.sav", nil)
	(*handler).ServeHTTP(wDownload, reqDownload)

	if wDownload.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK downloading, got %d", wDownload.Code)
	}
	if wDownload.Body.String() != "METROID_PRIME_SAVE_DATA_V1" {
		t.Errorf("Expected downloaded content 'METROID_PRIME_SAVE_DATA_V1', got '%s'", wDownload.Body.String())
	}

	// 5. Check Vault History
	wVault := httptest.NewRecorder()
	reqVault, _ := http.NewRequest("GET", "/api/v1/vault/metroid-prime/history", nil)
	(*handler).ServeHTTP(wVault, reqVault)

	if wVault.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK getting vault history, got %d", wVault.Code)
	}

	// 6. Check Audit Logs
	wLogs := httptest.NewRecorder()
	reqLogs, _ := http.NewRequest("GET", "/api/v1/logs", nil)
	(*handler).ServeHTTP(wLogs, reqLogs)

	if wLogs.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK getting logs, got %d", wLogs.Code)
	}
}
