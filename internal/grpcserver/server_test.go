package grpcserver

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	filesv1 "github.com/agynio/files/.gen/go/agynio/api/files/v1"
	"github.com/agynio/files/internal/filestore"
	"github.com/agynio/files/internal/model"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

type fakeFileStore struct {
	created     model.FileRecord
	createErr   error
	createCalls int
	getRecord   model.FileRecord
	getErr      error
	getCalls    int
	getID       uuid.UUID
}

func (f *fakeFileStore) CreateFile(ctx context.Context, record model.FileRecord) error {
	f.createCalls++
	f.created = record
	return f.createErr
}

func (f *fakeFileStore) GetFile(ctx context.Context, id uuid.UUID) (model.FileRecord, error) {
	f.getCalls++
	f.getID = id
	if f.getErr != nil {
		return model.FileRecord{}, f.getErr
	}
	return f.getRecord, nil
}

type fakeObjectStore struct {
	data          []byte
	key           string
	size          int64
	contentType   string
	putCalls      int
	putErr        error
	presignURL    string
	presignExpiry time.Duration
	presignKey    string
	presignCalls  int
	presignErr    error
}

func (f *fakeObjectStore) PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	f.putCalls++
	f.key = key
	f.size = size
	f.contentType = contentType
	data, err := io.ReadAll(reader)
	f.data = data
	if err != nil {
		return err
	}
	if f.putErr != nil {
		return f.putErr
	}
	return nil
}

func (f *fakeObjectStore) PresignGetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	f.presignCalls++
	f.presignKey = key
	f.presignExpiry = expiry
	if f.presignErr != nil {
		return "", f.presignErr
	}
	return f.presignURL, nil
}

type fakeUploadStream struct {
	ctx       context.Context
	requests  []*filesv1.UploadFileRequest
	resp      *filesv1.UploadFileResponse
	recvIndex int
}

func (f *fakeUploadStream) Recv() (*filesv1.UploadFileRequest, error) {
	if f.recvIndex >= len(f.requests) {
		return nil, io.EOF
	}
	req := f.requests[f.recvIndex]
	f.recvIndex++
	return req, nil
}

func (f *fakeUploadStream) SendAndClose(resp *filesv1.UploadFileResponse) error {
	f.resp = resp
	return nil
}

func (f *fakeUploadStream) SetHeader(metadata.MD) error  { return nil }
func (f *fakeUploadStream) SendHeader(metadata.MD) error { return nil }
func (f *fakeUploadStream) SetTrailer(metadata.MD)       {}
func (f *fakeUploadStream) Context() context.Context     { return f.ctx }
func (f *fakeUploadStream) SendMsg(any) error            { return nil }
func (f *fakeUploadStream) RecvMsg(any) error            { return nil }

func TestUploadFileSuccess(t *testing.T) {
	fixedID := uuid.MustParse("5b49f320-28ba-4f73-9c38-d4f371a6a4be")
	fixedTime := time.Date(2025, 4, 6, 7, 8, 9, 0, time.UTC)
	store := &fakeFileStore{}
	objectStore := &fakeObjectStore{}
	server := New(store, objectStore, Options{
		MaxFileSize: 1024,
		Now: func() time.Time {
			return fixedTime
		},
		NewID: func() uuid.UUID {
			return fixedID
		},
	})

	chunkA := []byte("hello ")
	chunkB := []byte("world")
	stream := &fakeUploadStream{
		ctx: context.Background(),
		requests: []*filesv1.UploadFileRequest{
			metadataRequest("greeting.txt", "text/plain", int64(len(chunkA)+len(chunkB))),
			chunkRequest(chunkA),
			chunkRequest(chunkB),
		},
	}

	if err := server.UploadFile(stream); err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if stream.resp == nil || stream.resp.File == nil {
		t.Fatalf("expected response file")
	}
	if stream.resp.File.Id != fixedID.String() {
		t.Fatalf("expected id %s, got %s", fixedID, stream.resp.File.Id)
	}
	if stream.resp.File.ContentType != "text/plain" {
		t.Fatalf("expected content type text/plain, got %s", stream.resp.File.ContentType)
	}
	if got := stream.resp.File.CreatedAt.AsTime(); !got.Equal(fixedTime) {
		t.Fatalf("expected created_at %v, got %v", fixedTime, got)
	}

	if objectStore.putCalls != 1 {
		t.Fatalf("expected object store put call")
	}
	if objectStore.key != fixedID.String() {
		t.Fatalf("expected key %s, got %s", fixedID, objectStore.key)
	}
	if objectStore.size != int64(len(chunkA)+len(chunkB)) {
		t.Fatalf("expected size %d, got %d", len(chunkA)+len(chunkB), objectStore.size)
	}
	if objectStore.contentType != "text/plain" {
		t.Fatalf("expected content type text/plain, got %s", objectStore.contentType)
	}
	if string(objectStore.data) != "hello world" {
		t.Fatalf("unexpected stored data: %s", string(objectStore.data))
	}

	if store.createCalls != 1 {
		t.Fatalf("expected create file called")
	}
	if store.created.CreatedAt != fixedTime {
		t.Fatalf("expected created_at %v, got %v", fixedTime, store.created.CreatedAt)
	}
}

