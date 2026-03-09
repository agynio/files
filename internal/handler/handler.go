package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/agynio/files/internal/filestore"
	"github.com/agynio/files/internal/model"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	defaultMaxFileSize = int64(20 * 1024 * 1024)
	defaultURLExpiry   = time.Hour
)

var allowedContentPrefixes = []string{
	"image/",
	"text/",
	"application/pdf",
	"application/msword",
	"application/vnd.openxmlformats-officedocument",
	"application/vnd.ms-excel",
	"application/vnd.ms-powerpoint",
	"application/json",
	"application/xml",
	"application/zip",
	"application/gzip",
	"application/x-tar",
}

type FileStore interface {
	CreateFile(ctx context.Context, record model.FileRecord) error
	GetFile(ctx context.Context, id uuid.UUID) (model.FileRecord, error)
}

type ObjectStore interface {
	PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	PresignGetURL(ctx context.Context, key string, expiry time.Duration) (string, error)
}

type HealthChecker interface {
	Ping(ctx context.Context) error
}

type Options struct {
	MaxFileSize int64
	URLExpiry   time.Duration
	Now         func() time.Time
	NewID       func() uuid.UUID
}

type Handler struct {
	store       FileStore
	objectStore ObjectStore
	health      HealthChecker
	maxFileSize int64
	urlExpiry   time.Duration
	now         func() time.Time
	newID       func() uuid.UUID
}

func New(store FileStore, objectStore ObjectStore, health HealthChecker, opts Options) *Handler {
	maxSize := opts.MaxFileSize
	if maxSize <= 0 {
		maxSize = defaultMaxFileSize
	}
	urlExpiry := opts.URLExpiry
	if urlExpiry <= 0 {
		urlExpiry = defaultURLExpiry
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	newID := opts.NewID
	if newID == nil {
		newID = uuid.New
	}
	return &Handler{
		store:       store,
		objectStore: objectStore,
		health:      health,
		maxFileSize: maxSize,
		urlExpiry:   urlExpiry,
		now:         now,
		newID:       newID,
	}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/files", h.Upload)
	r.Get("/files/{fileId}", h.GetMetadata)
	r.Get("/files/{fileId}/url", h.GetURL)
	r.Get("/healthz", h.Healthz)
}

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, h.maxFileSize)

	file, header, err := r.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "file exceeds max size")
			return
		}
		if errors.Is(err, http.ErrMissingFile) {
			writeError(w, http.StatusBadRequest, "file is required")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid multipart data")
		return
	}
	defer file.Close()

	if header.Size > h.maxFileSize {
		writeError(w, http.StatusRequestEntityTooLarge, "file exceeds max size")
		return
	}

	contentType, err := parseContentType(header.Header.Get("Content-Type"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !isAllowedContentType(contentType) {
		writeError(w, http.StatusUnsupportedMediaType, "content type is not allowed")
		return
	}

	id := h.newID()
	key := id.String()

	if err := h.objectStore.PutObject(ctx, key, file, header.Size, contentType); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store file")
		return
	}

	record := model.FileRecord{
		ID:          id,
		Filename:    header.Filename,
		ContentType: contentType,
		SizeBytes:   header.Size,
	}
	if err := h.store.CreateFile(ctx, record); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist metadata")
		return
	}

	writeJSON(w, http.StatusCreated, uploadResponse{
		ID:          record.ID.String(),
		Filename:    record.Filename,
		ContentType: record.ContentType,
		SizeBytes:   record.SizeBytes,
	})
}

func (h *Handler) GetMetadata(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUIDParam(r, "fileId")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	record, err := h.store.GetFile(ctx, id)
	if err != nil {
		if errors.Is(err, filestore.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to fetch metadata")
		return
	}

	writeJSON(w, http.StatusOK, metadataResponse{
		ID:          record.ID.String(),
		Filename:    record.Filename,
		ContentType: record.ContentType,
		SizeBytes:   record.SizeBytes,
		CreatedAt:   record.CreatedAt.UTC(),
	})
}

func (h *Handler) GetURL(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUUIDParam(r, "fileId")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if _, err := h.store.GetFile(ctx, id); err != nil {
		if errors.Is(err, filestore.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to fetch metadata")
		return
	}

	url, err := h.objectStore.PresignGetURL(ctx, id.String(), h.urlExpiry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate url")
		return
	}

	expiresAt := h.now().UTC().Add(h.urlExpiry)
	writeJSON(w, http.StatusOK, urlResponse{
		URL:       url,
		ExpiresAt: expiresAt,
	})
}

func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.health.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

type uploadResponse struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
}

type metadataResponse struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	SizeBytes   int64     `json:"sizeBytes"`
	CreatedAt   time.Time `json:"created_at"`
}

type urlResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type statusResponse struct {
	Status string `json:"status"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func parseUUIDParam(r *http.Request, name string) (uuid.UUID, error) {
	raw := chi.URLParam(r, name)
	if raw == "" {
		return uuid.Nil, fmt.Errorf("%s is required", name)
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s must be a valid UUID", name)
	}
	return id, nil
}

func parseContentType(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("content type is required")
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "", fmt.Errorf("content type is invalid")
	}
	return strings.ToLower(mediaType), nil
}

func isAllowedContentType(contentType string) bool {
	for _, prefix := range allowedContentPrefixes {
		if strings.HasPrefix(contentType, prefix) {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}
