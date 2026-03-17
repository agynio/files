//go:build e2e

package e2e

import (
	"context"
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