func TestUploadFileMissingMetadata(t *testing.T) {
	store := &fakeFileStore{}
	objectStore := &fakeObjectStore{}
	server := New(store, objectStore, Options{MaxFileSize: 1024})

	stream := &fakeUploadStream{ctx: context.Background()}

	err := server.UploadFile(stream)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if objectStore.putCalls != 0 {
		t.Fatalf("expected no object store calls")
	}
	if store.createCalls != 0 {
		t.Fatalf("expected no create file calls")
	}
}

func TestUploadFileInvalidMetadata(t *testing.T) {
	store := &fakeFileStore{}
	objectStore := &fakeObjectStore{}
	server := New(store, objectStore, Options{MaxFileSize: 1024})

	stream := &fakeUploadStream{
		ctx: context.Background(),
		requests: []*filesv1.UploadFileRequest{
			metadataRequest("bad.txt", "not-a-type", 4),
		},
	}

	err := server.UploadFile(stream)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if objectStore.putCalls != 0 {
		t.Fatalf("expected no object store calls")
	}
	if store.createCalls != 0 {
		t.Fatalf("expected no create file calls")
	}
}

func TestUploadFileChunkTooLarge(t *testing.T) {
	store := &fakeFileStore{}
	objectStore := &fakeObjectStore{}
	server := New(store, objectStore, Options{MaxFileSize: int64(maxChunkSize) + 1024})

	data := make([]byte, maxChunkSize+1)
	stream := &fakeUploadStream{
		ctx: context.Background(),
		requests: []*filesv1.UploadFileRequest{
			metadataRequest("big.bin", "application/pdf", int64(len(data))),
			chunkRequest(data),
		},
	}

	err := server.UploadFile(stream)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("expected no create file calls")
	}
}

func TestUploadFileDeclaredSizeTooSmall(t *testing.T) {
	store := &fakeFileStore{}
	objectStore := &fakeObjectStore{}
	server := New(store, objectStore, Options{MaxFileSize: 1024})

	data := []byte("data")
	stream := &fakeUploadStream{
		ctx: context.Background(),
		requests: []*filesv1.UploadFileRequest{
			metadataRequest("data.txt", "text/plain", int64(len(data)-1)),
			chunkRequest(data),
		},
	}

	err := server.UploadFile(stream)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("expected no create file calls")
	}
}

func TestUploadFileDeclaredSizeTooLarge(t *testing.T) {
	store := &fakeFileStore{}
	objectStore := &fakeObjectStore{}
	server := New(store, objectStore, Options{MaxFileSize: 1024})

	data := []byte("data")
	stream := &fakeUploadStream{
		ctx: context.Background(),
		requests: []*filesv1.UploadFileRequest{
			metadataRequest("data.txt", "text/plain", int64(len(data)+1)),
			chunkRequest(data),
		},
	}

	err := server.UploadFile(stream)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("expected no create file calls")
	}
}

func TestUploadFileObjectStoreFailure(t *testing.T) {
	store := &fakeFileStore{}
	objectStore := &fakeObjectStore{putErr: errors.New("boom")}
	server := New(store, objectStore, Options{MaxFileSize: 1024})

	data := []byte("data")
	stream := &fakeUploadStream{
		ctx: context.Background(),
		requests: []*filesv1.UploadFileRequest{
			metadataRequest("data.txt", "text/plain", int64(len(data))),
			chunkRequest(data),
		},
	}

	err := server.UploadFile(stream)
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected internal error, got %v", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("expected no create file calls")
	}
}

