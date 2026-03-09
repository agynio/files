package objectstore

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
)

type Store struct {
	client *minio.Client
	bucket string
}

func New(client *minio.Client, bucket string) *Store {
	return &Store{client: client, bucket: bucket}
}

func (s *Store) PutObject(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, reader, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

func (s *Store) PresignGetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	url, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign get url: %w", err)
	}
	return url.String(), nil
}
