package model

import (
	"time"

	"github.com/google/uuid"
)

type FileRecord struct {
	ID          uuid.UUID
	Filename    string
	ContentType string
	SizeBytes   int64
	CreatedAt   time.Time
}