func TestGetFileMetadataSuccess(t *testing.T) {
	fileID := uuid.MustParse("2d2f1af2-2dc7-4bc7-9f6d-e4d30aa3e2d6")
	createdAt := time.Date(2025, 5, 6, 7, 8, 9, 0, time.UTC)
	store := &fakeFileStore{getRecord: model.FileRecord{
		ID:          fileID,
		Filename:    "report.pdf",
		ContentType: "application/pdf",
		SizeBytes:   2048,
		CreatedAt:   createdAt,
	}}
	objectStore := &fakeObjectStore{}
	server := New(store, objectStore, Options{MaxFileSize: 1024})

	resp, err := server.GetFileMetadata(context.Background(), &filesv1.GetFileMetadataRequest{FileId: fileID.String()})
	if err != nil {
		t.Fatalf("get file metadata: %v", err)
	}
	if resp.File == nil {
		t.Fatalf("expected file info")
	}
	if resp.File.Id != fileID.String() {
		t.Fatalf("expected id %s, got %s", fileID, resp.File.Id)
	}
	if got := resp.File.CreatedAt.AsTime(); !got.Equal(createdAt) {
		t.Fatalf("expected created_at %v, got %v", createdAt, got)
	}
	if store.getCalls != 1 {
		t.Fatalf("expected get file called once")
	}
}

