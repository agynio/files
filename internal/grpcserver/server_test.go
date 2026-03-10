package grpcserver

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	filesv1 "github.com/agynio/files/gen/go/agynio/api/files/v1"
	"github.com/agynio/files/internal/model"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type fakeFileStore struct {
	created     model.FileRecord
	createErr   error
	createCalls int
}

func (f *fakeFileStore) CreateFile(ctx context.Context, record model.FileRecord) error {
	f.createCalls++
	f.created = record
	return f.createErr
}

type fakeObjectStore struct {
	data        []byte
	key         string
	size        int64
	contentType string
	putCalls    int
	err         error
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
	if f.err != nil {
		return f.err
	}
	return nil
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
	objectStore := &fakeObjectStore{err: errors.New("boom")}
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
