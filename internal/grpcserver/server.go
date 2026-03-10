package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	filesv1 "github.com/agynio/files/gen/go/agynio/api/files/v1"
	"github.com/agynio/files/internal/filetype"
	"github.com/agynio/files/internal/model"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	maxChunkSize = 256 * 1024
)

type FileStore interface {
	CreateFile(ctx context.Context, record model.FileRecord) error
}

type ObjectStore interface {
	PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
}

type Options struct {
	MaxFileSize int64
	Now         func() time.Time
	NewID       func() uuid.UUID
}

type Server struct {
	filesv1.UnimplementedFilesServiceServer
	store       FileStore
	objectStore ObjectStore
	maxFileSize int64
	now         func() time.Time
	newID       func() uuid.UUID
}

func New(store FileStore, objectStore ObjectStore, opts Options) *Server {
	maxSize := opts.MaxFileSize
	if maxSize <= 0 {
		maxSize = filetype.DefaultMaxFileSize
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	newID := opts.NewID
	if newID == nil {
		newID = uuid.New
	}
	return &Server{
		store:       store,
		objectStore: objectStore,
		maxFileSize: maxSize,
		now:         now,
		newID:       newID,
	}
}

func (s *Server) UploadFile(stream filesv1.FilesService_UploadFileServer) error {
	ctx := stream.Context()

	first, err := stream.Recv()
	if err == io.EOF {
		return status.Error(codes.InvalidArgument, "metadata is required")
	}
	if err != nil {
		return status.Errorf(codes.Internal, "receive metadata: %v", err)
	}
	metadata := first.GetMetadata()
	if metadata == nil {
		return status.Error(codes.InvalidArgument, "metadata is required")
	}

	filename := strings.TrimSpace(metadata.GetFilename())
	if filename == "" {
		return status.Error(codes.InvalidArgument, "filename is required")
	}

	contentType, err := filetype.ParseContentType(metadata.GetContentType())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "content_type: %v", err)
	}
	if !filetype.IsAllowedContentType(contentType) {
		return status.Error(codes.InvalidArgument, "content_type is not allowed")
	}

	sizeBytes := metadata.GetSizeBytes()
	if sizeBytes <= 0 {
		return status.Error(codes.InvalidArgument, "size_bytes must be positive")
	}
	if sizeBytes > s.maxFileSize {
		return status.Error(codes.ResourceExhausted, "file exceeds max size")
	}

	id := s.newID()
	key := id.String()

	reader, writer := io.Pipe()
	var putErr error
	done := make(chan struct{})
	go func() {
		putErr = s.objectStore.PutObject(ctx, key, reader, sizeBytes, contentType)
		close(done)
	}()
	waitUpload := func() error {
		<-done
		return putErr
	}
	abortUpload := func(cause error, statusErr error) error {
		_ = writer.CloseWithError(cause)
		_ = waitUpload()
		return statusErr
	}

	received := int64(0)
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return abortUpload(err, status.Errorf(codes.Internal, "receive chunk: %v", err))
		}
		if msg.GetMetadata() != nil {
			return abortUpload(errors.New("metadata sent after initial message"), status.Error(codes.InvalidArgument, "metadata must be the first message"))
		}
		chunk := msg.GetChunk()
		if chunk == nil {
			return abortUpload(errors.New("chunk missing"), status.Error(codes.InvalidArgument, "chunk is required"))
		}
		data := chunk.GetData()
		if len(data) > maxChunkSize {
			return abortUpload(errors.New("chunk exceeds max size"), status.Error(codes.InvalidArgument, "chunk exceeds max size"))
		}
		nextSize := received + int64(len(data))
		if nextSize > sizeBytes {
			return abortUpload(errors.New("file exceeds declared size"), status.Error(codes.InvalidArgument, "file exceeds declared size"))
		}
		if len(data) > 0 {
			if _, err := writer.Write(data); err != nil {
				return abortUpload(err, status.Errorf(codes.Internal, "stream to storage: %v", err))
			}
		}
		received = nextSize
	}

	if received != sizeBytes {
		return abortUpload(fmt.Errorf("expected %d bytes", sizeBytes), status.Error(codes.InvalidArgument, "file size does not match metadata"))
	}
	_ = writer.Close()
	if err := waitUpload(); err != nil {
		return status.Errorf(codes.Internal, "store file: %v", err)
	}

	createdAt := s.now().UTC()
	record := model.FileRecord{
		ID:          id,
		Filename:    filename,
		ContentType: contentType,
		SizeBytes:   sizeBytes,
		CreatedAt:   createdAt,
	}
	if err := s.store.CreateFile(ctx, record); err != nil {
		return status.Errorf(codes.Internal, "persist metadata: %v", err)
	}
	resp := &filesv1.UploadFileResponse{File: toProtoFileInfo(record)}
	return stream.SendAndClose(resp)
}

func toProtoFileInfo(record model.FileRecord) *filesv1.FileInfo {
	return &filesv1.FileInfo{
		Id:          record.ID.String(),
		Filename:    record.Filename,
		ContentType: record.ContentType,
		SizeBytes:   record.SizeBytes,
		CreatedAt:   timestamppb.New(record.CreatedAt.UTC()),
	}
}