func TestGetFileMetadataNotFound(t *testing.T) {
	store := &fakeFileStore{getErr: filestore.ErrFileNotFound}
	server := New(store, &fakeObjectStore{}, Options{MaxFileSize: 1024})

	_, err := server.GetFileMetadata(context.Background(), &filesv1.GetFileMetadataRequest{FileId: uuid.NewString()})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestGetFileMetadataInvalidUUID(t *testing.T) {
	store := &fakeFileStore{}
	server := New(store, &fakeObjectStore{}, Options{MaxFileSize: 1024})

	_, err := server.GetFileMetadata(context.Background(), &filesv1.GetFileMetadataRequest{FileId: "nope"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if store.getCalls != 0 {
		t.Fatalf("expected no get file calls")
	}
}

func TestGetFileMetadataEmptyFileId(t *testing.T) {
	store := &fakeFileStore{}
	server := New(store, &fakeObjectStore{}, Options{MaxFileSize: 1024})

	_, err := server.GetFileMetadata(context.Background(), &filesv1.GetFileMetadataRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if store.getCalls != 0 {
		t.Fatalf("expected no get file calls")
	}
}

func TestGetFileMetadataStoreError(t *testing.T) {
	store := &fakeFileStore{getErr: errors.New("boom")}
	server := New(store, &fakeObjectStore{}, Options{MaxFileSize: 1024})

	_, err := server.GetFileMetadata(context.Background(), &filesv1.GetFileMetadataRequest{FileId: uuid.NewString()})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected internal error, got %v", err)
	}
}

func TestGetDownloadUrlSuccess(t *testing.T) {
	fileID := uuid.MustParse("ab7f1810-1d10-4fe0-b4bb-d1b9f3859862")
	fixedNow := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)
	store := &fakeFileStore{getRecord: model.FileRecord{ID: fileID}}
	objectStore := &fakeObjectStore{presignURL: "https://example.com/file"}
	server := New(store, objectStore, Options{
		MaxFileSize: 1024,
		Now: func() time.Time {
			return fixedNow
		},
		URLExpiry: 2 * time.Hour,
	})

	resp, err := server.GetDownloadUrl(context.Background(), &filesv1.GetDownloadUrlRequest{FileId: fileID.String()})
	if err != nil {
		t.Fatalf("get download url: %v", err)
	}
	if resp.Url != "https://example.com/file" {
		t.Fatalf("expected url, got %s", resp.Url)
	}
	if got := resp.ExpiresAt.AsTime(); !got.Equal(fixedNow.Add(2 * time.Hour)) {
		t.Fatalf("expected expires_at %v, got %v", fixedNow.Add(2*time.Hour), got)
	}
	if objectStore.presignExpiry != 2*time.Hour {
		t.Fatalf("expected expiry 2h, got %v", objectStore.presignExpiry)
	}
}

func TestGetDownloadUrlCustomExpiry(t *testing.T) {
	fileID := uuid.MustParse("e49e9cad-6d12-4d07-8430-00a5cdbf226d")
	fixedNow := time.Date(2025, 7, 8, 9, 10, 11, 0, time.UTC)
	store := &fakeFileStore{getRecord: model.FileRecord{ID: fileID}}
	objectStore := &fakeObjectStore{presignURL: "https://example.com/custom"}
	server := New(store, objectStore, Options{
		MaxFileSize: 1024,
		Now: func() time.Time {
			return fixedNow
		},
		URLExpiry: time.Hour,
	})

	resp, err := server.GetDownloadUrl(context.Background(), &filesv1.GetDownloadUrlRequest{
		FileId: fileID.String(),
		Expiry: durationpb.New(30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("get download url: %v", err)
	}
	if objectStore.presignExpiry != 30*time.Minute {
		t.Fatalf("expected expiry 30m, got %v", objectStore.presignExpiry)
	}
	if got := resp.ExpiresAt.AsTime(); !got.Equal(fixedNow.Add(30 * time.Minute)) {
		t.Fatalf("expected expires_at %v, got %v", fixedNow.Add(30*time.Minute), got)
	}
}

func TestGetDownloadUrlNotFound(t *testing.T) {
	store := &fakeFileStore{getErr: filestore.ErrFileNotFound}
	server := New(store, &fakeObjectStore{}, Options{MaxFileSize: 1024})

	_, err := server.GetDownloadUrl(context.Background(), &filesv1.GetDownloadUrlRequest{FileId: uuid.NewString()})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestGetDownloadUrlInvalidUUID(t *testing.T) {
	store := &fakeFileStore{}
	server := New(store, &fakeObjectStore{}, Options{MaxFileSize: 1024})

	_, err := server.GetDownloadUrl(context.Background(), &filesv1.GetDownloadUrlRequest{FileId: "bad"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if store.getCalls != 0 {
		t.Fatalf("expected no get file calls")
	}
}

func TestGetDownloadUrlEmptyFileId(t *testing.T) {
	store := &fakeFileStore{}
	server := New(store, &fakeObjectStore{}, Options{MaxFileSize: 1024})

	_, err := server.GetDownloadUrl(context.Background(), &filesv1.GetDownloadUrlRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if store.getCalls != 0 {
		t.Fatalf("expected no get file calls")
	}
}

func TestGetDownloadUrlNegativeExpiry(t *testing.T) {
	fileID := uuid.MustParse("9ad18044-acde-4d31-8745-94e34bd0d33f")
	store := &fakeFileStore{}
	server := New(store, &fakeObjectStore{}, Options{MaxFileSize: 1024, URLExpiry: time.Hour})

	_, err := server.GetDownloadUrl(context.Background(), &filesv1.GetDownloadUrlRequest{
		FileId: fileID.String(),
		Expiry: durationpb.New(-5 * time.Minute),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestGetDownloadUrlExpiryExceedsMax(t *testing.T) {
	fileID := uuid.MustParse("2db5ef84-7346-4a3e-a2b0-2f11a3480d63")
	store := &fakeFileStore{}
	server := New(store, &fakeObjectStore{}, Options{MaxFileSize: 1024, URLExpiry: time.Hour})

	_, err := server.GetDownloadUrl(context.Background(), &filesv1.GetDownloadUrlRequest{
		FileId: fileID.String(),
		Expiry: durationpb.New(25 * time.Hour),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestGetDownloadUrlPresignError(t *testing.T) {
	fileID := uuid.MustParse("0c8b34ec-244b-4f7c-a0fe-7c08e58ef6c3")
	store := &fakeFileStore{getRecord: model.FileRecord{ID: fileID}}
	objectStore := &fakeObjectStore{presignErr: errors.New("boom")}
	server := New(store, objectStore, Options{MaxFileSize: 1024, URLExpiry: time.Hour})

	_, err := server.GetDownloadUrl(context.Background(), &filesv1.GetDownloadUrlRequest{FileId: fileID.String()})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected internal error, got %v", err)
	}
}

func TestGetDownloadUrlStoreError(t *testing.T) {
	fileID := uuid.MustParse("3241ae1e-66eb-48de-9391-3776c86a78ee")
	store := &fakeFileStore{getErr: errors.New("boom")}
	server := New(store, &fakeObjectStore{}, Options{MaxFileSize: 1024, URLExpiry: time.Hour})

	_, err := server.GetDownloadUrl(context.Background(), &filesv1.GetDownloadUrlRequest{FileId: fileID.String()})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected internal error, got %v", err)
	}
}

func metadataRequest(filename, contentType string, size int64) *filesv1.UploadFileRequest {
	return &filesv1.UploadFileRequest{
		Payload: &filesv1.UploadFileRequest_Metadata{
			Metadata: &filesv1.UploadFileMetadata{
				Filename:    filename,
				ContentType: contentType,
				SizeBytes:   size,
			},
		},
	}
}

func chunkRequest(data []byte) *filesv1.UploadFileRequest {
	return &filesv1.UploadFileRequest{
		Payload: &filesv1.UploadFileRequest_Chunk{
			Chunk: &filesv1.UploadFileChunk{Data: data},
		},
	}
}
