package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	filesv1 "github.com/agynio/files/gen/go/agynio/api/files/v1"
	"github.com/agynio/files/internal/config"
	"github.com/agynio/files/internal/db"
	"github.com/agynio/files/internal/filestore"
	"github.com/agynio/files/internal/grpcserver"
	"github.com/agynio/files/internal/handler"
	"github.com/agynio/files/internal/objectstore"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"google.golang.org/grpc"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("files: %v", err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}
	defer pool.Close()

	if err := db.ApplyMigrations(ctx, pool); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	minioClient, err := minio.New(cfg.S3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.S3AccessKey, cfg.S3SecretKey, ""),
		Secure: cfg.S3UseSSL,
		Region: cfg.S3Region,
	})
	if err != nil {
		return fmt.Errorf("create s3 client: %w", err)
	}

	fileStore := filestore.New(pool)
	objectStore := objectstore.New(minioClient, cfg.S3Bucket)
	h := handler.New(fileStore, objectStore, pool, handler.Options{MaxFileSize: cfg.MaxFileSize, URLExpiry: time.Hour})

	grpcServer := grpc.NewServer()
	filesv1.RegisterFilesServiceServer(grpcServer, grpcserver.New(fileStore, objectStore, grpcserver.Options{MaxFileSize: cfg.MaxFileSize}))
	grpcListener, err := net.Listen("tcp", cfg.GRPCAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.GRPCAddress, err)
	}

	router := chi.NewRouter()
	router.Use(middleware.RequestID, middleware.RealIP, middleware.Logger, middleware.Recoverer)
	h.RegisterRoutes(router)

	server := &http.Server{
		Addr:         cfg.HTTPAddress,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 2)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		grpcServer.GracefulStop()
	}()

	go func() {
		log.Printf("Files HTTP listening on %s", cfg.HTTPAddress)
		if err := server.ListenAndServe(); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				errCh <- nil
				return
			}
			errCh <- fmt.Errorf("serve http: %w", err)
			return
		}
		errCh <- nil
	}()

	go func() {
		log.Printf("Files gRPC listening on %s", cfg.GRPCAddress)
		if err := grpcServer.Serve(grpcListener); err != nil {
			if errors.Is(err, grpc.ErrServerStopped) {
				errCh <- nil
				return
			}
			errCh <- fmt.Errorf("serve grpc: %w", err)
			return
		}
		errCh <- nil
	}()

	var runErr error
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil && runErr == nil {
			runErr = err
			stop()
		}
	}
	return runErr
}
