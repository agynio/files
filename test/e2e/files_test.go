//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	filesv1 "github.com/agynio/files/.gen/go/agynio/api/files/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestGetFileMetadataRequiresID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(filesAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial files: %v", err)
	}
	defer conn.Close()

	client := filesv1.NewFilesServiceClient(conn)

	_, err = client.GetFileMetadata(ctx, &filesv1.GetFileMetadataRequest{})
	if err == nil {
		t.Fatal("expected invalid argument error")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %s: %s", st.Code(), st.Message())
	}
}

func TestGetFileContentRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(filesAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial files: %v", err)
	}
	defer conn.Close()

	client := filesv1.NewFilesServiceClient(conn)
	content := []byte("hello world")

	uploadStream, err := client.UploadFile(ctx)
	if err != nil {
		t.Fatalf("start upload: %v", err)
	}
	metadata := &filesv1.UploadFileRequest{
		Payload: &filesv1.UploadFileRequest_Metadata{
			Metadata: &filesv1.UploadFileMetadata{
				Filename:    "hello.txt",
				ContentType: "text/plain",
				SizeBytes:   int64(len(content)),
			},
		},
	}
	if err := uploadStream.Send(metadata); err != nil {
		t.Fatalf("send metadata: %v", err)
	}
	if err := uploadStream.Send(&filesv1.UploadFileRequest{
		Payload: &filesv1.UploadFileRequest_Chunk{
			Chunk: &filesv1.UploadFileChunk{Data: content[:5]},
		},
	}); err != nil {
		t.Fatalf("send chunk: %v", err)
	}
	if err := uploadStream.Send(&filesv1.UploadFileRequest{
		Payload: &filesv1.UploadFileRequest_Chunk{
			Chunk: &filesv1.UploadFileChunk{Data: content[5:]},
		},
	}); err != nil {
		t.Fatalf("send chunk: %v", err)
	}

	uploadResp, err := uploadStream.CloseAndRecv()
	if err != nil {
		t.Fatalf("finish upload: %v", err)
	}
	if uploadResp.GetFile() == nil || uploadResp.GetFile().GetId() == "" {
		t.Fatalf("expected file id in upload response")
	}

	downloadStream, err := client.GetFileContent(ctx, &filesv1.GetFileContentRequest{FileId: uploadResp.File.Id})
	if err != nil {
		t.Fatalf("get file content: %v", err)
	}
	var received []byte
	for {
		resp, err := downloadStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("receive chunk: %v", err)
		}
		received = append(received, resp.GetChunkData()...)
	}
	if !bytes.Equal(received, content) {
		t.Fatalf("downloaded content does not match")
	}
}
