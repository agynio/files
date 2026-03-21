//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	filesv1 "github.com/agynio/files/gen/go/agynio/api/files/v1"
	"github.com/agynio/files/internal/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	testTenantID     = "6b93bede-1e0c-4e83-a3b1-4f1ca5f68493"
	testIdentityID   = "identity-123"
	testIdentityType = "user"
	testAuthMethod   = "test"
)

func identityContext(ctx context.Context) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(
		identity.MetadataKeyTenantID, testTenantID,
		identity.MetadataKeyIdentityID, testIdentityID,
		identity.MetadataKeyIdentityType, testIdentityType,
		identity.MetadataKeyAuthMethod, testAuthMethod,
	))
}

func TestGetFileMetadataRequiresID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = identityContext(ctx)

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
