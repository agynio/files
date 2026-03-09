package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
	"time"

	"github.com/agynio/files/internal/filestore"
	"github.com/agynio/files/internal/model"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type fakeFileStore struct {
	created     model.FileRecord
	createErr   error
	createCalls int
	getRecord   model.FileRecord
	getErr      error
	getCalls    int
	lastID      uuid.UUID
}

func (f *fakeFileStore) CreateFile(ctx context.Context, record model.FileRecord) error {
	f.createCalls++
	f.created = record
	return f.createErr
}

func (f *fakeFileStore) GetFile(ctx context.Context, id uuid.UUID) (model.FileRecord, error) {
	f.getCalls++
	f.lastID = id
	if f.getErr != nil {
		return model.FileRecord{}, f.getErr
	}
	return f.getRecord, nil
}

type fakeObjectStore struct {
	putKey         string
	putSize        int64
	putContentType string
	putCalls       int
	putErr         error
	presignKey     string
	presignExpiry  time.Duration
	presignURL     string
	presignErr     error
}

func (f *fakeObjectStore) PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	f.putCalls++
	f.putKey = key
	f.putSize = size
	f.putContentType = contentType
	return f.putErr
}

func (f *fakeObjectStore) PresignGetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	f.presignKey = key
	f.presignExpiry = expiry
	if f.presignErr != nil {
		return "", f.presignErr
	}
	return f.presignURL, nil
}

type fakeHealth struct {
	err   error
	calls int
}

func (f *fakeHealth) Ping(ctx context.Context) error {
	f.calls++
	return f.err
}

func TestUploadSuccess(t *testing.T) {
	fixedID := uuid.MustParse("f2a2f1be-4f7b-4a6d-b1a4-0e2b3c2be48d")
	fileStore := &fakeFileStore{}
	objectStore := &fakeObjectStore{}
	health := &fakeHealth{}

	router := newTestRouter(fileStore, objectStore, health, Options{
		MaxFileSize: 1024,
		NewID: func() uuid.UUID {
			return fixedID
		},
	})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	partHeader := textproto.MIMEHeader{}
	partHeader.Set("Content-Disposition", `form-data; name="file"; filename="hello.txt"`)
	partHeader.Set("Content-Type", "text/plain")
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	content := []byte("hello world")
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/files", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, rec.Code)
	}

	var resp uploadResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != fixedID.String() {
		t.Fatalf("expected id %s, got %s", fixedID, resp.ID)
	}
	if resp.ContentType != "text/plain" {
		t.Fatalf("expected content type text/plain, got %s", resp.ContentType)
	}
	if resp.SizeBytes != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), resp.SizeBytes)
	}

	if objectStore.putCalls != 1 {
		t.Fatalf("expected object store put to be called")
	}
	if objectStore.putKey != fixedID.String() {
		t.Fatalf("expected object key %s, got %s", fixedID, objectStore.putKey)
	}
	if objectStore.putSize != int64(len(content)) {
		t.Fatalf("expected object size %d, got %d", len(content), objectStore.putSize)
	}
	if objectStore.putContentType != "text/plain" {
		t.Fatalf("expected content type text/plain, got %s", objectStore.putContentType)
	}

	if fileStore.createCalls != 1 {
		t.Fatalf("expected create file called")
	}
	if fileStore.created.ID != fixedID {
		t.Fatalf("expected record id %s, got %s", fixedID, fileStore.created.ID)
	}
}

func TestUploadRejectsContentType(t *testing.T) {
	fileStore := &fakeFileStore{}
	objectStore := &fakeObjectStore{}
	health := &fakeHealth{}
	router := newTestRouter(fileStore, objectStore, health, Options{MaxFileSize: 1024})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	partHeader := textproto.MIMEHeader{}
	partHeader.Set("Content-Disposition", `form-data; name="file"; filename="hello.bin"`)
	partHeader.Set("Content-Type", "application/octet-stream")
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	if _, err := part.Write([]byte("data")); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/files", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected status %d, got %d", http.StatusUnsupportedMediaType, rec.Code)
	}

	if objectStore.putCalls != 0 {
		t.Fatalf("expected object store not called")
	}
	if fileStore.createCalls != 0 {
		t.Fatalf("expected file store not called")
	}
}

func TestGetMetadataNotFound(t *testing.T) {
	fileStore := &fakeFileStore{getErr: filestore.ErrFileNotFound}
	objectStore := &fakeObjectStore{}
	health := &fakeHealth{}
	router := newTestRouter(fileStore, objectStore, health, Options{})

	id := uuid.MustParse("2c0d5d9d-7c5f-4a7b-8a73-3ae59b69d4e7")
	req := httptest.NewRequest(http.MethodGet, "/files/"+id.String(), nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestGetMetadataSuccess(t *testing.T) {
	createdAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("a65d2d1a-e8c7-4bc8-9f62-76e171640343")
	fileStore := &fakeFileStore{
		getRecord: model.FileRecord{
			ID:          id,
			Filename:    "notes.txt",
			ContentType: "text/plain",
			SizeBytes:   10,
			CreatedAt:   createdAt,
		},
	}
	objectStore := &fakeObjectStore{}
	health := &fakeHealth{}
	router := newTestRouter(fileStore, objectStore, health, Options{})

	req := httptest.NewRequest(http.MethodGet, "/files/"+id.String(), nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp metadataResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != id.String() {
		t.Fatalf("expected id %s, got %s", id, resp.ID)
	}
	if !resp.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected created_at %v, got %v", createdAt, resp.CreatedAt)
	}
}

func TestGetURLSuccess(t *testing.T) {
	baseTime := time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC)
	id := uuid.MustParse("2c1c1be6-78d7-44d1-9bb6-f32a83c3b642")
	fileStore := &fakeFileStore{getRecord: model.FileRecord{ID: id}}
	objectStore := &fakeObjectStore{presignURL: "https://example.com/file"}
	health := &fakeHealth{}
	router := newTestRouter(fileStore, objectStore, health, Options{
		Now: func() time.Time {
			return baseTime
		},
		URLExpiry: time.Hour,
	})

	req := httptest.NewRequest(http.MethodGet, "/files/"+id.String()+"/url", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp urlResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.URL != objectStore.presignURL {
		t.Fatalf("expected url %s, got %s", objectStore.presignURL, resp.URL)
	}
	expectedExpiry := baseTime.Add(time.Hour)
	if !resp.ExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("expected expiry %v, got %v", expectedExpiry, resp.ExpiresAt)
	}
	if objectStore.presignKey != id.String() {
		t.Fatalf("expected presign key %s, got %s", id, objectStore.presignKey)
	}
	if objectStore.presignExpiry != time.Hour {
		t.Fatalf("expected expiry %v, got %v", time.Hour, objectStore.presignExpiry)
	}
}

func newTestRouter(fileStore *fakeFileStore, objectStore *fakeObjectStore, health *fakeHealth, opts Options) http.Handler {
	h := New(fileStore, objectStore, health, opts)
	router := chi.NewRouter()
	h.RegisterRoutes(router)
	return router
}
