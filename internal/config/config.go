package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/agynio/files/internal/filetype"
)

const (
	defaultHTTPAddress = ":8080"
	defaultGRPCAddress = ":9090"
	defaultS3Region    = "us-east-1"
)

type Config struct {
	HTTPAddress string
	GRPCAddress string
	DatabaseURL string
	S3Endpoint  string
	S3Bucket    string
	S3Region    string
	S3AccessKey string
	S3SecretKey string
	S3UseSSL    bool
	MaxFileSize int64
}

func FromEnv() (Config, error) {
	cfg := Config{}

	cfg.HTTPAddress = strings.TrimSpace(os.Getenv("HTTP_ADDRESS"))
	if cfg.HTTPAddress == "" {
		cfg.HTTPAddress = defaultHTTPAddress
	}

	cfg.GRPCAddress = strings.TrimSpace(os.Getenv("GRPC_ADDRESS"))
	if cfg.GRPCAddress == "" {
		cfg.GRPCAddress = defaultGRPCAddress
	}

	var err error
	cfg.DatabaseURL, err = requiredEnv("DATABASE_URL")
	if err != nil {
		return Config{}, err
	}

	cfg.S3Endpoint, err = requiredEnv("S3_ENDPOINT")
	if err != nil {
		return Config{}, err
	}
	if strings.Contains(cfg.S3Endpoint, "://") {
		return Config{}, fmt.Errorf("S3_ENDPOINT must not include a scheme")
	}

	cfg.S3Bucket, err = requiredEnv("S3_BUCKET")
	if err != nil {
		return Config{}, err
	}

	cfg.S3AccessKey, err = requiredEnv("S3_ACCESS_KEY")
	if err != nil {
		return Config{}, err
	}

	cfg.S3SecretKey, err = requiredEnv("S3_SECRET_KEY")
	if err != nil {
		return Config{}, err
	}

	cfg.S3Region = strings.TrimSpace(os.Getenv("S3_REGION"))
	if cfg.S3Region == "" {
		cfg.S3Region = defaultS3Region
	}

	useSSL := strings.TrimSpace(os.Getenv("S3_USE_SSL"))
	if useSSL != "" {
		parsed, err := strconv.ParseBool(useSSL)
		if err != nil {
			return Config{}, fmt.Errorf("S3_USE_SSL must be a boolean")
		}
		cfg.S3UseSSL = parsed
	}

	maxSize := strings.TrimSpace(os.Getenv("MAX_FILE_SIZE"))
	if maxSize == "" {
		cfg.MaxFileSize = filetype.DefaultMaxFileSize
	} else {
		parsed, err := strconv.ParseInt(maxSize, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("MAX_FILE_SIZE must be an integer")
		}
		if parsed <= 0 {
			return Config{}, fmt.Errorf("MAX_FILE_SIZE must be positive")
		}
		cfg.MaxFileSize = parsed
	}

	return cfg, nil
}

func requiredEnv(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s must be set", name)
	}
	return value, nil
}
