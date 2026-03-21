package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
)

const (
	tenantIDKey     = "x-agyn-tenant-id"
	identityIDKey   = "x-agyn-identity-id"
	identityTypeKey = "x-agyn-identity-type"
	authMethodKey   = "x-agyn-auth-method"
)

type Identity struct {
	TenantID     uuid.UUID
	IdentityID   string
	IdentityType string
	AuthMethod   string
}

func FromContext(ctx context.Context) (Identity, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return Identity{}, errors.New("identity metadata is required")
	}

	tenantValue, err := requiredValue(md, tenantIDKey)
	if err != nil {
		return Identity{}, err
	}
	tenantID, err := uuid.Parse(tenantValue)
	if err != nil {
		return Identity{}, fmt.Errorf("tenant_id must be a valid UUID")
	}

	identityID, err := requiredValue(md, identityIDKey)
	if err != nil {
		return Identity{}, err
	}
	identityType, err := requiredValue(md, identityTypeKey)
	if err != nil {
		return Identity{}, err
	}
	authMethod, err := requiredValue(md, authMethodKey)
	if err != nil {
		return Identity{}, err
	}

	return Identity{
		TenantID:     tenantID,
		IdentityID:   identityID,
		IdentityType: identityType,
		AuthMethod:   authMethod,
	}, nil
}

func requiredValue(md metadata.MD, key string) (string, error) {
	values := md.Get(key)
	if len(values) == 0 {
		return "", fmt.Errorf("%s metadata is required", key)
	}
	value := strings.TrimSpace(values[0])
	if value == "" {
		return "", fmt.Errorf("%s metadata is required", key)
	}
	return value, nil
}
