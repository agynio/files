package filetype

import (
	"errors"
	"mime"
	"strings"
)

const DefaultMaxFileSize = int64(20 * 1024 * 1024)

var allowedContentPrefixes = []string{
	"image/",
	"text/",
	"application/pdf",
	"application/msword",
	"application/vnd.openxmlformats-officedocument",
	"application/vnd.ms-excel",
	"application/vnd.ms-powerpoint",
	"application/json",
	"application/xml",
	"application/zip",
	"application/gzip",
	"application/x-tar",
}

func ParseContentType(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("content type is required")
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "", errors.New("content type is invalid")
	}
	return strings.ToLower(mediaType), nil
}

func IsAllowedContentType(contentType string) bool {
	for _, prefix := range allowedContentPrefixes {
		if strings.HasPrefix(contentType, prefix) {
			return true
		}
	}
	return false
}
