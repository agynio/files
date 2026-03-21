package identity

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
)

func TestFromContext(t *testing.T) {
	tenantID := uuid.MustParse("6b93bede-1e0c-4e83-a3b1-4f1ca5f68493")
	validIdentity := Identity{
		TenantID:     tenantID,
		IdentityID:   "identity-123",
		IdentityType: "user",
		AuthMethod:   "test",
	}
	base := baseMetadata(validIdentity)

	tests := []struct {
		name    string
		ctx     context.Context
		want    Identity
		wantErr string
	}{
		{
			name: "valid",
			ctx:  metadata.NewIncomingContext(context.Background(), base),
			want: validIdentity,
		},
		{
			name:    "missing metadata",
			ctx:     context.Background(),
			wantErr: "identity metadata is required",
		},
		{
			name:    "missing tenant id",
			ctx:     metadata.NewIncomingContext(context.Background(), withoutKey(base, MetadataKeyTenantID)),
			wantErr: fmt.Sprintf("%s metadata is required", MetadataKeyTenantID),
		},
		{
			name:    "missing identity id",
			ctx:     metadata.NewIncomingContext(context.Background(), withoutKey(base, MetadataKeyIdentityID)),
			wantErr: fmt.Sprintf("%s metadata is required", MetadataKeyIdentityID),
		},
		{
			name:    "missing identity type",
			ctx:     metadata.NewIncomingContext(context.Background(), withoutKey(base, MetadataKeyIdentityType)),
			wantErr: fmt.Sprintf("%s metadata is required", MetadataKeyIdentityType),
		},
		{
			name:    "missing auth method",
			ctx:     metadata.NewIncomingContext(context.Background(), withoutKey(base, MetadataKeyAuthMethod)),
			wantErr: fmt.Sprintf("%s metadata is required", MetadataKeyAuthMethod),
		},
		{
			name:    "empty tenant id",
			ctx:     metadata.NewIncomingContext(context.Background(), withValue(base, MetadataKeyTenantID, " ")),
			wantErr: fmt.Sprintf("%s metadata is required", MetadataKeyTenantID),
		},
		{
			name:    "empty identity id",
			ctx:     metadata.NewIncomingContext(context.Background(), withValue(base, MetadataKeyIdentityID, "")),
			wantErr: fmt.Sprintf("%s metadata is required", MetadataKeyIdentityID),
		},
		{
			name:    "empty identity type",
			ctx:     metadata.NewIncomingContext(context.Background(), withValue(base, MetadataKeyIdentityType, "\t")),
			wantErr: fmt.Sprintf("%s metadata is required", MetadataKeyIdentityType),
		},
		{
			name:    "empty auth method",
			ctx:     metadata.NewIncomingContext(context.Background(), withValue(base, MetadataKeyAuthMethod, "")),
			wantErr: fmt.Sprintf("%s metadata is required", MetadataKeyAuthMethod),
		},
		{
			name:    "invalid tenant id",
			ctx:     metadata.NewIncomingContext(context.Background(), withValue(base, MetadataKeyTenantID, "not-a-uuid")),
			wantErr: "tenant_id must be a valid UUID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromContext(tt.ctx)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %+v, got %+v", tt.want, got)
			}
		})
	}
}

func baseMetadata(identity Identity) metadata.MD {
	return metadata.Pairs(
		MetadataKeyTenantID, identity.TenantID.String(),
		MetadataKeyIdentityID, identity.IdentityID,
		MetadataKeyIdentityType, identity.IdentityType,
		MetadataKeyAuthMethod, identity.AuthMethod,
	)
}

func withValue(md metadata.MD, key, value string) metadata.MD {
	clone := cloneMetadata(md)
	clone[key] = []string{value}
	return clone
}

func withoutKey(md metadata.MD, key string) metadata.MD {
	clone := cloneMetadata(md)
	delete(clone, key)
	return clone
}

func cloneMetadata(md metadata.MD) metadata.MD {
	clone := metadata.MD{}
	for key, values := range md {
		copied := make([]string, len(values))
		copy(copied, values)
		clone[key] = copied
	}
	return clone
}
