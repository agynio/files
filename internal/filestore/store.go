package filestore

import (
	"context"
	"errors"
	"fmt"

	"github.com/agynio/files/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrFileNotFound = errors.New("file not found")

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) CreateFile(ctx context.Context, record model.FileRecord) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO files (id, filename, content_type, size_bytes) VALUES ($1, $2, $3, $4)`, record.ID, record.Filename, record.ContentType, record.SizeBytes)
	if err != nil {
		return fmt.Errorf("insert file: %w", err)
	}
	return nil
}

func (s *Store) GetFile(ctx context.Context, id uuid.UUID) (model.FileRecord, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, filename, content_type, size_bytes, created_at FROM files WHERE id = $1`, id)
	var record model.FileRecord
	if err := row.Scan(&record.ID, &record.Filename, &record.ContentType, &record.SizeBytes, &record.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.FileRecord{}, ErrFileNotFound
		}
		return model.FileRecord{}, fmt.Errorf("get file: %w", err)
	}
	return record, nil
}
